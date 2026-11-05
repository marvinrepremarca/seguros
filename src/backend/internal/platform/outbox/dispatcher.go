package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// errFlagUnavailable: el flag del consumidor no se pudo evaluar (se reintenta, no se descarta).
var errFlagUnavailable = errors.New("flag no disponible")

var consumerPattern = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9-]*$`) //nolint:gochecknoglobals // regexp inmutable

// Handler procesa un evento dentro de la transacción (con RLS del tenant del evento) en la que
// también se registra en el inbox: los efectos y la marca de procesado son atómicos.
type Handler func(ctx context.Context, tx pgx.Tx, e Event) error

// Consumer es un consumidor de eventos de integración.
type Consumer struct {
	// Name identifica al consumidor en el inbox: <modulo>.<consumidor> (ej. billing.create-payment-plan).
	Name  string
	Topic string
	// Flag del módulo consumidor; vacío solo para módulos núcleo. Con el flag apagado para el
	// tenant del evento, el consumidor no produce efectos y confirma el mensaje.
	Flag   flags.Key
	Handle Handler
}

// FlagResolver evalúa un flag para un tenant (lo implementa flags.Evaluator).
type FlagResolver interface {
	Resolve(ctx context.Context, tenant kernel.TenantID, plan string, key flags.Key) (flags.Resolution, error)
}

// TenantPlans resuelve el plan vigente de un tenant (para evaluar flags). Lo implementa cmd/worker
// sobre el directorio de tenancy.
type TenantPlans interface {
	PlanOf(ctx context.Context, tenant kernel.TenantID) (string, error)
}

// DispatcherOptions configura el despachador.
type DispatcherOptions struct {
	DB         *db.DB
	Subscriber message.Subscriber
	// Flags resuelve el flag del consumidor. Se usa Resolve (no un Checker fail-closed): si el flag
	// no se puede evaluar, el evento se reintenta en vez de confirmarse como "apagado" y perderse.
	Flags  FlagResolver
	Plans  TenantPlans
	Clock  kernel.Clock
	Logger *slog.Logger
	// MaxRetries antes de enviar el mensaje a messaging.dead_letters (default 5).
	MaxRetries int
	// InitialInterval del backoff exponencial entre reintentos (default 100 ms).
	InitialInterval time.Duration
}

// Dispatcher entrega los eventos a los consumidores registrados (Watermill Router).
type Dispatcher struct {
	opts   DispatcherOptions
	router *message.Router
}

// NewInProcessPubSub crea el transporte en proceso (fase 1, un nodo): Publish bloquea hasta que
// todos los consumidores confirman. En fase 2 se sustituye por RabbitMQ sin tocar los consumidores.
func NewInProcessPubSub(logger *slog.Logger) *gochannel.GoChannel {
	return gochannel.NewGoChannel(gochannel.Config{BlockPublishUntilSubscriberAck: true}, watermill.NewSlogLogger(logger))
}

// NewDispatcher construye el despachador.
func NewDispatcher(opts DispatcherOptions) (*Dispatcher, error) {
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = 5
	}
	if opts.InitialInterval <= 0 {
		opts.InitialInterval = 100 * time.Millisecond
	}
	router, err := message.NewRouter(message.RouterConfig{CloseTimeout: 30 * time.Second}, watermill.NewSlogLogger(opts.Logger))
	if err != nil {
		return nil, fmt.Errorf("outbox: router: %w", err)
	}
	return &Dispatcher{opts: opts, router: router}, nil
}

// Register añade un consumidor. Debe llamarse antes de Run.
func (d *Dispatcher) Register(c Consumer) error {
	if !consumerPattern.MatchString(c.Name) || len(c.Name) > 120 {
		return fmt.Errorf("outbox: nombre de consumidor inválido %q", c.Name)
	}
	if !topicPattern.MatchString(c.Topic) {
		return fmt.Errorf("%w: %q", ErrInvalidTopic, c.Topic)
	}
	if c.Handle == nil {
		return fmt.Errorf("outbox: consumidor %s sin handler", c.Name)
	}
	h := d.router.AddConsumerHandler(c.Name, c.Topic, d.opts.Subscriber, func(msg *message.Message) error {
		return d.process(c, msg)
	})
	// Orden (de fuera hacia dentro): dead letter → reintentos con backoff → recuperación de panic.
	h.AddMiddleware(
		d.deadLetter(c),
		middleware.Retry{
			MaxRetries: d.opts.MaxRetries, InitialInterval: d.opts.InitialInterval,
			Multiplier: 2, MaxInterval: 5 * time.Second, Logger: watermill.NewSlogLogger(d.opts.Logger),
		}.Middleware,
		middleware.Recoverer,
	)
	return nil
}

// Run atiende hasta que ctx se cancele.
func (d *Dispatcher) Run(ctx context.Context) error {
	if err := d.router.Run(ctx); err != nil {
		return fmt.Errorf("outbox: router: %w", err)
	}
	return nil
}

// Running se cierra cuando todos los consumidores están suscritos (arranca el relay después).
func (d *Dispatcher) Running() chan struct{} { return d.router.Running() }

func eventOf(msg *message.Message, topic string) (Event, error) {
	id, err := uuid.Parse(msg.UUID)
	if err != nil {
		return Event{}, fmt.Errorf("outbox: id de evento inválido: %w", err)
	}
	t, err := uuid.Parse(msg.Metadata.Get(MetaTenantID))
	if err != nil || t == uuid.Nil {
		return Event{}, errors.New("outbox: evento sin tenant válido")
	}
	at, err := time.Parse(time.RFC3339Nano, msg.Metadata.Get(MetaOccurredAt))
	if err != nil {
		return Event{}, fmt.Errorf("outbox: occurred_at inválido: %w", err)
	}
	return Event{ID: id, TenantID: kernel.TenantID(t), Topic: topic, OccurredAt: at, Payload: []byte(msg.Payload)}, nil
}

func (d *Dispatcher) process(c Consumer, msg *message.Message) error {
	if t := msg.Metadata.Get(MetaTargetConsumers); t != "" && !slices.Contains(strings.Split(t, ","), c.Name) {
		return nil // reproceso dirigido a otro consumidor
	}
	e, err := eventOf(msg, c.Topic)
	if err != nil {
		// Mensaje imposible de atribuir a un tenant: no se puede reintentar ni guardar con RLS.
		d.opts.Logger.Error("evento mal formado descartado", "consumer", c.Name, "error", err)
		return nil
	}
	ctx := reqctx.WithTenantID(msg.Context(), e.TenantID)
	if c.Flag != "" {
		plan, err := d.opts.Plans.PlanOf(ctx, e.TenantID)
		if err != nil {
			return fmt.Errorf("outbox: plan del tenant: %w", err)
		}
		res, err := d.opts.Flags.Resolve(ctx, e.TenantID, plan, c.Flag)
		if err != nil || res.Source == flags.SourceError {
			return fmt.Errorf("outbox: flag %s no evaluable: %w", c.Flag, errors.Join(err, errFlagUnavailable))
		}
		if !res.Enabled {
			d.opts.Logger.DebugContext(ctx, "consumidor apagado por flag", "consumer", c.Name, "flag", string(c.Flag), "event_id", e.ID.String())
			return nil
		}
	}
	return d.opts.DB.WithTenantTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO messaging.inbox (consumer, event_id, tenant_id, processed_at) VALUES ($1, $2, $3, $4)
			ON CONFLICT DO NOTHING`, c.Name, e.ID, uuid.UUID(e.TenantID), d.opts.Clock.Now().UTC())
		if err != nil {
			return fmt.Errorf("outbox: inbox: %w", err)
		}
		if tag.RowsAffected() == 0 {
			d.opts.Logger.DebugContext(ctx, "evento ya procesado", "consumer", c.Name, "event_id", e.ID.String())
			return nil
		}
		if err := c.Handle(ctx, tx, e); err != nil {
			return fmt.Errorf("outbox: %s: %w", c.Name, err)
		}
		return nil
	})
}

// deadLetter guarda en messaging.dead_letters el mensaje que agotó sus reintentos y lo confirma
// (el relay no queda bloqueado; el mensaje se puede reprocesar desde la tabla).
func (d *Dispatcher) deadLetter(c Consumer) message.HandlerMiddleware {
	return func(h message.HandlerFunc) message.HandlerFunc {
		return func(msg *message.Message) ([]*message.Message, error) {
			out, err := h(msg)
			if err == nil {
				return out, nil
			}
			e, perr := eventOf(msg, c.Topic)
			if perr != nil {
				d.opts.Logger.Error("evento mal formado descartado", "consumer", c.Name, "error", perr)
				return nil, nil
			}
			ctx := reqctx.WithTenantID(context.WithoutCancel(msg.Context()), e.TenantID)
			d.opts.Logger.ErrorContext(ctx, "evento enviado a dead letters", "consumer", c.Name, "event_id", e.ID.String(), "error", err) // el log no persiste en BD; trace_id correlaciona
			if derr := d.recordDeadLetter(ctx, c, e, err); derr != nil {
				// Sin poder guardarlo, se devuelve el error: Watermill lo reentrega.
				return nil, errors.Join(err, derr)
			}
			return nil, nil
		}
	}
}

func (d *Dispatcher) recordDeadLetter(ctx context.Context, c Consumer, e Event, cause error) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	// Solo un código estable: el texto de un error puede contener datos (valores de SQL, PII).
	msg := "outbox.consumer_failed"
	if de, ok := kernel.AsDomainError(cause); ok {
		msg = de.Code()
	}
	return d.opts.DB.WithTenantTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO messaging.dead_letters (id, tenant_id, consumer, event_id, topic, payload, error, failed_at, occurred_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			id, uuid.UUID(e.TenantID), c.Name, e.ID, e.Topic, []byte(e.Payload), msg, d.opts.Clock.Now().UTC(), e.OccurredAt)
		if err != nil {
			return fmt.Errorf("outbox: guardando dead letter: %w", err)
		}
		return nil
	})
}

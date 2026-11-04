//go:build integration

package outbox_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
	"github.com/marvinrepremarca/seguros/test/pgtest"
)

//nolint:gochecknoglobals // compartidos por la suite (TestMain)
var (
	database *db.DB // rol seguros_worker (relay y consumidores)
	appDB    *db.DB // rol seguros_app (API)
)

var platformDB *db.DB //nolint:gochecknoglobals // conexión de la suite

func TestMain(m *testing.M) { os.Exit(runMain(m)) }

func runMain(m *testing.M) int {
	ctx := context.Background()
	env, err := pgtest.Start(ctx)
	if err != nil {
		slog.Error("iniciando PostgreSQL", "error", err)
		return 1
	}
	defer env.Terminate() //nolint:errcheck // limpieza al final de la suite
	if err := env.MigrateAll(ctx); err != nil {
		slog.Error("migrando", "error", err)
		return 1
	}
	database, err = env.OpenWorker(ctx)
	if err != nil {
		slog.Error("abriendo BD", "error", err)
		return 1
	}
	defer database.Close()
	platformDB, err = env.OpenPlatform(ctx)
	if err != nil {
		return 1
	}
	defer platformDB.Close()
	appDB, err = env.OpenApp(ctx)
	if err != nil {
		slog.Error("abriendo BD", "error", err)
		return 1
	}
	defer appDB.Close()
	return m.Run()
}

type payload struct {
	PolicyID string `json:"policyId"`
}

// flagResolver enciende el flag solo para los tenants indicados; con fail, no puede evaluarlo.
type flagResolver struct {
	mu   sync.Mutex
	on   map[kernel.TenantID]bool
	fail bool
}

func (f *flagResolver) Resolve(_ context.Context, t kernel.TenantID, _ string, _ flags.Key) (flags.Resolution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return flags.Resolution{Source: flags.SourceError}, errors.New("store caído")
	}
	return flags.Resolution{Enabled: f.on[t], Source: flags.SourceOverride}, nil
}

type plans struct{}

func (plans) PlanOf(context.Context, kernel.TenantID) (string, error) { return "basico", nil }

type recorder struct {
	mu     sync.Mutex
	events []outbox.Event
	fail   bool
}

func (r *recorder) handle(ctx context.Context, tx pgx.Tx, e outbox.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return errors.New("falla siempre")
	}
	if tid, ok := reqctx.TenantID(ctx); !ok || tid != e.TenantID {
		return errors.New("el contexto no trae el tenant del evento")
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM messaging.inbox WHERE event_id = $1`, e.ID).Scan(&n); err != nil {
		return err
	}
	if n != 1 {
		return errors.New("el inbox se escribe en la misma transacción")
	}
	r.events = append(r.events, e)
	return nil
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

type harness struct {
	pubsub message.Publisher
	relay  *outbox.Relay
	writer *outbox.Writer
	cancel context.CancelFunc
	done   chan error
}

// start levanta un despachador con los consumidores dados sobre un tópico propio de la prueba.
func start(t *testing.T, consumers ...outbox.Consumer) *harness {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	clock := kernel.SystemClock{}
	ps := outbox.NewInProcessPubSub(logger)
	d, err := outbox.NewDispatcher(outbox.DispatcherOptions{
		DB: database, Subscriber: ps, Flags: checker, Plans: plans{}, Clock: clock, Logger: logger,
		MaxRetries: 2, InitialInterval: time.Millisecond,
	})
	require.NoError(t, err)
	for _, c := range consumers {
		require.NoError(t, d.Register(c))
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	<-d.Running()
	h := &harness{pubsub: ps, relay: outbox.NewRelay(database, ps, clock, logger, outbox.RelayOptions{}), writer: outbox.NewWriter(clock), cancel: cancel, done: done}
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-done)
		require.NoError(t, ps.Close())
	})
	return h
}

var checker = &flagResolver{on: map[kernel.TenantID]bool{}} //nolint:gochecknoglobals // flags de la suite

// La suite comparte el outbox: las pruebas no son paralelas para que cada relay publique solo lo suyo.

func newTenant() context.Context {
	return reqctx.WithTenantID(context.Background(), kernel.TenantID(uuid.Must(uuid.NewV7())))
}

func write(ctx context.Context, t *testing.T, h *harness, topic string, commit bool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		var err error
		id, err = h.writer.Add(ctx, tx, topic, payload{PolicyID: "p-1"})
		if err != nil {
			return err
		}
		if !commit {
			return errors.New("rollback")
		}
		return nil
	})
	if commit {
		require.NoError(t, err)
	}
	return id
}

func drain(t *testing.T, h *harness) {
	t.Helper()
	for {
		n, err := h.relay.RelayOnce(context.Background())
		require.NoError(t, err)
		if n == 0 {
			return
		}
	}
}

func TestDeliveryIsAtomicAndIdempotent(t *testing.T) {
	rec := &recorder{}
	h := start(t, outbox.Consumer{Name: "test.recorder", Topic: "test.policy_issued", Handle: rec.handle})
	ctx := newTenant()

	id := write(ctx, t, h, "test.policy_issued", true)
	write(ctx, t, h, "test.policy_issued", false) // rollback: el evento no existe
	drain(t, h)

	require.Equal(t, 1, rec.count())
	e := rec.events[0]
	assert.Equal(t, id, e.ID)
	var p payload
	require.NoError(t, e.Decode(&p))
	assert.Equal(t, "p-1", p.PolicyID)

	// Reentrega del mismo evento (at-least-once): el inbox evita el doble efecto.
	msg := message.NewMessage(id.String(), message.Payload(e.Payload))
	msg.Metadata.Set(outbox.MetaTenantID, e.TenantID.String())
	msg.Metadata.Set(outbox.MetaOccurredAt, e.OccurredAt.Format(time.RFC3339Nano))
	require.NoError(t, h.pubsub.Publish("test.policy_issued", msg))
	assert.Equal(t, 1, rec.count())

	drain(t, h)
	assert.Equal(t, 1, rec.count(), "publicado una sola vez")
}

func TestFlagOffConsumerHasNoEffects(t *testing.T) {
	rec := &recorder{}
	h := start(t, outbox.Consumer{Name: "test.flagged", Topic: "test.flagged_event", Flag: flags.ClaimsModule, Handle: rec.handle})
	on, off := newTenant(), newTenant()
	tOn, _ := reqctx.TenantID(on)
	checker.mu.Lock()
	checker.on[tOn] = true
	checker.mu.Unlock()

	write(on, t, h, "test.flagged_event", true)
	write(off, t, h, "test.flagged_event", true)
	drain(t, h)

	require.Equal(t, 1, rec.count(), "solo el tenant con el módulo encendido")
	assert.Equal(t, tOn, rec.events[0].TenantID)
	var n int
	require.NoError(t, database.WithTenantTx(off, func(tx pgx.Tx) error {
		return tx.QueryRow(off, `SELECT count(*) FROM messaging.inbox`).Scan(&n)
	}))
	assert.Zero(t, n, "con el flag apagado ni siquiera se marca en el inbox (al encender no se pierde nada nuevo)")
}

func TestFailingConsumerGoesToDeadLetters(t *testing.T) {
	rec := &recorder{fail: true}
	h := start(t, outbox.Consumer{Name: "test.failing", Topic: "test.failing_event", Handle: rec.handle})
	ctx := newTenant()
	id := write(ctx, t, h, "test.failing_event", true)
	drain(t, h)

	var n int
	var consumer string
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*), max(consumer) FROM messaging.dead_letters WHERE event_id = $1`, id).Scan(&n, &consumer)
	}))
	assert.Equal(t, 1, n)
	assert.Equal(t, "test.failing", consumer)
	n2, err := h.relay.RelayOnce(context.Background())
	require.NoError(t, err)
	assert.Zero(t, n2, "el evento quedó publicado: el relay no se bloquea")
}

func TestAPIRoleCannotRelay(t *testing.T) {
	for _, q := range []string{
		`SELECT * FROM messaging.claim_outbox(10)`,
		`SELECT messaging.mark_published(ARRAY[gen_random_uuid()], now())`,
		`SELECT messaging.purge_published(now())`,
	} {
		err := appDB.WithoutTenantTx(context.Background(), func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), q)
			return err
		})
		require.Error(t, err, "la API no cruza tenants: %s", q)
	}
}

func TestOutboxRLS(t *testing.T) {
	ctx := newTenant()
	err := appDB.WithTenantTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT count(*) FROM messaging.outbox`)
		return err
	})
	require.Error(t, err, "el rol de la app no lee el outbox directamente")

	err = appDB.WithTenantTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO messaging.outbox (id, tenant_id, topic, payload, occurred_at) VALUES ($1, $2, 'test.x', '{}', now())`,
			uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()))
		return err
	})
	require.Error(t, err, "no puede insertar eventos de otro tenant")
}

func TestWriterValidates(t *testing.T) {
	w := outbox.NewWriter(kernel.SystemClock{})
	ctx := newTenant()
	err := database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		_, err := w.Add(ctx, tx, "Topic Malo", payload{})
		return err
	})
	require.ErrorIs(t, err, outbox.ErrInvalidTopic)
	err = database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		_, err := w.Add(ctx, tx, "test.list", []string{"no es objeto"})
		return err
	})
	require.ErrorIs(t, err, outbox.ErrPayloadTooLong)
	err = database.WithoutTenantTx(context.Background(), func(tx pgx.Tx) error {
		_, err := w.Add(context.Background(), tx, "test.x", payload{})
		return err
	})
	require.ErrorIs(t, err, outbox.ErrNoTenant)
}

func TestUnavailableFlagIsRetriedNotDropped(t *testing.T) {
	rec := &recorder{}
	h := start(t, outbox.Consumer{Name: "test.flag-down", Topic: "test.flag_down", Flag: flags.ClaimsModule, Handle: rec.handle})
	ctx := newTenant()
	tid, _ := reqctx.TenantID(ctx)
	checker.mu.Lock()
	checker.on[tid] = true
	checker.fail = true
	checker.mu.Unlock()
	t.Cleanup(func() {
		checker.mu.Lock()
		checker.fail = false
		checker.mu.Unlock()
	})

	id := write(ctx, t, h, "test.flag_down", true)
	drain(t, h)
	assert.Zero(t, rec.count(), "sin flag evaluable no hay efectos")
	var n int
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM messaging.dead_letters WHERE event_id = $1`, id).Scan(&n)
	}))
	assert.Equal(t, 1, n, "queda en dead letters para reproceso: no se confirma como apagado")
}

func TestDeadLetterRetryRepublishesTheEvent(t *testing.T) {
	rec := &recorder{fail: true}
	h := start(t, outbox.Consumer{Name: "test.retry", Topic: "test.retry_event", Handle: rec.handle})
	ctx := newTenant()
	id := write(ctx, t, h, "test.retry_event", true)
	drain(t, h)

	platform := platformDB
	letters, err := outbox.ListDeadLetters(ctx, platform)
	require.NoError(t, err)
	require.Len(t, letters, 1)
	assert.Equal(t, id, letters[0].EventID)

	other := newTenant()
	require.ErrorIs(t, outbox.RetryDeadLetter(other, platform, letters[0].ID, time.Now()), outbox.ErrDeadLetterNotPending, "otro tenant no lo alcanza") //nolint:forbidigo // instante informativo

	rec.mu.Lock()
	rec.fail = false
	rec.mu.Unlock()
	require.NoError(t, outbox.RetryDeadLetter(ctx, platform, letters[0].ID, time.Now()))                                                 //nolint:forbidigo // instante informativo
	require.ErrorIs(t, outbox.RetryDeadLetter(ctx, platform, letters[0].ID, time.Now()), outbox.ErrDeadLetterNotPending, "una sola vez") //nolint:forbidigo // instante informativo
	drain(t, h)
	assert.Equal(t, 1, rec.count(), "el consumidor lo procesó al reintentar")
	letters, err = outbox.ListDeadLetters(ctx, platform)
	require.NoError(t, err)
	assert.Empty(t, letters)
}

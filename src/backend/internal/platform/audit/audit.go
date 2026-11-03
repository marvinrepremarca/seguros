// Package audit escribe la bitácora inmutable audit.events (ROADMAP 1.9). Record se invoca dentro
// de la misma transacción que el cambio auditado (db.WithTenantTx), de modo que no hay cambio sin
// evento ni evento sin cambio. El tenant y el actor salen del contexto autenticado (reqctx); nunca
// del llamador.
//
// details nunca lleva PII ni datos Restringidos (rules/security.md): solo IDs, códigos y valores
// técnicos. Record valida claves y tamaños para que un error de programación no filtre datos.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/trace"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Outcome es el resultado de la acción auditada.
type Outcome string

// Resultados posibles.
const (
	OutcomeSuccess Outcome = "success"
	OutcomeDenied  Outcome = "denied"
	OutcomeFailure Outcome = "failure"
)

// Límites (coinciden con los CHECK de la migración).
const (
	maxDetails     = 20
	maxDetailValue = 200
	maxResourceID  = 200
)

//nolint:gochecknoglobals // regexps inmutables
var (
	actionPattern       = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_-]*){2,4}$`)
	resourceTypePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_-]*){0,3}$`)
	detailKeyPattern    = regexp.MustCompile(`^[a-z][a-zA-Z0-9_]{0,39}$`)
)

// ErrInvalidEntry indica una entrada mal formada (error de programación, nunca de usuario).
var ErrInvalidEntry = kernel.NewError(kernel.KindValidation, "audit.entry.invalid", "entrada de auditoría inválida")

// Entry describe una acción auditada.
type Entry struct {
	// Action es <modulo>.<recurso>.<accion> (ej. "identity.session.login").
	Action string
	// ResourceType es <modulo>.<recurso> (ej. "featuremgmt.flag").
	ResourceType string
	// ResourceID identifica el recurso (UUID, clave de flag...). Nunca un dato personal.
	ResourceID string
	Outcome    Outcome
	// Details: valores técnicos sin PII (valor anterior/nuevo de un flag, motivo, códigos).
	Details map[string]string
}

// Recorder escribe eventos de auditoría.
type Recorder struct {
	clock kernel.Clock
}

// NewRecorder crea un Recorder.
func NewRecorder(clock kernel.Clock) *Recorder { return &Recorder{clock: clock} }

// Record inserta el evento en tx, que debe haber sido abierta con db.WithTenantTx para el tenant
// del contexto (RLS rechaza cualquier otro). Sin usuario en el contexto, el actor es "system".
func (r *Recorder) Record(ctx context.Context, tx pgx.Tx, e Entry) error {
	tenant, ok := reqctx.TenantID(ctx)
	if !ok {
		return fmt.Errorf("audit: %w", ErrInvalidEntry.WithMessage("sin tenant en el contexto"))
	}
	if err := e.validate(); err != nil {
		return err
	}
	details, err := json.Marshal(nonNil(e.Details))
	if err != nil {
		return fmt.Errorf("audit: serializando details: %w", err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("audit: generando id: %w", err)
	}

	actorType, actorID := "system", uuid.NullUUID{}
	if user, ok := reqctx.UserID(ctx); ok {
		actorType, actorID = "user", uuid.NullUUID{UUID: uuid.UUID(user), Valid: true}
	}
	var traceID *string
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
		s := sc.TraceID().String()
		traceID = &s
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO audit.events
			(id, tenant_id, occurred_at, actor_type, actor_id, action, resource_type, resource_id,
			 outcome, request_id, trace_id, details)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9, NULLIF($10, ''), $11, $12)`,
		id, uuid.UUID(tenant), r.clock.Now().UTC(), actorType, actorID, e.Action, e.ResourceType, e.ResourceID,
		string(e.Outcome), reqctx.RequestID(ctx), traceID, details,
	)
	if err != nil {
		return fmt.Errorf("audit: insertando evento %s: %w", e.Action, err)
	}
	return nil
}

func (e Entry) validate() error {
	invalid := func(msg string) error { return fmt.Errorf("audit: %w", ErrInvalidEntry.WithMessage("%s", msg)) }
	switch {
	case !actionPattern.MatchString(e.Action) || len(e.Action) > 120:
		return invalid("action debe ser <modulo>.<recurso>.<accion>")
	case !resourceTypePattern.MatchString(e.ResourceType) || len(e.ResourceType) > 80:
		return invalid("resourceType inválido")
	case len(e.ResourceID) > maxResourceID:
		return invalid("resourceId demasiado largo")
	case len(e.Details) > maxDetails:
		return invalid("demasiados details")
	}
	switch e.Outcome {
	case OutcomeSuccess, OutcomeDenied, OutcomeFailure:
	default:
		return invalid("outcome inválido")
	}
	for k, v := range e.Details {
		if !detailKeyPattern.MatchString(k) || len(v) > maxDetailValue {
			return invalid("detail inválido: " + k)
		}
	}
	return nil
}

func nonNil(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

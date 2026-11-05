// Package app contiene los casos de uso de billing.
package app

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/marvinrepremarca/seguros/internal/modules/billing/domain"
	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Errores de la aplicación.
var (
	ErrPlanNotFound    = kernel.NewError(kernel.KindNotFound, "billing.plan.not_found", "la póliza no tiene plan de pagos")
	ErrPaymentNotFound = kernel.NewError(kernel.KindNotFound, "billing.payment.not_found", "pago inexistente")
	ErrKeyReused       = kernel.NewError(kernel.KindConflict, "billing.payment.idempotency_key_reused", "esa llave ya se usó con otros datos")
	ErrInvalidText     = kernel.NewError(kernel.KindValidation, "billing.invalid_text", "el texto es obligatorio y no puede superar el máximo")
)

// Payment es un pago registrado.
type Payment struct {
	ID               uuid.UUID
	PlanID           uuid.UUID
	Amount           kernel.Money
	Surplus          kernel.Money
	PaidOn           kernel.Date
	Method           string
	ReportedBy       string
	SupportReference string
	RequestHash      string
	CreatedAt        time.Time
	ReversedAt       *time.Time
}

// Activity es una gestión de cobro.
type Activity struct {
	ID        uuid.UUID
	Channel   string
	Note      string
	CreatedAt time.Time
}

// Receivable es una cuota con saldo.
type Receivable struct {
	InstallmentID uuid.UUID
	PolicyID      uuid.UUID
	PolicyNumber  string
	InsurerID     uuid.UUID
	HolderPartyID uuid.UUID
	HolderName    string
	Currency      string
	DueDate       kernel.Date
	Balance       decimal.Decimal
	DaysOverdue   int32
}

// Bucket es el saldo de un tramo de antigüedad.
type Bucket struct {
	Currency     string
	Bucket       string
	Balance      decimal.Decimal
	Installments int32
}

// ArrearsSetting son los días de gracia; sin aseguradora, el general del tenant.
type ArrearsSetting struct {
	InsurerID uuid.NullUUID
	GraceDays int32
}

// Repository persiste billing en el tenant del contexto.
type Repository interface {
	InsertPlan(ctx context.Context, p *domain.PaymentPlan) error
	// GetPlan devuelve ErrPlanNotFound si no existe.
	GetPlan(ctx context.Context, policyID uuid.UUID) (*domain.PaymentPlan, error)
	// SavePlan devuelve domain.ErrConcurrentUpdate si la versión cambió.
	SavePlan(ctx context.Context, p *domain.PaymentPlan) error
	InsertPayment(ctx context.Context, p Payment, key string, allocations []domain.Allocation) error
	PaymentByKey(ctx context.Context, key string) (Payment, bool, error)
	GetPayment(ctx context.Context, planID, id uuid.UUID) (Payment, []domain.Allocation, error)
	ListPayments(ctx context.Context, planID uuid.UUID) ([]Payment, error)
	MarkReversed(ctx context.Context, id uuid.UUID, reason string, at time.Time) error
	InsertActivity(ctx context.Context, planID uuid.UUID, a Activity) error
	ListActivities(ctx context.Context, planID uuid.UUID) ([]Activity, error)
	GraceDays(ctx context.Context, insurerID uuid.UUID) (int32, error)
	LeadDays(ctx context.Context) (int32, error)
	SetLeadDays(ctx context.Context, days int32, at time.Time) error
	ListSettings(ctx context.Context) ([]ArrearsSetting, error)
	SetSetting(ctx context.Context, s ArrearsSetting, at time.Time) error
	ListReceivables(ctx context.Context, today kernel.Date, minDays, maxDays int32, after uuid.NullUUID, limit int32) ([]Receivable, error)
	Summary(ctx context.Context, today kernel.Date) ([]Bucket, error)
	PoliciesWithDue(ctx context.Context, today kernel.Date) ([]uuid.UUID, error)
}

// UnitOfWork es una transacción del tenant.
type UnitOfWork interface {
	Billing() Repository
	Audit(ctx context.Context, e audit.Entry) error
	Publish(ctx context.Context, topic string, payload any) error
}

// Store abre unidades de trabajo.
type Store interface {
	WithinTenant(ctx context.Context, fn func(ctx context.Context, uow UnitOfWork) error) error
}

// Service agrupa los casos de uso.
type Service struct {
	Store   Store
	Parties parties.Directory
	Clock   kernel.Clock
}

// NewID genera IDs UUIDv7.
func NewID() (uuid.UUID, error) { return uuid.NewV7() }

func entry(action string, id uuid.UUID, details map[string]string) audit.Entry {
	return audit.Entry{Action: action, ResourceType: "billing.plan", ResourceID: id.String(), Outcome: audit.OutcomeSuccess, Details: details}
}

func cleanText(s string, limit int) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || len([]rune(s)) > limit {
		return "", ErrInvalidText
	}
	return s, nil
}

func (s *Service) today() kernel.Date { return kernel.Today(s.Clock) }

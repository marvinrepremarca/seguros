// Package app contiene los casos de uso de renovaciones.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/renewals/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// DueCase es un caso que puede cruzar un umbral.
type DueCase struct {
	PolicyID    uuid.UUID
	EffectiveTo kernel.Date
}

// Repository persiste renovaciones en el tenant del contexto.
type Repository interface {
	InsertCase(ctx context.Context, s domain.Snapshot, at time.Time) error
	GetCase(ctx context.Context, policyID uuid.UUID) (*domain.Case, error)
	SaveCase(ctx context.Context, c *domain.Case, at time.Time) error
	ListCases(ctx context.Context, status string, after uuid.NullUUID, limit int32) ([]domain.Snapshot, error)
	OpenCasesUntil(ctx context.Context, today, until kernel.Date) ([]DueCase, error)
	SentAlerts(ctx context.Context, policyID uuid.UUID) ([]int32, error)
	InsertAlert(ctx context.Context, policyID uuid.UUID, days int32, at time.Time) error
	AlertDays(ctx context.Context) ([]int32, error)
	SetAlertDays(ctx context.Context, days []int32, at time.Time) error
	Retention(ctx context.Context, from, to kernel.Date) (due, renewed int32, err error)
}

// UnitOfWork es una transacción del tenant.
type UnitOfWork interface {
	Renewals() Repository
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

func entry(action string, id uuid.UUID) audit.Entry {
	return audit.Entry{Action: action, ResourceType: "renewals.case", ResourceID: id.String(), Outcome: audit.OutcomeSuccess}
}

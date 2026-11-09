// Package app contiene los casos de uso de cumplimiento.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/compliance/domain"
	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Suppression es una solicitud de supresión de datos.
type Suppression struct {
	ID, PartyID uuid.UUID
	Status      string
	Resolution  string
	CreatedAt   time.Time
}

// Repository persiste cumplimiento en el tenant del contexto.
type Repository interface {
	InsertCheck(ctx context.Context, c domain.KycCheck, details []byte, at time.Time) error
	LatestCheck(ctx context.Context, party uuid.UUID) (domain.KycCheck, bool, error)
	Checks(ctx context.Context, party uuid.UUID) ([]domain.KycCheck, error)
	CheckDetails(ctx context.Context, party, check uuid.UUID) ([]byte, error)
	InsertComplaint(ctx context.Context, s domain.ComplaintData, at time.Time) error
	GetComplaint(ctx context.Context, id uuid.UUID) (*domain.Complaint, error)
	SaveComplaint(ctx context.Context, c *domain.Complaint, at time.Time) error
	Complaints(ctx context.Context, before uuid.NullUUID, limit int32) ([]domain.ComplaintData, error)
	InsertSuppression(ctx context.Context, id, party uuid.UUID, at time.Time) error
	ResolveSuppression(ctx context.Context, id uuid.UUID, accepted bool, resolution string, at time.Time) error
	Suppressions(ctx context.Context, before uuid.NullUUID, limit int32) ([]Suppression, error)
}

// UnitOfWork es una transacción del tenant.
type UnitOfWork interface {
	Records() Repository
	Audit(ctx context.Context, e audit.Entry) error
	Publish(ctx context.Context, topic string, payload any) error
}

// Store abre unidades de trabajo.
type Store interface {
	WithinTenant(ctx context.Context, fn func(ctx context.Context, uow UnitOfWork) error) error
}

// Cipher cifra datos Restringidos del tenant del contexto (lo implementa platform/crypto).
type Cipher interface {
	EncryptFor(ctx context.Context, field, record string, plaintext []byte) ([]byte, error)
	DecryptFor(ctx context.Context, field, record string, ciphertext []byte) ([]byte, error)
}

// Service agrupa los casos de uso.
type Service struct {
	Store   Store
	Parties parties.ComplianceView
	Cipher  Cipher
	Clock   kernel.Clock
}

func entry(action, resource string, id uuid.UUID) audit.Entry {
	return audit.Entry{Action: action, ResourceType: resource, ResourceID: id.String(), Outcome: audit.OutcomeSuccess}
}

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

// SealedCheck son los campos Restringidos de una verificación, cifrados con la llave del tenant.
type SealedCheck struct{ Result, Source, Details []byte }

// StoredCheck es una verificación leída: Result y Source vienen cifrados, o en claro en filas previas
// al cifrado (migración 00002).
type StoredCheck struct {
	Check        domain.KycCheck
	ResultSealed []byte
	SourceSealed []byte
}

// Repository persiste cumplimiento en el tenant del contexto.
type Repository interface {
	InsertCheck(ctx context.Context, c domain.KycCheck, sealed SealedCheck, at time.Time) error
	LatestCheck(ctx context.Context, party uuid.UUID) (StoredCheck, bool, error)
	Checks(ctx context.Context, party uuid.UUID) ([]StoredCheck, error)
	CheckDetails(ctx context.Context, party, check uuid.UUID) ([]byte, error)
	InsertComplaint(ctx context.Context, s domain.ComplaintData, at time.Time) error
	GetComplaint(ctx context.Context, id uuid.UUID) (*domain.Complaint, error)
	SaveComplaint(ctx context.Context, c *domain.Complaint, at time.Time) error
	Complaints(ctx context.Context, before uuid.NullUUID, limit int32) ([]domain.ComplaintData, error)
	InsertSuppression(ctx context.Context, id, party uuid.UUID, at time.Time) error
	GetSuppression(ctx context.Context, id uuid.UUID) (domain.SuppressionStatus, int32, error)
	SaveSuppression(ctx context.Context, id uuid.UUID, status domain.SuppressionStatus, resolution string, version int32, at time.Time) error
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

// Package app contiene los casos de uso de siniestros.
package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/claims/domain"
	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// maxAgentPolicies acota el alcance de un asesor en el listado. Lo que exceda queda fuera: falla cerrado.
const maxAgentPolicies = 5000

// ChecklistItem es un soporte esperado.
type ChecklistItem struct {
	ID       uuid.UUID
	Label    string
	Received bool
}

// Note es una gestión.
type Note struct {
	ID        uuid.UUID
	Body      string
	CreatedAt time.Time
}

// ListFilter filtra el listado. Scoped limita a Policies.
type ListFilter struct {
	Status   string
	Scoped   bool
	Policies []uuid.UUID
	Before   uuid.NullUUID
	Limit    int32
}

// Repository persiste siniestros del tenant del contexto.
type Repository interface {
	Insert(ctx context.Context, s domain.Snapshot, at time.Time) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Claim, error)
	Save(ctx context.Context, c *domain.Claim, at time.Time) error
	List(ctx context.Context, f ListFilter) ([]domain.Snapshot, error)
	SetHealth(ctx context.Context, id uuid.UUID, ciphertext []byte, at time.Time) error
	Health(ctx context.Context, id uuid.UUID) ([]byte, error)
	AddChecklistItem(ctx context.Context, claim uuid.UUID, label string, at time.Time) error
	SetChecklistItem(ctx context.Context, claim, item uuid.UUID, received bool, at time.Time) error
	Checklist(ctx context.Context, claim uuid.UUID) ([]ChecklistItem, error)
	AddNote(ctx context.Context, claim uuid.UUID, body string, at time.Time) error
	Notes(ctx context.Context, claim uuid.UUID) ([]Note, error)
}

// UnitOfWork es una transacción del tenant.
type UnitOfWork interface {
	Claims() Repository
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
	Store    Store
	Policies policies.Directory
	Parties  parties.Directory
	Cipher   Cipher
	Clock    kernel.Clock
}

// View es un siniestro con el nombre del tomador.
type View struct {
	domain.Snapshot
	HolderName string
}

func entry(action string, id uuid.UUID) audit.Entry {
	return audit.Entry{Action: action, ResourceType: "claims.claim", ResourceID: id.String(), Outcome: audit.OutcomeSuccess}
}

func currentUser(ctx context.Context) (uuid.UUID, error) {
	u, ok := reqctx.UserID(ctx)
	if !ok {
		return uuid.Nil, authz.ErrUnauthenticated
	}
	return uuid.UUID(u), nil
}

// ownScope devuelve el asesor que limita la lectura (DECISIONS N29); sin valor, ve todos.
func ownScope(ctx context.Context) (uuid.NullUUID, error) {
	err := authz.Require(ctx, authz.ClaimRead)
	if err == nil {
		return uuid.NullUUID{}, nil
	}
	if !errors.Is(err, authz.ErrForbidden) {
		return uuid.NullUUID{}, err
	}
	if err := authz.Require(ctx, authz.ClaimReadOwn); err != nil {
		return uuid.NullUUID{}, err
	}
	u, err := currentUser(ctx)
	if err != nil {
		return uuid.NullUUID{}, err
	}
	return uuid.NullUUID{UUID: u, Valid: true}, nil
}

func (s *Service) views(ctx context.Context, rows []domain.Snapshot) ([]View, error) {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.HolderPartyID)
	}
	names, err := s.Parties.Summaries(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("claims: consultando tomadores: %w", err)
	}
	// Quien no puede leer salud no sabe si existe.
	hide := authz.Require(ctx, authz.ClaimHealthRead) != nil
	// El soporte de la decisión es texto libre de la aseguradora: el asesor solo ve el estado (N29).
	own := authz.Require(ctx, authz.ClaimRead) != nil
	out := make([]View, 0, len(rows))
	for _, r := range rows {
		if hide {
			r.HasHealthDetails = false
		}
		if own {
			r.DecisionSupport = ""
		}
		out = append(out, View{Snapshot: r, HolderName: names[r.HolderPartyID].Name})
	}
	return out, nil
}

func (s *Service) view(ctx context.Context, snap domain.Snapshot) (View, error) {
	out, err := s.views(ctx, []domain.Snapshot{snap})
	if err != nil {
		return View{}, err
	}
	return out[0], nil
}

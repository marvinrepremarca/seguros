// Package app contiene los casos de uso de policies.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	catalog "github.com/marvinrepremarca/seguros/internal/modules/catalog/contracts"
	identity "github.com/marvinrepremarca/seguros/internal/modules/identity/contracts"
	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// ListFilter filtra el listado de pólizas.
type ListFilter struct {
	Before uuid.NullUUID
	Agent  uuid.NullUUID // solo las pólizas de este asesor
	Status domain.Status // vacío = todas
	Limit  int32
}

// Summary es una fila del listado de pólizas.
type Summary struct {
	ID               domain.PolicyID
	Status           domain.Status
	Number           string
	InsurerID        uuid.UUID
	LineOfBusinessID uuid.UUID
	HolderPartyID    uuid.UUID
	HolderName       string
	NetPremium       kernel.Money
	Taxes            kernel.Money
	EffectiveFrom    kernel.Date
	EffectiveTo      kernel.Date
}

// RenewalSettings dice qué debe conservar una renovación (DECISIONS N25). Por defecto, tomador y ramo.
type RenewalSettings struct{ KeepHolder, KeepLine bool }

// Renewal es una nueva vigencia de una póliza.
type Renewal struct {
	ID     domain.PolicyID
	Status domain.Status
}

// PolicyRepository persiste el agregado Policy del tenant del contexto.
type PolicyRepository interface {
	Insert(ctx context.Context, p *domain.Policy) error
	// Get devuelve domain.ErrNotFound si no existe (o es de otro tenant).
	Get(ctx context.Context, id domain.PolicyID) (*domain.Policy, error)
	// Save aplica concurrencia optimista (domain.ErrConcurrentUpdate) y devuelve domain.ErrNumberTaken
	// si el número de póliza ya existe en esa aseguradora.
	Save(ctx context.Context, p *domain.Policy) error
	List(ctx context.Context, f ListFilter) ([]Summary, error)
	// AppendAgentHistory agrega una fila al historial de asesores (append-only).
	AppendAgentHistory(ctx context.Context, e domain.PolicyAgentChanged) error
	// ReassignableByAgent lista pólizas no terminadas del asesor, sin las que se le asignaron después de cutoff.
	ReassignableByAgent(ctx context.Context, agent uuid.UUID, cutoff time.Time, limit int32) ([]domain.PolicyID, error)
	InsertTransferRequest(ctx context.Context, q TransferRequest) error
	// RenewalsOf lista las nuevas vigencias no rechazadas ni canceladas de la póliza.
	RenewalsOf(ctx context.Context, previous domain.PolicyID) ([]Renewal, error)
	RenewalSettings(ctx context.Context) (RenewalSettings, error)
	SetRenewalSettings(ctx context.Context, v RenewalSettings, at time.Time) error
	// IDsAfter recorre las pólizas del tenant en orden de ID.
	IDsAfter(ctx context.Context, after uuid.NullUUID, limit int32) ([]domain.PolicyID, error)
}

// UnitOfWork agrupa repositorio, auditoría y outbox en una transacción con RLS.
type UnitOfWork interface {
	Policies() PolicyRepository
	Audit(ctx context.Context, e audit.Entry) error
	// Publish escribe un evento de integración en el outbox de la misma transacción.
	Publish(ctx context.Context, topic string, payload any) error
}

// DuePolicy identifica una póliza por vencer, de cualquier tenant.
type DuePolicy struct {
	Tenant kernel.TenantID
	ID     domain.PolicyID
}

// Store abre unidades de trabajo para el tenant del contexto.
type Store interface {
	WithinTenant(ctx context.Context, fn func(ctx context.Context, uow UnitOfWork) error) error
	// DueForExpiration lista pólizas Vigentes cuyo último día cubierto ya pasó (solo el worker).
	DueForExpiration(ctx context.Context, today kernel.Date, limit int32) ([]DuePolicy, error)
}

// Deps son las dependencias de los casos de uso.
type Deps struct {
	Store   Store
	Parties parties.Directory
	Catalog catalog.Directory
	Schemas catalog.RiskSchemas
	Users   identity.Directory
	Gate    *KycGate
	Clock   kernel.Clock
	Logger  *slog.Logger
}

// Service agrupa los comandos sobre el agregado Policy: comparten la misma unidad de trabajo
// (cargar → validar versión → dominio → guardar → auditar → publicar), en change.
type Service struct{ Deps }

// View es una póliza con los nombres de sus partes.
type View struct {
	domain.Snapshot
	PartyNames map[uuid.UUID]string
}

// change ejecuta fn sobre la póliza en una transacción y audita el cambio de estado.
func (s *Service) change(ctx context.Context, id domain.PolicyID, version int32, action string, fn func(*domain.Policy) (map[string]string, error)) (View, error) {
	var snapshot domain.Snapshot
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		p, err := uow.Policies().Get(ctx, id)
		if err != nil {
			return err
		}
		if p.Version() != version {
			return domain.ErrConcurrentUpdate
		}
		from := p.Status()
		details, err := fn(p)
		if err != nil {
			return err
		}
		if err := uow.Policies().Save(ctx, p); err != nil {
			return err
		}
		if err := record(ctx, uow, p, action, from, details); err != nil {
			return err
		}
		current, err := uow.Policies().Get(ctx, id)
		if err != nil {
			return err
		}
		snapshot = current.Snapshot()
		return nil
	})
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, snapshot)
}

// record audita y publica los eventos acumulados por el agregado.
func record(ctx context.Context, uow UnitOfWork, p *domain.Policy, action string, from domain.Status, details map[string]string) error {
	if details == nil {
		details = map[string]string{}
	}
	if from != "" {
		details["from"] = string(from)
	}
	details["to"] = string(p.Status())
	if err := uow.Audit(ctx, audit.Entry{
		Action: action, ResourceType: "policies.policy", ResourceID: p.ID().String(), Outcome: audit.OutcomeSuccess, Details: details,
	}); err != nil {
		return err
	}
	return publish(ctx, uow, p.PullEvents())
}

func (s *Service) view(ctx context.Context, snap domain.Snapshot) (View, error) {
	ids := make([]uuid.UUID, 0, len(snap.Terms.Parties))
	for _, p := range snap.Terms.Parties {
		ids = append(ids, p.PartyID)
	}
	found, err := s.Parties.Summaries(ctx, ids)
	if err != nil {
		return View{}, fmt.Errorf("policies: consultando partes: %w", err)
	}
	names := make(map[uuid.UUID]string, len(found))
	for id, f := range found {
		names[id] = f.Name
	}
	return View{Snapshot: snap, PartyNames: names}, nil
}

// readScope resuelve quién puede leer: con policies.policy.read, todas; con read-own, solo las del
// asesor. El permiso declarado en el contrato es `authenticated` porque son dos permisos alternativos.
func readScope(ctx context.Context) (uuid.NullUUID, error) {
	if err := authz.Require(ctx, authz.PolicyRead); err == nil {
		return uuid.NullUUID{}, nil
	} else if !errors.Is(err, authz.ErrForbidden) {
		return uuid.NullUUID{}, err
	}
	if err := authz.Require(ctx, authz.PolicyReadOwn); err != nil {
		return uuid.NullUUID{}, err
	}
	user, ok := reqctx.UserID(ctx)
	if !ok {
		return uuid.NullUUID{}, authz.ErrUnauthenticated
	}
	return uuid.NullUUID{UUID: uuid.UUID(user), Valid: true}, nil
}

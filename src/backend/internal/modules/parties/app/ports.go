// Package app contiene los casos de uso de parties.
package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/parties/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// documentField es el campo cifrado del número de documento (AAD y derivación del blind index).
const documentField = "parties.party.document_number"

// Cipher cifra datos Restringidos del tenant del contexto (lo implementa platform/crypto).
type Cipher interface {
	EncryptFor(ctx context.Context, field, record string, plaintext []byte) ([]byte, error)
	DecryptFor(ctx context.Context, field, record string, ciphertext []byte) ([]byte, error)
	BlindIndex(ctx context.Context, field, normalized string) ([]byte, error)
}

// SealedDocument es el documento listo para guardar: cifrado y con su blind index.
type SealedDocument struct {
	Ciphertext []byte
	BlindIndex []byte
}

// ListFilter filtra el listado de terceros.
type ListFilter struct {
	NamePattern string // patrón ILIKE ya escapado; vacío = todos
	Before      uuid.NullUUID
	Limit       int32
	VisibleTo   uuid.NullUUID // solo los clientes de este asesor; sin valor = todos
}

// PartyRepository persiste terceros del tenant del contexto.
type PartyRepository interface {
	// Insert devuelve domain.ErrDocumentTaken si el documento ya existe.
	Insert(ctx context.Context, p *domain.Party, doc SealedDocument) error
	// Get devuelve domain.ErrNotFound si no existe (o es de otro tenant).
	Get(ctx context.Context, id domain.PartyID) (*domain.Party, error)
	// GetByBlindIndex devuelve domain.ErrNotFound si no hay coincidencia.
	GetByBlindIndex(ctx context.Context, docType domain.DocumentType, blindIndex []byte) (*domain.Party, error)
	GetDocument(ctx context.Context, id domain.PartyID) (domain.DocumentType, []byte, error)
	// IsVisibleTo indica si el tercero es cliente del asesor: lo registró o es parte de sus pólizas.
	IsVisibleTo(ctx context.Context, id domain.PartyID, agent uuid.UUID) (bool, error)
	List(ctx context.Context, f ListFilter) ([]*domain.Party, error)
	// Update aplica concurrencia optimista; doc nil conserva el documento guardado.
	Update(ctx context.Context, p *domain.Party, doc *SealedDocument) error
	InsertConsent(ctx context.Context, c domain.Consent) error
	ListConsents(ctx context.Context, id domain.PartyID) ([]domain.Consent, error)
	Summaries(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]contracts.PartySummary, error)
}

// UnitOfWork agrupa repositorio y auditoría en una transacción con RLS.
type UnitOfWork interface {
	Parties() PartyRepository
	Audit(ctx context.Context, e audit.Entry) error
}

// Store abre unidades de trabajo para el tenant del contexto.
type Store interface {
	WithinTenant(ctx context.Context, fn func(ctx context.Context, uow UnitOfWork) error) error
}

// Deps son las dependencias de los casos de uso.
type Deps struct {
	Store  Store
	Cipher Cipher
	Clock  kernel.Clock
}

func (d Deps) seal(ctx context.Context, id domain.PartyID, doc domain.DocumentID) (SealedDocument, error) {
	ciphertext, err := d.Cipher.EncryptFor(ctx, documentField, id.String(), []byte(doc.Number()))
	if err != nil {
		return SealedDocument{}, fmt.Errorf("parties: cifrando documento: %w", err)
	}
	index, err := d.blindIndex(ctx, doc)
	if err != nil {
		return SealedDocument{}, err
	}
	return SealedDocument{Ciphertext: ciphertext, BlindIndex: index}, nil
}

func (d Deps) blindIndex(ctx context.Context, doc domain.DocumentID) ([]byte, error) {
	index, err := d.Cipher.BlindIndex(ctx, documentField, doc.Number())
	if err != nil {
		return nil, fmt.Errorf("parties: índice del documento: %w", err)
	}
	return index, nil
}

func partyEntry(action string, id domain.PartyID, details map[string]string) audit.Entry {
	return audit.Entry{
		Action: action, ResourceType: "parties.party", ResourceID: id.String(),
		Outcome: audit.OutcomeSuccess, Details: details,
	}
}

// scopeFor resuelve el alcance del usuario: con full, todos los clientes (sin valor); con own, solo los
// suyos (DECISIONS N10). El contrato declara `authenticated` porque son permisos alternativos.
func scopeFor(ctx context.Context, full, own authz.Permission) (uuid.NullUUID, error) {
	err := authz.Require(ctx, full)
	if err == nil {
		return uuid.NullUUID{}, nil
	}
	if !errors.Is(err, authz.ErrForbidden) {
		return uuid.NullUUID{}, err
	}
	if err := authz.Require(ctx, own); err != nil {
		return uuid.NullUUID{}, err
	}
	user, ok := reqctx.UserID(ctx)
	if !ok {
		return uuid.NullUUID{}, authz.ErrUnauthenticated
	}
	return uuid.NullUUID{UUID: uuid.UUID(user), Valid: true}, nil
}

func readScope(ctx context.Context) (uuid.NullUUID, error) {
	return scopeFor(ctx, authz.PartyRead, authz.PartyReadOwn)
}

// getInScope carga el tercero; uno fuera del alcance responde como inexistente (no revela que existe).
func getInScope(ctx context.Context, repo PartyRepository, id domain.PartyID, scope uuid.NullUUID) (*domain.Party, error) {
	p, err := repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if scope.Valid {
		visible, err := repo.IsVisibleTo(ctx, id, scope.UUID)
		if err != nil {
			return nil, err
		}
		if !visible {
			return nil, domain.ErrNotFound
		}
	}
	return p, nil
}

// withDuplicateAudit deja rastro de un documento repetido, con el cliente que coincidió: la respuesta
// confirma que el número existe y la transacción del cambio ya se revirtió. Si no se puede auditar,
// responde como error interno: no se confirma la existencia sin rastro.
func (d Deps) withDuplicateAudit(ctx context.Context, err error, docType domain.DocumentType, blindIndex []byte) error {
	if !errors.Is(err, domain.ErrDocumentTaken) {
		return err
	}
	auditErr := d.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		existing, err := uow.Parties().GetByBlindIndex(ctx, docType, blindIndex)
		if err != nil {
			return err
		}
		return uow.Audit(ctx, audit.Entry{
			Action: "parties.party.duplicate_rejected", ResourceType: "parties.party", ResourceID: existing.ID().String(), Outcome: audit.OutcomeDenied,
			Details: map[string]string{"documentType": string(docType)},
		})
	})
	if auditErr != nil {
		return fmt.Errorf("parties: auditando documento repetido: %s", auditErr.Error()) //nolint:errorlint // sin %w: un error de dominio envuelto expondría un código al cliente
	}
	return err
}

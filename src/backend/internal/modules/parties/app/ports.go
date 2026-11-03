// Package app contiene los casos de uso de parties.
package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/parties/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
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

package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/parties/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// GetPartyHandler consulta un tercero.
type GetPartyHandler struct{ Deps }

// Handle devuelve el tercero (404 si es de otro tenant).
func (h *GetPartyHandler) Handle(ctx context.Context, id domain.PartyID) (domain.PartySnapshot, error) {
	if err := authz.Require(ctx, authz.PartyRead); err != nil {
		return domain.PartySnapshot{}, err
	}
	var out domain.PartySnapshot
	err := h.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		p, err := uow.Parties().Get(ctx, id)
		if err != nil {
			return err
		}
		out = p.Snapshot()
		return nil
	})
	return out, err
}

// ListPartiesQuery filtra y pagina.
type ListPartiesQuery struct {
	Q      string
	Cursor string
	Limit  int32
}

// PartyPage es una página de terceros.
type PartyPage struct {
	Items      []domain.PartySnapshot
	NextCursor string
}

// ListPartiesHandler lista terceros.
type ListPartiesHandler struct{ Deps }

// Handle pagina por ID (keyset), con búsqueda opcional por parte del nombre.
func (h *ListPartiesHandler) Handle(ctx context.Context, q ListPartiesQuery) (PartyPage, error) {
	if err := authz.Require(ctx, authz.PartyRead); err != nil {
		return PartyPage{}, err
	}
	before, err := kernel.DecodeCursor(q.Cursor)
	if err != nil {
		return PartyPage{}, err
	}
	size := kernel.PageSize(q.Limit)
	filter := ListFilter{Before: before, Limit: size + 1}
	if t := strings.TrimSpace(q.Q); t != "" {
		filter.NamePattern = "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(t) + "%"
	}
	var page PartyPage
	err = h.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		rows, err := uow.Parties().List(ctx, filter)
		if err != nil {
			return err
		}
		if int32(len(rows)) > size { //nolint:gosec // acotado por MaxPageSize + 1
			rows = rows[:size]
			page.NextCursor = kernel.EncodeCursor(uuid.UUID(rows[size-1].ID()))
		}
		page.Items = make([]domain.PartySnapshot, 0, len(rows))
		for _, p := range rows {
			page.Items = append(page.Items, p.Snapshot())
		}
		return nil
	})
	return page, err
}

// SearchByDocumentHandler busca un tercero por documento exacto.
type SearchByDocumentHandler struct{ Deps }

// Handle busca por blind index. Toda búsqueda queda auditada, también la que no encuentra nada.
func (h *SearchByDocumentHandler) Handle(ctx context.Context, in DocumentInput) (domain.PartySnapshot, error) {
	if err := authz.Require(ctx, authz.PartyRead); err != nil {
		return domain.PartySnapshot{}, err
	}
	doc, err := domain.NewDocumentID(in.Type, in.Number)
	if err != nil {
		return domain.PartySnapshot{}, err
	}
	index, err := h.blindIndex(ctx, doc)
	if err != nil {
		return domain.PartySnapshot{}, err
	}
	var found *domain.Party
	err = h.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		p, err := uow.Parties().GetByBlindIndex(ctx, doc.Type(), index)
		if err != nil && !isNotFound(err) {
			return err
		}
		resource := domain.PartyID{}
		if err == nil {
			found, resource = p, p.ID()
		}
		// Sin coincidencia no hay recurso: el evento registra el intento (y nunca el número).
		entry := partyEntry("parties.party.document_searched", resource, map[string]string{
			"documentType": string(doc.Type()), "found": strconv.FormatBool(found != nil),
		})
		if found == nil {
			entry.ResourceID = ""
		}
		return uow.Audit(ctx, entry) // la transacción debe confirmar aunque no se encuentre
	})
	if err != nil {
		return domain.PartySnapshot{}, err
	}
	if found == nil {
		return domain.PartySnapshot{}, domain.ErrNotFound
	}
	return found.Snapshot(), nil
}

func isNotFound(err error) bool {
	de, ok := kernel.AsDomainError(err)
	return ok && de.Code() == domain.ErrNotFound.Code()
}

// RevealDocumentHandler devuelve el documento completo.
type RevealDocumentHandler struct{ Deps }

// Handle audita la lectura ANTES de descifrar: no hay lectura de dato Restringido sin rastro.
func (h *RevealDocumentHandler) Handle(ctx context.Context, id domain.PartyID) (domain.DocumentType, string, error) {
	if err := authz.Require(ctx, authz.DocumentReveal); err != nil {
		return "", "", err
	}
	var docType domain.DocumentType
	var ciphertext []byte
	err := h.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		docType, ciphertext, err = uow.Parties().GetDocument(ctx, id)
		if err != nil {
			return err
		}
		return uow.Audit(ctx, partyEntry("parties.document.revealed", id, nil))
	})
	if err != nil {
		return "", "", err
	}
	plain, err := h.Cipher.DecryptFor(ctx, documentField, id.String(), ciphertext)
	if err != nil {
		return "", "", fmt.Errorf("parties: descifrando documento: %w", err)
	}
	return docType, string(plain), nil
}

// RegisterConsentCommand registra una autorización o revocación.
type RegisterConsentCommand struct {
	PartyID     domain.PartyID
	Status      domain.ConsentStatus
	Purpose     string
	TextVersion string
	ConsentDate kernel.Date
}

// RegisterConsentHandler registra autorizaciones.
type RegisterConsentHandler struct{ Deps }

// Handle guarda un registro inmutable y lo audita.
func (h *RegisterConsentHandler) Handle(ctx context.Context, cmd RegisterConsentCommand) (domain.Consent, error) {
	if err := authz.Require(ctx, authz.ConsentCreate); err != nil {
		return domain.Consent{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return domain.Consent{}, fmt.Errorf("parties: %w", err)
	}
	consent, err := domain.NewConsent(id, cmd.PartyID, cmd.Status, cmd.Purpose, cmd.TextVersion, cmd.ConsentDate, kernel.Today(h.Clock), h.Clock.Now())
	if err != nil {
		return domain.Consent{}, err
	}
	err = h.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if _, err := uow.Parties().Get(ctx, cmd.PartyID); err != nil {
			return err
		}
		if err := uow.Parties().InsertConsent(ctx, consent); err != nil {
			return err
		}
		return uow.Audit(ctx, partyEntry("parties.consent.registered", cmd.PartyID, map[string]string{
			"status": string(consent.Status), "textVersion": consent.TextVersion,
		}))
	})
	if err != nil {
		return domain.Consent{}, err
	}
	return consent, nil
}

// ListConsentsHandler consulta el historial de autorizaciones.
type ListConsentsHandler struct{ Deps }

// Handle devuelve el historial del tercero (404 si no existe).
func (h *ListConsentsHandler) Handle(ctx context.Context, id domain.PartyID) ([]domain.Consent, error) {
	if err := authz.Require(ctx, authz.ConsentRead); err != nil {
		return nil, err
	}
	var out []domain.Consent
	err := h.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if _, err := uow.Parties().Get(ctx, id); err != nil {
			return err
		}
		var err error
		out, err = uow.Parties().ListConsents(ctx, id)
		return err
	})
	return out, err
}

// Directory implementa contracts.Directory para otros módulos.
type Directory struct{ Deps }

var _ contracts.Directory = (*Directory)(nil)

// Summaries implementa contracts.Directory. Sin permiso propio: lo invoca un caso de uso que ya
// autorizó su operación, y solo expone el nombre (nunca el documento).
func (d *Directory) Summaries(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]contracts.PartySummary, error) {
	var out map[uuid.UUID]contracts.PartySummary
	err := d.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		out, err = uow.Parties().Summaries(ctx, ids)
		return err
	})
	return out, err
}

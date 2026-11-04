package app

import (
	"context"
	"strconv"

	"github.com/marvinrepremarca/seguros/internal/modules/parties/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// SearchByDocumentHandler busca un tercero por documento exacto.
type SearchByDocumentHandler struct{ Deps }

// Handle busca por blind index. Toda búsqueda queda auditada, también la que no encuentra nada.
func (h *SearchByDocumentHandler) Handle(ctx context.Context, in DocumentInput) (domain.PartySnapshot, error) {
	scope, err := readScope(ctx)
	if err != nil {
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
		resource, outOfScope := domain.PartyID{}, false
		if err == nil {
			resource = p.ID()
			found = p
			if scope.Valid {
				visible, err := uow.Parties().IsVisibleTo(ctx, p.ID(), scope.UUID)
				if err != nil {
					return err
				}
				if !visible { // cliente de otro asesor: responde como inexistente y queda auditado
					found, outOfScope = nil, true
				}
			}
		}
		// Sin coincidencia no hay recurso: el evento registra el intento (y nunca el número).
		entry := partyEntry("parties.party.document_searched", resource, map[string]string{
			"documentType": string(doc.Type()), "found": strconv.FormatBool(found != nil), "outOfScope": strconv.FormatBool(outOfScope),
		})
		if err != nil {
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

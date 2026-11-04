package app

import (
	"context"
	"strconv"

	"github.com/marvinrepremarca/seguros/internal/modules/parties/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// DocumentInput es un documento recibido sin validar.
type DocumentInput struct {
	Type   domain.DocumentType
	Number string
}

// UpdatePartyCommand rectifica un tercero. Document nil conserva el documento.
type UpdatePartyCommand struct {
	ID       domain.PartyID
	Version  int32
	Name     string
	Contact  domain.Contact
	Document *DocumentInput
}

// UpdatePartyHandler rectifica terceros.
type UpdatePartyHandler struct{ Deps }

// Handle aplica la rectificación con concurrencia optimista y la audita sin datos personales.
func (h *UpdatePartyHandler) Handle(ctx context.Context, cmd UpdatePartyCommand) (domain.PartySnapshot, error) {
	scope, err := scopeFor(ctx, authz.PartyUpdate, authz.PartyUpdateOwn)
	if err != nil {
		return domain.PartySnapshot{}, err
	}
	// Cambiar el documento cambia la identidad del cliente: tiene permiso propio (DECISIONS N10).
	if cmd.Document != nil {
		if err := authz.Require(ctx, authz.DocumentRectify); err != nil {
			return domain.PartySnapshot{}, err
		}
	}
	var doc *domain.DocumentID
	var sealed *SealedDocument
	if cmd.Document != nil {
		parsed, err := domain.NewDocumentID(cmd.Document.Type, cmd.Document.Number)
		if err != nil {
			return domain.PartySnapshot{}, err
		}
		s, err := h.seal(ctx, cmd.ID, parsed)
		if err != nil {
			return domain.PartySnapshot{}, err
		}
		doc, sealed = &parsed, &s
	}
	var out domain.PartySnapshot
	err = h.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		party, err := getInScope(ctx, uow.Parties(), cmd.ID, scope)
		if err != nil {
			return err
		}
		if party.Version() != cmd.Version {
			return domain.ErrConcurrentUpdate
		}
		if err := party.Rectify(cmd.Name, cmd.Contact, doc, h.Clock.Now()); err != nil {
			return err
		}
		if err := uow.Parties().Update(ctx, party, sealed); err != nil {
			return err
		}
		if err := uow.Audit(ctx, partyEntry("parties.party.updated", cmd.ID, map[string]string{
			"documentChanged": strconv.FormatBool(doc != nil),
		})); err != nil {
			return err
		}
		updated, err := uow.Parties().Get(ctx, cmd.ID)
		if err != nil {
			return err
		}
		out = updated.Snapshot()
		return nil
	})
	if err != nil && cmd.Document != nil {
		return out, h.withDuplicateAudit(ctx, err, cmd.Document.Type)
	}
	return out, err
}

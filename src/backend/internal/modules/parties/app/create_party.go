package app

import (
	"context"
	"fmt"

	"github.com/marvinrepremarca/seguros/internal/modules/parties/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// CreatePartyCommand registra un tercero.
type CreatePartyCommand struct {
	Kind           domain.Kind
	Name           string
	DocumentType   domain.DocumentType
	DocumentNumber string
	Contact        domain.Contact
}

// CreatePartyHandler registra terceros.
type CreatePartyHandler struct{ Deps }

// Handle valida, cifra el documento fuera de la transacción y guarda con auditoría.
func (h *CreatePartyHandler) Handle(ctx context.Context, cmd CreatePartyCommand) (domain.PartySnapshot, error) {
	if err := authz.Require(ctx, authz.PartyCreate); err != nil {
		return domain.PartySnapshot{}, err
	}
	doc, err := domain.NewDocumentID(cmd.DocumentType, cmd.DocumentNumber)
	if err != nil {
		return domain.PartySnapshot{}, err
	}
	id, err := kernel.NewID[domain.PartyID]()
	if err != nil {
		return domain.PartySnapshot{}, fmt.Errorf("parties: %w", err)
	}
	party, err := domain.NewParty(id, cmd.Kind, cmd.Name, doc, cmd.Contact, h.Clock.Now())
	if err != nil {
		return domain.PartySnapshot{}, err
	}
	sealed, err := h.seal(ctx, id, doc)
	if err != nil {
		return domain.PartySnapshot{}, err
	}
	err = h.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Parties().Insert(ctx, party, sealed); err != nil {
			return err
		}
		return uow.Audit(ctx, partyEntry("parties.party.created", id, map[string]string{"kind": string(cmd.Kind)}))
	})
	if err != nil {
		return domain.PartySnapshot{}, err
	}
	return party.Snapshot(), nil
}

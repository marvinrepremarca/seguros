package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/parties/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// AssignResponsibleCommand traspasa un cliente a otro asesor.
type AssignResponsibleCommand struct {
	ID      domain.PartyID
	Version int32
	AgentID uuid.UUID
	Reason  string
	Note    string
}

// AssignResponsibleHandler traspasa clientes (DECISIONS N11).
type AssignResponsibleHandler struct{ Deps }

// Handle exige un asesor activo del tenant y audita el anterior, el nuevo y el motivo.
func (h *AssignResponsibleHandler) Handle(ctx context.Context, cmd AssignResponsibleCommand) (domain.PartySnapshot, error) {
	if err := authz.Require(ctx, authz.PartyAssign); err != nil {
		return domain.PartySnapshot{}, err
	}
	reason, err := domain.ValidTransferReason(cmd.Reason, cmd.Note)
	if err != nil {
		return domain.PartySnapshot{}, err
	}
	agent, ok, err := h.Agents.Agent(ctx, cmd.AgentID)
	if err != nil {
		return domain.PartySnapshot{}, fmt.Errorf("parties: consultando asesor: %w", err)
	}
	if !ok || !agent.Active {
		return domain.PartySnapshot{}, domain.ErrInvalidAgent
	}
	var out domain.PartySnapshot
	err = h.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		party, err := uow.Parties().Get(ctx, cmd.ID)
		if err != nil {
			return err
		}
		if party.Version() != cmd.Version {
			return domain.ErrConcurrentUpdate
		}
		previous := party.Snapshot().Responsible
		party.AssignResponsible(cmd.AgentID, h.Clock.Now())
		if err := uow.Parties().Update(ctx, party, nil); err != nil {
			return err
		}
		if err := uow.Parties().RecordResponsibleChange(ctx, cmd.ID, party.Version()+1, previous, cmd.AgentID, reason, party.Snapshot().UpdatedAt); err != nil {
			return err
		}
		details := map[string]string{"new": cmd.AgentID.String(), "reason": reason.Code}
		if previous.Valid {
			details["previous"] = previous.UUID.String()
		}
		if err := uow.Audit(ctx, partyEntry("parties.party.responsible_changed", cmd.ID, details)); err != nil {
			return err
		}
		updated, err := uow.Parties().Get(ctx, cmd.ID)
		if err != nil {
			return err
		}
		out = updated.Snapshot()
		return nil
	})
	return out, err
}

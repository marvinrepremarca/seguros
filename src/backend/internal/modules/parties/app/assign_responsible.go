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
}

// AssignResponsibleHandler traspasa clientes (DECISIONS N11).
type AssignResponsibleHandler struct{ Deps }

// Handle exige un asesor activo del tenant y audita el anterior, el nuevo y el motivo.
func (h *AssignResponsibleHandler) Handle(ctx context.Context, cmd AssignResponsibleCommand) (domain.PartySnapshot, error) {
	if err := authz.Require(ctx, authz.PartyAssign); err != nil {
		return domain.PartySnapshot{}, err
	}
	reason, err := domain.ValidTransferReason(cmd.Reason)
	if err != nil {
		return domain.PartySnapshot{}, err
	}
	agents, err := h.Agents.Agents(ctx)
	if err != nil {
		return domain.PartySnapshot{}, fmt.Errorf("parties: consultando asesores: %w", err)
	}
	active := false
	for _, a := range agents {
		active = active || (a.ID == cmd.AgentID && a.Active)
	}
	if !active {
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
		details := map[string]string{"new": cmd.AgentID.String(), "reason": reason}
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

package app

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"

	identity "github.com/marvinrepremarca/seguros/internal/modules/identity/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// transferBatch es cuántas pólizas se traspasan por transacción del consumidor.
const transferBatch = 200

// TransferPortfolio pide pasar toda la cartera de un asesor a otro (DECISIONS N11). El origen puede
// ser alguien que ya se fue; el destino debe estar activo. Se procesa en el worker: una cartera grande
// no cabe en una petición.
func (s *Service) TransferPortfolio(ctx context.Context, from, to uuid.UUID, reason string) error {
	if err := authz.Require(ctx, authz.PolicyAssign); err != nil {
		return err
	}
	user, ok := reqctx.UserID(ctx)
	if !ok {
		return authz.ErrUnauthenticated
	}
	reason, err := domain.ValidTransferReason(reason)
	if err != nil {
		return err
	}
	if from == to {
		return domain.ErrSameAgent
	}
	if err := s.checkActiveAgent(ctx, to); err != nil {
		return err
	}
	users, err := s.Users.Agents(ctx)
	if err != nil {
		return fmt.Errorf("policies: consultando asesores: %w", err)
	}
	if !slices.ContainsFunc(users, func(u identity.User) bool { return u.ID == from }) {
		return domain.ErrInvalidAgent
	}
	return s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Audit(ctx, audit.Entry{
			Action: "policies.portfolio.transfer_requested", ResourceType: "policies.portfolio", ResourceID: from.String(), Outcome: audit.OutcomeSuccess,
			Details: map[string]string{"new": to.String(), "reason": reason},
		}); err != nil {
			return err
		}
		return uow.Publish(ctx, contracts.TopicPortfolioTransferRequested, contracts.PortfolioTransferRequested{
			FromAgentID: from, ToAgentID: to, Reason: reason, RequestedBy: uuid.UUID(user),
		})
	})
}

// TransferPoliciesBatch traspasa un lote dentro de la unidad de trabajo del consumidor. Si pudo quedar
// más, vuelve a publicar el evento: cada lote es una transacción acotada. Un destino que dejó de estar
// activo mientras tanto no se revalida aquí: el traspaso ya se autorizó al pedirlo.
func TransferPoliciesBatch(ctx context.Context, uow UnitOfWork, clock kernel.Clock, ev contracts.PortfolioTransferRequested) error {
	ids, err := uow.Policies().ReassignableByAgent(ctx, ev.FromAgentID, transferBatch)
	if err != nil {
		return err
	}
	for _, id := range ids {
		p, err := uow.Policies().Get(ctx, id)
		if err != nil {
			return err
		}
		from := p.Status()
		if err := p.ReassignAgent(ev.ToAgentID, ev.Reason, clock.Now()); err != nil {
			if errors.Is(err, domain.ErrSameAgent) {
				continue
			}
			return err
		}
		if err := uow.Policies().Save(ctx, p); err != nil {
			return err
		}
		if err := record(ctx, uow, p, "policies.policy.agent_changed", from, map[string]string{
			"previous": ev.FromAgentID.String(), "new": ev.ToAgentID.String(), "reason": ev.Reason, "requestedBy": ev.RequestedBy.String(),
		}); err != nil {
			return err
		}
	}
	if len(ids) == transferBatch {
		return uow.Publish(ctx, contracts.TopicPortfolioTransferRequested, ev)
	}
	return nil
}

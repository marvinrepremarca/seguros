package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
)

// Directory implementa contracts.Directory.
type Directory struct{ Store Store }

var _ contracts.Directory = (*Directory)(nil)

// Facts implementa contracts.Directory.
func (d *Directory) Facts(ctx context.Context, id uuid.UUID) (contracts.PolicyFacts, bool, error) {
	var out contracts.PolicyFacts
	err := d.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		p, err := uow.Policies().Get(ctx, domain.PolicyID(id))
		if err != nil {
			return err
		}
		s := p.Snapshot()
		out = contracts.PolicyFacts{
			ID: id, Status: string(s.Status), Number: s.Number, InsurerID: s.Terms.InsurerID, HolderPartyID: p.HolderID(), AgentID: s.Terms.AgentID,
			Currency: s.Terms.NetPremium.Currency(), EffectiveFrom: s.Terms.Period.From(), EffectiveTo: s.Terms.Period.To(),
		}
		return nil
	})
	if errors.Is(err, domain.ErrNotFound) {
		return contracts.PolicyFacts{}, false, nil
	}
	return out, err == nil, err
}

// IDsOfAgent implementa contracts.Directory.
func (d *Directory) IDsOfAgent(ctx context.Context, agent uuid.UUID, limit int32) ([]uuid.UUID, error) {
	var out []uuid.UUID
	err := d.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		rows, err := uow.Policies().List(ctx, ListFilter{Agent: uuid.NullUUID{UUID: agent, Valid: true}, Limit: limit})
		if err != nil {
			return err
		}
		out = make([]uuid.UUID, 0, len(rows))
		for _, r := range rows {
			out = append(out, uuid.UUID(r.ID))
		}
		return nil
	})
	return out, err
}

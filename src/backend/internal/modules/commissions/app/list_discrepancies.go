package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// ListDiscrepancies consulta las diferencias abiertas.
func (s *Service) ListDiscrepancies(ctx context.Context, cursor string, limit int32) ([]Discrepancy, string, error) {
	if err := authz.Require(ctx, authz.CommissionRead); err != nil {
		return nil, "", err
	}
	after, err := kernel.DecodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	size := kernel.PageSize(limit)
	var rows []Discrepancy
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		rows, err = uow.Commissions().ListDiscrepancies(ctx, after, size+1)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	items, next := page(rows, size, func(d Discrepancy) uuid.UUID { return d.ID })
	return items, next, nil
}

package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// ListAccruals consulta la comisión causada.
func (s *Service) ListAccruals(ctx context.Context, pending bool, cursor string, limit int32) ([]Accrual, string, error) {
	if err := authz.Require(ctx, authz.CommissionRead); err != nil {
		return nil, "", err
	}
	after, err := kernel.DecodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	size := kernel.PageSize(limit)
	var rows []Accrual
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		rows, err = uow.Commissions().ListAccruals(ctx, pending, after, size+1)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	items, next := page(rows, size, func(a Accrual) uuid.UUID { return a.ID })
	return items, next, nil
}

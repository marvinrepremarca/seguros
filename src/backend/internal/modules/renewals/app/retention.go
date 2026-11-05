package app

import (
	"context"

	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Retention consulta la tasa de retención del periodo.
func (s *Service) Retention(ctx context.Context, from, to kernel.Date) (int32, int32, error) {
	if err := authz.Require(ctx, authz.RenewalRead); err != nil {
		return 0, 0, err
	}
	var due, renewed int32
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		due, renewed, err = uow.Renewals().Retention(ctx, from, to)
		return err
	})
	return due, renewed, err
}

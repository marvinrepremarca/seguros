package app

import (
	"context"
	"errors"

	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Directory implementa contracts.Directory (consultas de solo lectura).
type Directory struct {
	store Store
}

var _ contracts.Directory = (*Directory)(nil)

// NewDirectory construye el directorio de tenants.
func NewDirectory(store Store) *Directory { return &Directory{store: store} }

// ResolveByOrgAlias implementa contracts.Directory.
func (d *Directory) ResolveByOrgAlias(ctx context.Context, orgAlias string) (contracts.TenantInfo, error) {
	id, err := d.store.ResolveOrgAlias(ctx, orgAlias)
	if err != nil {
		return contracts.TenantInfo{}, mapNotFound(err)
	}
	return d.Get(ctx, id)
}

// Get implementa contracts.Directory.
func (d *Directory) Get(ctx context.Context, id kernel.TenantID) (contracts.TenantInfo, error) {
	if kernel.IsZeroID(id) {
		return contracts.TenantInfo{}, contracts.ErrTenantNotFound
	}
	var info contracts.TenantInfo
	ctx = reqctx.WithTenantID(ctx, id)
	err := d.store.WithinTenant(ctx, func(ctx context.Context, repo TenantRepository) error {
		t, err := repo.Get(ctx, id)
		if err != nil {
			return err
		}
		info = contracts.TenantInfo{ID: t.ID(), Plan: string(t.Plan()), Active: t.IsActive()}
		return nil
	})
	if err != nil {
		return contracts.TenantInfo{}, mapNotFound(err)
	}
	return info, nil
}

func mapNotFound(err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return contracts.ErrTenantNotFound
	}
	return err
}

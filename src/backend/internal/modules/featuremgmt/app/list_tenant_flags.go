package app

import (
	"context"
	"fmt"

	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// ListTenantFlagsHandler es la consulta del estado de los flags del tenant.
type ListTenantFlagsHandler struct{ Deps }

// NewListTenantFlagsHandler construye el handler.
func NewListTenantFlagsHandler(d Deps) *ListTenantFlagsHandler { return &ListTenantFlagsHandler{d} }

// Handle devuelve el estado de todos los flags del tenant autenticado
// (featuremgmt.flag.read).
func (s *ListTenantFlagsHandler) Handle(ctx context.Context) ([]FlagView, error) {
	if err := authz.Require(ctx, authz.FeatureFlagRead); err != nil {
		return nil, err
	}
	tenant, _ := reqctx.TenantID(ctx)
	info, err := s.tenants.Get(ctx, tenant)
	if err != nil {
		return nil, fmt.Errorf("featuremgmt: leyendo tenant: %w", err)
	}
	global, err := s.store.GlobalSwitches(ctx)
	if err != nil {
		return nil, err
	}
	var views []FlagView
	err = s.store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		overrides, err := uow.Overrides().List(ctx, tenant)
		if err != nil {
			return err
		}
		state := flags.State{Global: global, Overrides: enabledMap(overrides)}
		for _, k := range s.reg.Keys() {
			def, _ := s.reg.Get(k)
			v := FlagView{Definition: def, Resolution: flags.Evaluate(s.reg, state, info.Plan, k)}
			if o, ok := overrides[k]; ok {
				v.Override = &o
			}
			views = append(views, v)
		}
		return nil
	})
	return views, err
}

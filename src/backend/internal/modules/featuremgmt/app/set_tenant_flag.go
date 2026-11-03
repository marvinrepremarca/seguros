package app

import (
	"context"
	"fmt"
	"strconv"

	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// SetTenantFlagCommand fija (Enabled != nil) o elimina (nil: vuelve al plan) un override.
type SetTenantFlagCommand struct {
	Key     flags.Key
	Enabled *bool
	Reason  string
}

// SetTenantFlagHandler cambia overrides de un tenant con un actor fijo: la API construye uno de
// TenantAdmin y cmd/platformctl uno de plataforma (nunca se elige el actor desde la petición).
type SetTenantFlagHandler struct {
	Deps
	actor Actor
}

// NewSetTenantFlagHandler construye el handler para el actor dado.
func NewSetTenantFlagHandler(d Deps, actor Actor) *SetTenantFlagHandler {
	return &SetTenantFlagHandler{Deps: d, actor: actor}
}

// Handle cambia un override del tenant del contexto. Valida editabilidad, plan y
// dependencias (no se enciende con dependencias apagadas ni se apaga algo de lo que dependen flags
// encendidos), audita en la misma transacción e invalida cachés tras el commit.
func (s *SetTenantFlagHandler) Handle(ctx context.Context, cmd SetTenantFlagCommand) (FlagView, error) {
	actor := s.actor
	if actor == ActorTenantAdmin {
		if err := authz.Require(ctx, authz.FeatureFlagUpdate); err != nil {
			return FlagView{}, err
		}
	}
	tenant, ok := reqctx.TenantID(ctx)
	if !ok {
		return FlagView{}, authz.ErrUnauthenticated
	}
	def, ok := s.reg.Get(cmd.Key)
	if !ok {
		return FlagView{}, domain.ErrUnknownFlag
	}
	if def.Kind == flags.KindOps {
		return FlagView{}, domain.ErrGlobalOnly
	}
	reason, err := domain.NewReason(cmd.Reason)
	if err != nil {
		return FlagView{}, err
	}
	info, err := s.tenants.Get(ctx, tenant)
	if err != nil {
		return FlagView{}, fmt.Errorf("featuremgmt: leyendo tenant: %w", err)
	}
	if actor == ActorTenantAdmin {
		if !def.TenantEditable {
			return FlagView{}, domain.ErrNotTenantEditable
		}
		if len(def.Plans) > 0 && !containsPlan(def.Plans, info.Plan) && cmd.Enabled != nil && *cmd.Enabled {
			return FlagView{}, domain.ErrNotInPlan
		}
	}
	global, err := s.store.GlobalSwitches(ctx)
	if err != nil {
		return FlagView{}, err
	}

	var view FlagView
	err = s.store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		current, err := uow.Overrides().List(ctx, tenant)
		if err != nil {
			return err
		}
		before := flags.State{Global: global, Overrides: enabledMap(current)}
		after := flags.State{Global: global, Overrides: enabledMap(current)}
		if cmd.Enabled == nil {
			delete(after.Overrides, cmd.Key)
		} else {
			after.Overrides[cmd.Key] = *cmd.Enabled
		}
		prev := flags.Evaluate(s.reg, before, info.Plan, cmd.Key)
		next := flags.Evaluate(s.reg, after, info.Plan, cmd.Key)
		if err := s.checkDependencies(def, before, after, info.Plan, next); err != nil {
			return err
		}

		now := s.clock.Now()
		if cmd.Enabled == nil {
			err = uow.Overrides().Delete(ctx, tenant, cmd.Key)
		} else {
			err = uow.Overrides().Upsert(ctx, tenant, cmd.Key, *cmd.Enabled, string(reason), now)
		}
		if err != nil {
			return err
		}
		override := "cleared"
		if cmd.Enabled != nil {
			override = strconv.FormatBool(*cmd.Enabled)
		}
		if err := uow.Audit(ctx, audit.Entry{
			Action: "featuremgmt.flag.updated", ResourceType: "featuremgmt.flag", ResourceID: string(cmd.Key),
			Outcome: audit.OutcomeSuccess, Details: map[string]string{
				"previous": strconv.FormatBool(prev.Enabled), "new": strconv.FormatBool(next.Enabled),
				"override": override, "source": string(next.Source), "reason": truncate(string(reason), 200),
				"actor": actorName(actor),
			},
		}); err != nil {
			return err
		}
		view = FlagView{Definition: def, Resolution: next}
		if cmd.Enabled != nil {
			view.Override = &Override{Enabled: *cmd.Enabled, Reason: string(reason), UpdatedAt: now}
		}
		return nil
	})
	if err != nil {
		return FlagView{}, err
	}
	s.invalidate(ctx, tenant)
	return view, nil
}

func (s *SetTenantFlagHandler) checkDependencies(def flags.Definition, before, after flags.State, plan string, next flags.Resolution) error {
	if next.Source == flags.SourceDependency {
		return domain.ErrDependencyDisabled
	}
	if next.Enabled {
		return nil
	}
	for _, dep := range s.reg.Dependents(def.Key) {
		if flags.Evaluate(s.reg, before, plan, dep).Enabled && !flags.Evaluate(s.reg, after, plan, dep).Enabled {
			return domain.ErrHasEnabledDependents.WithMessage("%s depende de %s", dep, def.Key)
		}
	}
	return nil
}

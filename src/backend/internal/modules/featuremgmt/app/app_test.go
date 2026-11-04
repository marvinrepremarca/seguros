package app_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt/app"
	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt/domain"
	tenancy "github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

const registryYAML = `flags:
  - {key: module.billing, kind: module, description: d, owner: billing, plans: [basico, profesional, empresarial]}
  - {key: module.commissions, kind: module, description: d, owner: commissions, plans: [empresarial], requires: [module.billing]}
  - {key: module.claims, kind: module, description: d, owner: claims, plans: [profesional, empresarial]}
  - {key: billing.reminders, kind: feature, description: d, owner: billing, default: true, tenantEditable: true, requires: [module.billing]}
  - {key: claims.whatsapp-intake, kind: feature, description: d, owner: claims, tenantEditable: true, plans: [empresarial]}
  - {key: ops.integrations.insurer-x, kind: ops, description: d, owner: integrations, default: true}
`

type fakeStore struct {
	mu        sync.Mutex
	overrides map[kernel.TenantID]map[flags.Key]app.Override
	global    map[flags.Key]bool
	audits    []audit.Entry
	events    []event
	failWrite error
}

func (s *fakeStore) WithinTenant(ctx context.Context, fn func(context.Context, app.UnitOfWork) error) error {
	if _, ok := reqctx.TenantID(ctx); !ok {
		return errors.New("sin tenant")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Transacción simulada: si fn falla, se descartan cambios y auditoría.
	snapshot := map[kernel.TenantID]map[flags.Key]app.Override{}
	for t, m := range s.overrides {
		snapshot[t] = map[flags.Key]app.Override{}
		for k, v := range m {
			snapshot[t][k] = v
		}
	}
	n, m := len(s.audits), len(s.events)
	if err := fn(ctx, &uow{s: s}); err != nil {
		s.overrides = snapshot
		s.audits = s.audits[:n]
		s.events = s.events[:m]
		return err
	}
	return nil
}

func (s *fakeStore) GlobalSwitches(context.Context) (map[flags.Key]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[flags.Key]bool{}
	for k, v := range s.global {
		out[k] = v
	}
	return out, nil
}

func (s *fakeStore) SetGlobalSwitch(_ context.Context, key flags.Key, enabled *bool, _, _ string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if enabled == nil {
		delete(s.global, key)
	} else {
		s.global[key] = *enabled
	}
	return nil
}

type uow struct{ s *fakeStore }

func (u *uow) Overrides() app.OverrideRepository { return u }

func (u *uow) Audit(_ context.Context, e audit.Entry) error {
	u.s.audits = append(u.s.audits, e)
	return nil
}

func (u *uow) Publish(_ context.Context, topic string, payload any) error {
	u.s.events = append(u.s.events, event{topic: topic, payload: payload})
	return nil
}

type event struct {
	topic   string
	payload any
}

func (u *uow) List(_ context.Context, t kernel.TenantID) (map[flags.Key]app.Override, error) {
	out := map[flags.Key]app.Override{}
	for k, v := range u.s.overrides[t] {
		out[k] = v
	}
	return out, nil
}

func (u *uow) Upsert(_ context.Context, t kernel.TenantID, k flags.Key, enabled bool, reason string, now time.Time) error {
	if u.s.failWrite != nil {
		return u.s.failWrite
	}
	if u.s.overrides[t] == nil {
		u.s.overrides[t] = map[flags.Key]app.Override{}
	}
	u.s.overrides[t][k] = app.Override{Enabled: enabled, Reason: reason, UpdatedAt: now}
	return nil
}

func (u *uow) Delete(_ context.Context, t kernel.TenantID, k flags.Key) error {
	delete(u.s.overrides[t], k)
	return nil
}

type fakeTenants struct{ plans map[kernel.TenantID]string }

func (f fakeTenants) ResolveByOrgAlias(context.Context, string) (tenancy.TenantInfo, error) {
	return tenancy.TenantInfo{}, tenancy.ErrTenantNotFound
}

func (f fakeTenants) Get(_ context.Context, id kernel.TenantID) (tenancy.TenantInfo, error) {
	p, ok := f.plans[id]
	if !ok {
		return tenancy.TenantInfo{}, tenancy.ErrTenantNotFound
	}
	return tenancy.TenantInfo{ID: id, Plan: p, Active: true}, nil
}

type fakeInvalidator struct {
	mu      sync.Mutex
	tenants []kernel.TenantID
	err     error
}

func (f *fakeInvalidator) Invalidate(_ context.Context, t kernel.TenantID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tenants = append(f.tenants, t)
	return f.err
}

type env struct {
	deps  app.Deps
	store *fakeStore
	inv   *fakeInvalidator
	plans map[kernel.TenantID]string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	reg, err := flags.ParseRegistry([]byte(registryYAML))
	require.NoError(t, err)
	store := &fakeStore{overrides: map[kernel.TenantID]map[flags.Key]app.Override{}, global: map[flags.Key]bool{}}
	inv := &fakeInvalidator{}
	plans := map[kernel.TenantID]string{}
	clock := kernel.NewFakeClock(time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC))
	return &env{
		deps:  app.NewDeps(reg, store, fakeTenants{plans: plans}, inv, clock, slog.New(slog.DiscardHandler)),
		store: store, inv: inv, plans: plans,
	}
}

func (e *env) tenant(plan string) kernel.TenantID {
	id := kernel.TenantID(uuid.Must(uuid.NewV7()))
	e.plans[id] = plan
	return id
}

func as(t *testing.T, tenant kernel.TenantID, roles ...authz.Role) context.Context {
	t.Helper()
	policy, err := authz.DefaultPolicy()
	require.NoError(t, err)
	p := authz.NewPrincipal(kernel.UserID(uuid.Must(uuid.NewV7())), tenant, roles, policy)
	return authz.WithPrincipal(context.Background(), p)
}

func platform(tenant kernel.TenantID) context.Context {
	return reqctx.WithTenantID(context.Background(), tenant)
}

func on() *bool  { v := true; return &v }
func off() *bool { v := false; return &v }

func TestTenantAdminTogglesEditableFeature(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tenant := e.tenant("basico")
	h := app.NewSetTenantFlagHandler(e.deps, app.ActorTenantAdmin)

	v, err := h.Handle(as(t, tenant, authz.RoleTenantAdmin), app.SetTenantFlagCommand{Key: "billing.reminders", Enabled: off(), Reason: "No queremos recordatorios"})
	require.NoError(t, err)
	assert.False(t, v.Resolution.Enabled)
	assert.Equal(t, flags.SourceOverride, v.Resolution.Source)
	require.NotNil(t, v.Override)

	require.Len(t, e.store.audits, 1)
	a := e.store.audits[0]
	assert.Equal(t, "featuremgmt.flag.updated", a.Action)
	assert.Equal(t, "billing.reminders", a.ResourceID)
	assert.Equal(t, map[string]string{
		"previous": "true", "new": "false", "override": "false", "source": "override",
		"reason": "No queremos recordatorios", "actor": "tenant_admin",
	}, a.Details)
	assert.Equal(t, []kernel.TenantID{tenant}, e.inv.tenants, "invalida la caché del tenant")
	require.Len(t, e.store.events, 1, "publica FeatureToggled en la misma transacción")
	assert.Equal(t, contracts.TopicFeatureToggled, e.store.events[0].topic)
	assert.Equal(t, contracts.FeatureToggled{FlagKey: "billing.reminders", Enabled: false}, e.store.events[0].payload)

	v, err = h.Handle(as(t, tenant, authz.RoleTenantAdmin), app.SetTenantFlagCommand{Key: "billing.reminders", Reason: "Volver al valor por defecto"})
	require.NoError(t, err)
	assert.True(t, v.Resolution.Enabled)
	assert.Nil(t, v.Override)
	assert.Equal(t, "cleared", e.store.audits[1].Details["override"])
}

func TestSetTenantFlagRejections(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		actor app.Actor
		plan  string
		roles []authz.Role
		cmd   app.SetTenantFlagCommand
		want  error
	}{
		{"sin permiso", app.ActorTenantAdmin, "basico", []authz.Role{authz.RoleManager}, app.SetTenantFlagCommand{Key: "billing.reminders", Enabled: off(), Reason: "motivo"}, authz.ErrForbidden},
		{"sin sesión", app.ActorTenantAdmin, "basico", nil, app.SetTenantFlagCommand{Key: "billing.reminders", Enabled: off(), Reason: "motivo"}, authz.ErrForbidden},
		{"módulo no editable por el tenant", app.ActorTenantAdmin, "basico", []authz.Role{authz.RoleTenantAdmin}, app.SetTenantFlagCommand{Key: "module.claims", Enabled: on(), Reason: "motivo"}, domain.ErrNotTenantEditable},
		{"funcionalidad fuera del plan", app.ActorTenantAdmin, "profesional", []authz.Role{authz.RoleTenantAdmin}, app.SetTenantFlagCommand{Key: "claims.whatsapp-intake", Enabled: on(), Reason: "motivo"}, domain.ErrNotInPlan},
		{"flag no registrado", app.ActorTenantAdmin, "basico", []authz.Role{authz.RoleTenantAdmin}, app.SetTenantFlagCommand{Key: "module.nada", Enabled: on(), Reason: "motivo"}, domain.ErrUnknownFlag},
		{"ops es global", app.ActorPlatform, "basico", nil, app.SetTenantFlagCommand{Key: "ops.integrations.insurer-x", Enabled: off(), Reason: "motivo"}, domain.ErrGlobalOnly},
		{"motivo corto", app.ActorPlatform, "basico", nil, app.SetTenantFlagCommand{Key: "module.claims", Enabled: on(), Reason: "x"}, domain.ErrInvalidReason},
		{"encender con dependencia apagada", app.ActorPlatform, "profesional", nil, app.SetTenantFlagCommand{Key: "module.commissions", Enabled: on(), Reason: "add-on"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			tenant := e.tenant(tt.plan)
			ctx := platform(tenant)
			if tt.actor == app.ActorTenantAdmin && tt.roles != nil {
				ctx = as(t, tenant, tt.roles...)
			}
			if tt.name == "sin sesión" {
				ctx = context.Background()
				_, err := app.NewSetTenantFlagHandler(e.deps, tt.actor).Handle(ctx, tt.cmd)
				require.ErrorIs(t, err, authz.ErrUnauthenticated)
				return
			}
			_, err := app.NewSetTenantFlagHandler(e.deps, tt.actor).Handle(ctx, tt.cmd)
			if tt.want == nil {
				require.NoError(t, err, "billing está en el plan profesional: la dependencia se cumple")
				return
			}
			require.ErrorIs(t, err, tt.want)
			assert.Empty(t, e.store.audits)
			assert.Empty(t, e.inv.tenants)
		})
	}
}

func TestDependencies(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tenant := e.tenant("empresarial") // billing y commissions por plan
	h := app.NewSetTenantFlagHandler(e.deps, app.ActorPlatform)
	ctx := platform(tenant)

	_, err := h.Handle(ctx, app.SetTenantFlagCommand{Key: "module.billing", Enabled: off(), Reason: "baja de cartera"})
	require.ErrorIs(t, err, domain.ErrHasEnabledDependents, "commissions depende de billing")

	_, err = h.Handle(ctx, app.SetTenantFlagCommand{Key: "module.commissions", Enabled: off(), Reason: "no usa comisiones"})
	require.NoError(t, err)
	_, err = h.Handle(ctx, app.SetTenantFlagCommand{Key: "billing.reminders", Enabled: off(), Reason: "sin recordatorios"})
	require.NoError(t, err)
	_, err = h.Handle(ctx, app.SetTenantFlagCommand{Key: "module.billing", Enabled: off(), Reason: "baja de cartera"})
	require.NoError(t, err, "ya no hay dependientes encendidos")

	_, err = h.Handle(ctx, app.SetTenantFlagCommand{Key: "module.commissions", Enabled: on(), Reason: "reactivar"})
	require.ErrorIs(t, err, domain.ErrDependencyDisabled)
	assert.Len(t, e.store.audits, 3)
}

func TestKillSwitchedDependencyBlocksEnable(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tenant := e.tenant("profesional")
	require.NoError(t, app.NewSetGlobalSwitchHandler(e.deps).Handle(context.Background(), app.SetGlobalSwitchCommand{Key: "module.billing", Enabled: off(), Reason: "incidente", Operator: "ana.ops"}))
	assert.Equal(t, []kernel.TenantID{{}}, e.inv.tenants, "invalida la caché global")
	_, err := app.NewSetTenantFlagHandler(e.deps, app.ActorPlatform).Handle(platform(tenant), app.SetTenantFlagCommand{Key: "module.commissions", Enabled: on(), Reason: "add-on"})
	require.ErrorIs(t, err, domain.ErrDependencyDisabled)
}

func TestSetGlobalSwitchValidates(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	h := app.NewSetGlobalSwitchHandler(e.deps)
	require.ErrorIs(t, h.Handle(context.Background(), app.SetGlobalSwitchCommand{Key: "module.nada", Enabled: off(), Reason: "motivo", Operator: "ana.ops"}), domain.ErrUnknownFlag)
	require.ErrorIs(t, h.Handle(context.Background(), app.SetGlobalSwitchCommand{Key: "module.billing", Enabled: off(), Reason: "", Operator: "ana.ops"}), domain.ErrInvalidReason)
	require.ErrorIs(t, h.Handle(context.Background(), app.SetGlobalSwitchCommand{Key: "module.billing", Reason: "restaurar"}), domain.ErrInvalidOperator)
	require.NoError(t, h.Handle(context.Background(), app.SetGlobalSwitchCommand{Key: "module.billing", Reason: "restaurar", Operator: "ana.ops"}))
}

func TestInvalidationFailureDoesNotFailTheChange(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.inv.err = errors.New("valkey caído")
	tenant := e.tenant("basico")
	_, err := app.NewSetTenantFlagHandler(e.deps, app.ActorPlatform).Handle(platform(tenant), app.SetTenantFlagCommand{Key: "module.claims", Enabled: on(), Reason: "add-on"})
	require.NoError(t, err, "el cambio ya está guardado; las cachés vencen solas")
}

func TestWriteFailureRollsBackAudit(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.store.failWrite = errors.New("bd")
	tenant := e.tenant("basico")
	_, err := app.NewSetTenantFlagHandler(e.deps, app.ActorPlatform).Handle(platform(tenant), app.SetTenantFlagCommand{Key: "module.claims", Enabled: on(), Reason: "add-on"})
	require.Error(t, err)
	assert.Empty(t, e.store.audits)
	assert.Empty(t, e.store.events)
	assert.Empty(t, e.inv.tenants)
}

func TestListTenantFlags(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tenant := e.tenant("profesional")
	_, err := app.NewSetTenantFlagHandler(e.deps, app.ActorPlatform).Handle(platform(tenant), app.SetTenantFlagCommand{Key: "module.commissions", Enabled: on(), Reason: "add-on comisiones"})
	require.NoError(t, err)

	list := app.NewListTenantFlagsHandler(e.deps)
	_, err = list.Handle(as(t, tenant, authz.RoleSalesAgent))
	require.ErrorIs(t, err, authz.ErrForbidden)

	views, err := list.Handle(as(t, tenant, authz.RoleManager))
	require.NoError(t, err)
	byKey := map[flags.Key]app.FlagView{}
	for _, v := range views {
		byKey[v.Definition.Key] = v
	}
	assert.Len(t, views, 6)
	assert.Equal(t, flags.Resolution{Enabled: true, Source: flags.SourceOverride}, byKey["module.commissions"].Resolution)
	require.NotNil(t, byKey["module.commissions"].Override)
	assert.Equal(t, "add-on comisiones", byKey["module.commissions"].Override.Reason)
	assert.Equal(t, flags.SourcePlan, byKey["module.claims"].Resolution.Source)
	assert.Nil(t, byKey["module.claims"].Override)

	other := e.tenant("profesional")
	views, err = list.Handle(as(t, other, authz.RoleManager))
	require.NoError(t, err)
	for _, v := range views {
		assert.Nil(t, v.Override, "el otro tenant no ve overrides ajenos")
	}
}

// ADR 0012: apagar globalmente un flag de control exige repetir su clave; restaurarlo no.
func TestKillOfControlFlagRequiresConfirmation(t *testing.T) {
	t.Parallel()
	reg, err := flags.ParseRegistry([]byte("flags:\n  - {key: module.compliance, kind: module, description: d, owner: x, control: true}\n"))
	require.NoError(t, err)
	store := &fakeStore{overrides: map[kernel.TenantID]map[flags.Key]app.Override{}, global: map[flags.Key]bool{}}
	clock := kernel.NewFakeClock(time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC))
	deps := app.NewDeps(reg, store, fakeTenants{plans: map[kernel.TenantID]string{}}, &fakeInvalidator{}, clock, slog.New(slog.DiscardHandler))
	h := app.NewSetGlobalSwitchHandler(deps)
	ctx := context.Background()

	cmd := app.SetGlobalSwitchCommand{Key: "module.compliance", Enabled: off(), Reason: "incidente", Operator: "ana.ops"}
	require.ErrorIs(t, h.Handle(ctx, cmd), domain.ErrControlConfirmationRequired)
	_, killed := store.global["module.compliance"]
	assert.False(t, killed, "sin confirmación no cambia nada")

	cmd.ConfirmControl = "module.otro"
	require.ErrorIs(t, h.Handle(ctx, cmd), domain.ErrControlConfirmationRequired, "la clave debe coincidir")

	cmd.ConfirmControl = "module.compliance"
	require.NoError(t, h.Handle(ctx, cmd))
	assert.False(t, store.global["module.compliance"])

	require.NoError(t, h.Handle(ctx, app.SetGlobalSwitchCommand{Key: "module.compliance", Reason: "resuelto", Operator: "ana.ops"}), "restaurar enciende el control: no pide confirmación")
}

func TestTurningOffAControlFlagForATenantNeedsConfirmation(t *testing.T) {
	t.Parallel()
	reg, err := flags.ParseRegistry([]byte("flags:\n  - {key: module.compliance, kind: module, description: d, owner: x, control: true}\n"))
	require.NoError(t, err)
	store := &fakeStore{overrides: map[kernel.TenantID]map[flags.Key]app.Override{}, global: map[flags.Key]bool{}}
	tenant := kernel.TenantID(uuid.Must(uuid.NewV7()))
	clock := kernel.NewFakeClock(time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC))
	deps := app.NewDeps(reg, store, fakeTenants{plans: map[kernel.TenantID]string{tenant: "basico"}}, &fakeInvalidator{}, clock, slog.New(slog.DiscardHandler))
	h := app.NewSetTenantFlagHandler(deps, app.ActorPlatform)
	ctx := platform(tenant)
	_, err = h.Handle(ctx, app.SetTenantFlagCommand{Key: "module.compliance", Enabled: on(), Reason: "contrató cumplimiento"})
	require.NoError(t, err)
	_, err = h.Handle(ctx, app.SetTenantFlagCommand{Key: "module.compliance", Reason: "limpiar override"})
	require.ErrorIs(t, err, domain.ErrControlConfirmationRequired, "limpiar también lo apaga")
	cmd := app.SetTenantFlagCommand{Key: "module.compliance", Enabled: off(), Reason: "piloto sin SARLAFT"}
	_, err = h.Handle(ctx, cmd)
	require.ErrorIs(t, err, domain.ErrControlConfirmationRequired)
	cmd.ConfirmControl = "module.compliance"
	_, err = h.Handle(ctx, cmd)
	require.ErrorIs(t, err, domain.ErrInvalidOperator)
	cmd.Operator = "ana.ops"
	_, err = h.Handle(ctx, cmd)
	require.NoError(t, err)
}

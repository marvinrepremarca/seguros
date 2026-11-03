package flags_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// --- registro ---

func TestEmbeddedRegistryIsValid(t *testing.T) {
	t.Parallel()
	reg, err := flags.LoadRegistry()
	require.NoError(t, err)
	d, ok := reg.Get(flags.CommissionsModule)
	require.True(t, ok)
	assert.Equal(t, []flags.Key{flags.BillingModule}, d.Requires, "comisiones requiere cartera (DECISIONS N3)")
	assert.Equal(t, []flags.Key{flags.CommissionsModule}, reg.Dependents(flags.BillingModule))
	for _, k := range reg.Keys() {
		def, _ := reg.Get(k)
		assert.Equal(t, flags.KindModule, def.Kind, "Fase 1 solo registra módulos")
	}
}

func TestNoExpiredReleaseFlags(t *testing.T) {
	t.Parallel()
	reg, err := flags.LoadRegistry()
	require.NoError(t, err)
	today := kernel.Today(kernel.SystemClock{})
	assert.Empty(t, reg.ExpiredReleases(today), "retira los flags release vencidos (rules/feature-flags.md)")
}

func TestGeneratedFilesAreUpToDate(t *testing.T) {
	t.Parallel()
	reg, err := flags.LoadRegistry()
	require.NoError(t, err)
	wantGo, err := flags.RenderGo(reg)
	require.NoError(t, err)
	gotGo, err := os.ReadFile(flags.GeneratedGoFile)
	require.NoError(t, err)
	assert.Equal(t, string(wantGo), string(gotGo), "ejecuta make generate")
	gotTS, err := os.ReadFile(flags.GeneratedTSFile)
	require.NoError(t, err)
	assert.Equal(t, string(flags.RenderTS(reg)), string(gotTS), "ejecuta make generate")
}

func TestParseRegistryRejects(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"campo desconocido":      "flags:\n  - {key: module.x, kind: module, description: d, owner: x, color: red}\n",
		"tipo desconocido":       "flags:\n  - {key: module.x, kind: toggle, description: d, owner: x}\n",
		"clave de módulo mala":   "flags:\n  - {key: claims, kind: module, description: d, owner: x}\n",
		"mayúsculas":             "flags:\n  - {key: module.Claims, kind: module, description: d, owner: x}\n",
		"funcionalidad con ops.": "flags:\n  - {key: ops.x, kind: feature, description: d, owner: x}\n",
		"sin descripción":        "flags:\n  - {key: module.x, kind: module, owner: x}\n",
		"sin dueño":              "flags:\n  - {key: module.x, kind: module, description: d}\n",
		"plan desconocido":       "flags:\n  - {key: module.x, kind: module, description: d, owner: x, plans: [oro]}\n",
		"duplicado":              "flags:\n  - {key: module.x, kind: module, description: d, owner: x}\n  - {key: module.x, kind: module, description: d, owner: x}\n",
		"requiere inexistente":   "flags:\n  - {key: module.x, kind: module, description: d, owner: x, requires: [module.y]}\n",
		"se requiere a sí mismo": "flags:\n  - {key: module.x, kind: module, description: d, owner: x, requires: [module.x]}\n",
		"ciclo": "flags:\n  - {key: module.x, kind: module, description: d, owner: x, requires: [module.y]}\n" +
			"  - {key: module.y, kind: module, description: d, owner: y, requires: [module.x]}\n",
		"release sin removeBy":      "flags:\n  - {key: release.x.y, kind: release, description: d, owner: x}\n",
		"removeBy fuera de release": "flags:\n  - {key: module.x, kind: module, description: d, owner: x, removeBy: 2030-01-01}\n",
		"módulo editable":           "flags:\n  - {key: module.x, kind: module, description: d, owner: x, tenantEditable: true}\n",
		"ops con plan":              "flags:\n  - {key: ops.integrations.x, kind: ops, description: d, owner: x, plans: [basico]}\n",
		"depende de release": "flags:\n  - {key: module.x, kind: module, description: d, owner: x, requires: [release.x.y]}\n" +
			"  - {key: release.x.y, kind: release, description: d, owner: x, removeBy: 2030-01-01}\n",
		"yaml roto": "flags: [",
	}
	for name, doc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := flags.ParseRegistry([]byte(doc))
			require.Error(t, err)
		})
	}
}

func TestExpiredReleases(t *testing.T) {
	t.Parallel()
	reg, err := flags.ParseRegistry([]byte("flags:\n  - {key: release.billing.new-x, kind: release, description: d, owner: billing, removeBy: 2026-11-01}\n"))
	require.NoError(t, err)
	before, err := kernel.NewDate(2026, time.October, 31)
	require.NoError(t, err)
	after, err := kernel.NewDate(2026, time.November, 2)
	require.NoError(t, err)
	assert.Empty(t, reg.ExpiredReleases(before))
	assert.Equal(t, []flags.Key{"release.billing.new-x"}, reg.ExpiredReleases(after))
}

func TestNames(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "ClaimsModule", flags.GoName("module.claims"))
	assert.Equal(t, "BillingPaymentReminders", flags.GoName("billing.payment-reminders"))
	assert.Equal(t, "OpsIntegrationsInsurerSura", flags.GoName("ops.integrations.insurer-sura"))
	assert.Equal(t, "ReleaseCommissionsAutoReconciliation", flags.GoName("release.commissions.auto-reconciliation"))
	assert.Equal(t, []string{"claims", "module"}, flags.TSPath("module.claims"))
	assert.Equal(t, []string{"billing", "paymentReminders"}, flags.TSPath("billing.payment-reminders"))
}

// --- evaluación ---

const testRegistry = `flags:
  - {key: module.billing, kind: module, description: d, owner: billing, plans: [basico, profesional]}
  - {key: module.commissions, kind: module, description: d, owner: commissions, plans: [empresarial], requires: [module.billing]}
  - {key: billing.reminders, kind: feature, description: d, owner: billing, default: true, tenantEditable: true}
  - {key: ops.integrations.insurer-x, kind: ops, description: d, owner: integrations, default: true}
`

func testReg(t *testing.T) *flags.Registry {
	t.Helper()
	reg, err := flags.ParseRegistry([]byte(testRegistry))
	require.NoError(t, err)
	return reg
}

func TestEvaluatePrecedence(t *testing.T) {
	t.Parallel()
	reg := testReg(t)
	tests := []struct {
		name  string
		state flags.State
		plan  string
		key   flags.Key
		want  flags.Resolution
	}{
		{"incluido en el plan", flags.State{}, "basico", "module.billing", flags.Resolution{Enabled: true, Source: flags.SourcePlan}},
		{"fuera del plan → default", flags.State{}, "empresarial", "module.billing", flags.Resolution{Enabled: false, Source: flags.SourceDefault}},
		{"override enciende (add-on)", flags.State{Overrides: map[flags.Key]bool{"module.billing": true}}, "empresarial", "module.billing", flags.Resolution{Enabled: true, Source: flags.SourceOverride}},
		{"override apaga", flags.State{Overrides: map[flags.Key]bool{"module.billing": false}}, "basico", "module.billing", flags.Resolution{Enabled: false, Source: flags.SourceOverride}},
		{"kill switch gana al override", flags.State{Overrides: map[flags.Key]bool{"module.billing": true}, Global: map[flags.Key]bool{"module.billing": false}}, "basico", "module.billing", flags.Resolution{Enabled: false, Source: flags.SourceKillSwitch}},
		{"global true no fuerza un módulo", flags.State{Global: map[flags.Key]bool{"module.billing": true}}, "empresarial", "module.billing", flags.Resolution{Enabled: false, Source: flags.SourceDefault}},
		{"dependencia apagada", flags.State{}, "empresarial", "module.commissions", flags.Resolution{Enabled: false, Source: flags.SourceDependency}},
		{"dependencia encendida", flags.State{Overrides: map[flags.Key]bool{"module.billing": true}}, "empresarial", "module.commissions", flags.Resolution{Enabled: true, Source: flags.SourcePlan}},
		{"default true", flags.State{}, "basico", "billing.reminders", flags.Resolution{Enabled: true, Source: flags.SourceDefault}},
		{"ops default", flags.State{}, "", "ops.integrations.insurer-x", flags.Resolution{Enabled: true, Source: flags.SourceDefault}},
		{"ops apagado globalmente", flags.State{Global: map[flags.Key]bool{"ops.integrations.insurer-x": false}}, "", "ops.integrations.insurer-x", flags.Resolution{Enabled: false, Source: flags.SourceKillSwitch}},
		{"ops ignora overrides de tenant", flags.State{Overrides: map[flags.Key]bool{"ops.integrations.insurer-x": false}}, "", "ops.integrations.insurer-x", flags.Resolution{Enabled: true, Source: flags.SourceDefault}},
		{"clave no registrada", flags.State{}, "basico", "module.nada", flags.Resolution{Enabled: false, Source: flags.SourceError}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, flags.Evaluate(reg, tt.state, tt.plan, tt.key))
		})
	}
}

type fakeStore struct {
	mu        sync.Mutex
	overrides map[kernel.TenantID]map[flags.Key]bool
	global    map[flags.Key]bool
	err       error
	calls     int
}

func (s *fakeStore) TenantOverrides(_ context.Context, t kernel.TenantID) (map[flags.Key]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	out := map[flags.Key]bool{}
	for k, v := range s.overrides[t] {
		out[k] = v
	}
	return out, nil
}

func (s *fakeStore) GlobalSwitches(context.Context) (map[flags.Key]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	return s.global, nil
}

func (s *fakeStore) set(t kernel.TenantID, k flags.Key, v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.overrides[t] == nil {
		s.overrides[t] = map[flags.Key]bool{}
	}
	s.overrides[t][k] = v
}

func (s *fakeStore) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func newTenant() kernel.TenantID { return kernel.TenantID(uuid.Must(uuid.NewV7())) }

func setup(t *testing.T) (*flags.Evaluator, *fakeStore, *kernel.FakeClock) {
	t.Helper()
	store := &fakeStore{overrides: map[kernel.TenantID]map[flags.Key]bool{}}
	clock := kernel.NewFakeClock(time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC))
	return flags.NewEvaluator(testReg(t), store, nil, clock, slog.New(slog.DiscardHandler)), store, clock
}

func tenantCtx(tenant kernel.TenantID, plan string) context.Context {
	return reqctx.WithTenantPlan(reqctx.WithTenantID(context.Background(), tenant), plan)
}

func TestTenantIsolation(t *testing.T) {
	t.Parallel()
	eval, store, _ := setup(t)
	a, b := newTenant(), newTenant()
	store.set(a, "module.billing", true)
	store.set(b, "module.billing", false)
	assert.True(t, eval.IsEnabled(tenantCtx(a, "empresarial"), "module.billing"))
	assert.False(t, eval.IsEnabled(tenantCtx(b, "empresarial"), "module.billing"), "B no hereda el override de A")
}

func TestL1CacheExpiresAndInvalidates(t *testing.T) {
	t.Parallel()
	eval, store, clock := setup(t)
	a := newTenant()
	ctx := tenantCtx(a, "empresarial")

	assert.False(t, eval.IsEnabled(ctx, "module.billing"))
	store.set(a, "module.billing", true)
	assert.False(t, eval.IsEnabled(ctx, "module.billing"), "L1 vigente")

	clock.Advance(31 * time.Second)
	assert.True(t, eval.IsEnabled(ctx, "module.billing"), "L1 vencida → se relee")

	store.set(a, "module.billing", false)
	require.NoError(t, eval.Invalidate(context.Background(), a))
	assert.False(t, eval.IsEnabled(ctx, "module.billing"), "invalidación inmediata")
}

func TestFailClosedAndLastKnownValue(t *testing.T) {
	t.Parallel()
	eval, store, clock := setup(t)
	a, b := newTenant(), newTenant()
	store.set(a, "module.billing", true)
	assert.True(t, eval.IsEnabled(tenantCtx(a, "empresarial"), "module.billing"))

	store.fail(errors.New("bd caída"))
	clock.Advance(time.Minute)
	assert.True(t, eval.IsEnabled(tenantCtx(a, "empresarial"), "module.billing"), "usa el último valor conocido")
	assert.False(t, eval.IsEnabled(tenantCtx(b, "basico"), "module.billing"), "sin valor conocido → apagado aunque el plan lo incluya")
}

func TestEvaluationWithoutTenant(t *testing.T) {
	t.Parallel()
	eval, _, _ := setup(t)
	assert.False(t, eval.IsEnabled(context.Background(), "module.billing"), "nunca se evalúa sin tenant")
	assert.True(t, eval.IsEnabled(context.Background(), "ops.integrations.insurer-x"), "los flags ops son globales")
	_, err := eval.Snapshot(context.Background(), kernel.TenantID{}, "")
	require.ErrorIs(t, err, flags.ErrNoTenant)
}

func TestSnapshot(t *testing.T) {
	t.Parallel()
	eval, _, _ := setup(t)
	snap, err := eval.Snapshot(context.Background(), newTenant(), "basico")
	require.NoError(t, err)
	assert.Len(t, snap, 4)
	assert.True(t, snap["module.billing"].Enabled)
	assert.False(t, snap["module.commissions"].Enabled)
}

func TestRequireMiddleware(t *testing.T) {
	t.Parallel()
	eval, store, _ := setup(t)
	on, off := newTenant(), newTenant()
	store.set(on, "module.billing", true)
	h := flags.Require(eval, "module.billing")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	call := func(ctx context.Context) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/billing/installments", nil))
		return rec.Code
	}
	assert.Equal(t, http.StatusOK, call(tenantCtx(on, "empresarial")), "flag ON")
	assert.Equal(t, http.StatusNotFound, call(tenantCtx(off, "empresarial")), "flag OFF → 404, no 403")
	assert.Equal(t, http.StatusUnauthorized, call(context.Background()), "sin tenant")
}

// --- OpenFeature ---

func TestOpenFeatureProvider(t *testing.T) {
	t.Parallel()
	eval, store, _ := setup(t)
	a := newTenant()
	store.set(a, "module.billing", true)
	domain := "test-" + uuid.NewString()
	require.NoError(t, openfeature.SetNamedProviderAndWait(domain, flags.NewProvider(eval)))
	checker := flags.NewOpenFeatureChecker(openfeature.NewClient(domain))

	policy, err := authz.DefaultPolicy()
	require.NoError(t, err)
	p := authz.NewPrincipal(kernel.UserID(uuid.Must(uuid.NewV7())), a, []authz.Role{authz.RoleManager}, policy)
	ctx := reqctx.WithTenantPlan(authz.WithPrincipal(context.Background(), p), "empresarial")

	assert.True(t, checker.IsEnabled(ctx, "module.billing"))
	assert.True(t, checker.IsEnabled(ctx, "module.commissions"), "plan empresarial y billing encendido por override")
	assert.False(t, checker.IsEnabled(ctx, "module.inexistente"), "no registrado → default false")
	assert.False(t, checker.IsEnabled(context.Background(), "module.billing"), "sin tenant → false")
	assert.True(t, checker.IsEnabled(context.Background(), "ops.integrations.insurer-x"))

	ec := flags.EvaluationContext(ctx)
	attrs := ec.Attributes()
	assert.Equal(t, a.String(), ec.TargetingKey())
	assert.Equal(t, "empresarial", attrs[flags.AttrPlan])
	assert.Equal(t, kernel.IDString(p.UserID), attrs[flags.AttrUserID])
	assert.Equal(t, []string{"Manager"}, attrs[flags.AttrRoles])

	cli := openfeature.NewClient(domain)
	_, err = cli.StringValue(ctx, "module.billing", "x", flags.EvaluationContext(ctx))
	require.Error(t, err, "solo booleanos")
	det, err := cli.BooleanValueDetails(ctx, "module.billing", false, flags.EvaluationContext(ctx))
	require.NoError(t, err)
	assert.Equal(t, string(flags.SourceOverride), det.Variant)
}

//go:build integration

// Package integrationsapi prueba la capa de aseguradoras contra PostgreSQL real.
package integrationsapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/integrations"
	"github.com/marvinrepremarca/seguros/internal/modules/integrations/app"
	"github.com/marvinrepremarca/seguros/internal/modules/integrations/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
	"github.com/marvinrepremarca/seguros/test/pgtest"
)

//nolint:gochecknoglobals // compartidos por la suite (TestMain)
var (
	database *db.DB
	super    *pgx.Conn
)

func TestMain(m *testing.M) { os.Exit(runMain(m)) }

func runMain(m *testing.M) int {
	ctx := context.Background()
	env, err := pgtest.Start(ctx)
	if err != nil {
		return 1
	}
	defer env.Terminate() //nolint:errcheck // limpieza al final de la suite
	if err := env.MigrateAll(ctx); err != nil {
		return 1
	}
	if database, err = env.OpenApp(ctx); err != nil {
		return 1
	}
	defer database.Close()
	if super, err = pgx.Connect(ctx, env.SuperuserURL); err != nil {
		return 1
	}
	return m.Run()
}

type principalKey struct{}

type checker struct {
	off     map[kernel.TenantID]bool
	offKeys map[flags.Key]bool
}

func (c checker) IsEnabled(ctx context.Context, k flags.Key) bool {
	t, ok := reqctx.TenantID(ctx)
	return ok && !c.off[t] && !c.offKeys[k]
}

type failing struct{ calls int }

func (f *failing) Quote(context.Context, domain.QuoteQuery) (domain.QuoteResult, error) {
	f.calls++
	return domain.QuoteResult{}, errors.New("portal caído, token=secreto")
}

type env struct {
	router *chi.Mux
	policy *authz.Policy
	check  checker
	broken *failing
}

func newEnv(t *testing.T) *env {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	clock := kernel.NewFakeClock(time.Now().UTC()) //nolint:forbidigo // igual que el resto de suites
	policy, err := authz.DefaultPolicy()
	require.NoError(t, err)
	e := &env{policy: policy, check: checker{off: map[kernel.TenantID]bool{}, offKeys: map[flags.Key]bool{}}, broken: &failing{}}
	connectors := []app.Connector{
		integrations.SimulatorConnector(),
		{Key: "broken", Name: "Caída", KillSwitch: flags.IntegrationsModule, Gateway: e.broken},
	}
	mod := integrations.New(integrations.Deps{DB: database, Connectors: connectors, Checker: e.check, Policy: policy, Clock: clock, Logger: logger})
	root := httpx.NewRouter(httpx.RouterOptions{Logger: logger, Clock: clock, MaxBodyBytes: 1 << 20})
	var mountErr error
	root.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ctx := req.Context()
				if p, ok := ctx.Value(principalKey{}).(authz.Principal); ok {
					ctx = reqctx.WithTenantPlan(authz.WithPrincipal(ctx, p), "empresarial")
				}
				next.ServeHTTP(w, req.WithContext(ctx))
			})
		})
		mountErr = mod.Mount(r)
	})
	require.NoError(t, mountErr)
	e.router = root
	return e
}

func (e *env) do(t *testing.T, tenant kernel.TenantID, role authz.Role, method, path string, body, out any) *httptest.ResponseRecorder {
	t.Helper()
	who := authz.NewPrincipal(kernel.UserID(uuid.Must(uuid.NewV7())), tenant, []authz.Role{role}, e.policy)
	var rd bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&rd).Encode(body))
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, &rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req.WithContext(context.WithValue(req.Context(), principalKey{}, who)))
	if out != nil && rec.Code < 300 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), out), rec.Body.String())
	}
	return rec
}

func newTenant() kernel.TenantID { return kernel.TenantID(uuid.Must(uuid.NewV7())) }

func query() map[string]any {
	return map[string]any{"lineOfBusiness": "autos", "sumInsured": "1000000"}
}

const simQuotes = "/api/v1/integrations/connectors/simulator/quotes"

func TestSimulatorMarksResultAndLogsCall(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	var r struct {
		NetPremium string `json:"netPremium"`
		Simulated  bool   `json:"simulated"`
		Reference  string `json:"reference"`
	}
	rec := e.do(t, tenant, authz.RoleOperations, http.MethodPost, simQuotes, query(), &r)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "50000", r.NetPremium)
	assert.True(t, r.Simulated)
	var n int
	require.NoError(t, super.QueryRow(context.Background(), `SELECT count(*) FROM integrations.calls WHERE tenant_id = $1 AND connector = 'simulator' AND outcome = 'success'`, uuid.UUID(tenant)).Scan(&n))
	assert.Equal(t, 1, n)
	require.NoError(t, super.QueryRow(context.Background(), `SELECT count(*) FROM integrations.calls WHERE tenant_id = $1`, uuid.UUID(newTenant())).Scan(&n))
	assert.Zero(t, n)
}

func TestKillSwitchStopsCalls(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	e.check.offKeys[flags.OpsIntegrationsSimulator] = true
	assert.Equal(t, http.StatusServiceUnavailable, e.do(t, tenant, authz.RoleOperations, http.MethodPost, simQuotes, query(), nil).Code)
	var list []struct {
		Key     string `json:"key"`
		Enabled bool   `json:"enabled"`
	}
	require.Equal(t, http.StatusOK, e.do(t, tenant, authz.RoleOperations, http.MethodGet, "/api/v1/integrations/connectors", nil, &list).Code)
	require.Len(t, list, 2)
	assert.Equal(t, "simulator", list[0].Key)
	assert.False(t, list[0].Enabled)
}

func TestFailureHidesDetailAndBreakerOpens(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	path := "/api/v1/integrations/connectors/broken/quotes"
	for range 8 {
		rec := e.do(t, tenant, authz.RoleOperations, http.MethodPost, path, query(), nil)
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.NotContains(t, rec.Body.String(), "secreto")
	}
	assert.Equal(t, 6, e.broken.calls, "el circuito se abre tras más de 5 fallos seguidos")
}

func TestValidationPermissionsAndFlag(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	assert.Equal(t, http.StatusUnprocessableEntity, e.do(t, tenant, authz.RoleOperations, http.MethodPost, simQuotes, map[string]any{"lineOfBusiness": "autos", "sumInsured": "0"}, nil).Code)
	assert.Equal(t, http.StatusNotFound, e.do(t, tenant, authz.RoleOperations, http.MethodPost, "/api/v1/integrations/connectors/nada/quotes", query(), nil).Code)
	assert.Equal(t, http.StatusForbidden, e.do(t, tenant, authz.RoleSalesAgent, http.MethodPost, simQuotes, query(), nil).Code)
	off := newTenant()
	e.check.off[off] = true
	assert.Equal(t, http.StatusNotFound, e.do(t, off, authz.RoleOperations, http.MethodGet, "/api/v1/integrations/connectors", nil, nil).Code)
}

//go:build integration

// Package complianceapi prueba cumplimiento contra PostgreSQL real.
package complianceapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/compliance"
	compliancehttp "github.com/marvinrepremarca/seguros/internal/modules/compliance/adapters/http"
	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
	"github.com/marvinrepremarca/seguros/test/contracttest"
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

type checker struct{ off map[kernel.TenantID]bool }

func (c checker) IsEnabled(ctx context.Context, _ flags.Key) bool {
	t, ok := reqctx.TenantID(ctx)
	return ok && !c.off[t]
}

// view simula a parties: la huella cambia al rectificar el documento. Un tercero con dueño solo
// existe para su tenant, como bajo RLS.
type view struct {
	prints map[uuid.UUID][]byte
	owner  map[uuid.UUID]kernel.TenantID
}

func (v *view) visible(ctx context.Context, p uuid.UUID) bool {
	t, ok := v.owner[p]
	if !ok {
		return true
	}
	current, _ := reqctx.TenantID(ctx)
	return current == t
}

func (v *view) DocumentFingerprint(ctx context.Context, p uuid.UUID) ([]byte, bool, error) {
	f, ok := v.prints[p]
	return f, ok && v.visible(ctx, p), nil
}

func (v *view) HabeasExport(ctx context.Context, p uuid.UUID) (parties.HabeasRecord, bool, error) {
	_, ok := v.prints[p]
	return parties.HabeasRecord{Kind: "person", Name: "Ana", DocumentType: "CC", DocumentLast4: "1234"}, ok && v.visible(ctx, p), nil
}

type cipher struct{}

func reversed(b []byte) []byte {
	out := slices.Clone(b)
	slices.Reverse(out)
	return out
}

func (cipher) EncryptFor(_ context.Context, _, _ string, p []byte) ([]byte, error) {
	return reversed(p), nil
}

func (cipher) DecryptFor(_ context.Context, _, _ string, c []byte) ([]byte, error) {
	return reversed(c), nil
}

type env struct {
	router *chi.Mux
	policy *authz.Policy
	off    map[kernel.TenantID]bool
	v      *view
	mod    *compliance.Module
}

func newEnv(t *testing.T) *env {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	clock := kernel.NewFakeClock(time.Now().UTC()) //nolint:forbidigo // partición de auditoría del mes actual
	policy, err := authz.DefaultPolicy()
	require.NoError(t, err)
	e := &env{policy: policy, off: map[kernel.TenantID]bool{}, v: &view{prints: map[uuid.UUID][]byte{}, owner: map[uuid.UUID]kernel.TenantID{}}}
	e.mod = compliance.New(compliance.Deps{DB: database, Audit: audit.NewRecorder(clock), Outbox: outbox.NewWriter(clock), Parties: e.v, Cipher: cipher{}, Checker: checker{off: e.off}, Policy: policy, Clock: clock, Logger: logger})
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
		mountErr = e.mod.Mount(r)
	})
	require.NoError(t, mountErr)
	e.router = root
	return e
}

func (e *env) as(tenant kernel.TenantID, roles ...authz.Role) authz.Principal {
	return authz.NewPrincipal(kernel.UserID(uuid.Must(uuid.NewV7())), tenant, roles, e.policy)
}

func (e *env) do(t *testing.T, who authz.Principal, method, path string, body, out any) int {
	t.Helper()
	ctx := context.WithValue(context.Background(), principalKey{}, who)
	var rd bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&rd).Encode(body))
	}
	req := httptest.NewRequestWithContext(ctx, method, path, &rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	contracttest.New(t, compliancehttp.GetSpec).Check(t, req, rec)
	if out != nil && rec.Code < 300 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), out), rec.Body.String())
	}
	return rec.Code
}

func newTenant() kernel.TenantID { return kernel.TenantID(uuid.Must(uuid.NewV7())) }

func count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, super.QueryRow(context.Background(), sql, args...).Scan(&n))
	return n
}

// El KYC gobierna la expedición: vale el último, y solo para el documento verificado (N4, ADR 0012).
func TestKycBindsToCurrentDocument(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	officer := e.as(tenant, authz.RoleComplianceOfficer)
	party := uuid.Must(uuid.NewV7())
	e.v.prints[party] = []byte{1}
	ctx := reqctx.WithTenantID(context.Background(), tenant)
	base := "/api/v1/compliance/parties/" + party.String() + "/kyc"
	today := time.Now().UTC().Format("2006-01-02") //nolint:forbidigo // el reloj falso parte de ahora

	approved := func() bool {
		ok, err := e.mod.Statuses.IsApproved(ctx, party)
		require.NoError(t, err)
		return ok
	}
	assert.False(t, approved(), "sin verificaciones")
	assert.Equal(t, http.StatusForbidden, e.do(t, e.as(tenant, authz.RoleOperations), http.MethodPost, base, map[string]any{"approved": true, "source": "ONU", "checkedOn": today}, nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, officer, http.MethodPost, "/api/v1/compliance/parties/"+uuid.NewString()+"/kyc", map[string]any{"approved": true, "source": "ONU", "checkedOn": today}, nil))
	assert.Equal(t, http.StatusUnprocessableEntity, e.do(t, officer, http.MethodPost, base, map[string]any{"approved": true, "source": "ONU", "checkedOn": "2999-01-01"}, nil))

	var check struct {
		ID string `json:"id"`
	}
	secret := "Coincidencia parcial revisada"
	require.Equal(t, http.StatusCreated, e.do(t, officer, http.MethodPost, base, map[string]any{"approved": true, "source": "ONU", "checkedOn": today, "details": secret}, &check))
	assert.True(t, approved())
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM messaging.outbox WHERE tenant_id = $1 AND topic = 'compliance.kyc_check_completed'`, uuid.UUID(tenant)))

	var stored []byte
	require.NoError(t, super.QueryRow(context.Background(), `SELECT details_ciphertext FROM compliance.kyc_checks WHERE id = $1`, check.ID).Scan(&stored))
	assert.NotContains(t, string(stored), secret)
	var details struct {
		Details string `json:"details"`
	}
	assert.Equal(t, http.StatusForbidden, e.do(t, e.as(tenant, authz.RoleOperations), http.MethodPost, base+"/"+check.ID+"/details", nil, nil))
	require.Equal(t, http.StatusOK, e.do(t, officer, http.MethodPost, base+"/"+check.ID+"/details", nil, &details))
	assert.Equal(t, secret, details.Details)
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM audit.events WHERE tenant_id = $1 AND action = 'compliance.kyc.details_read'`, uuid.UUID(tenant)))

	// Otro tenant no ve la verificación, aunque el tercero exista para el fake.
	var other struct {
		Checks []any `json:"checks"`
	}
	require.Equal(t, http.StatusOK, e.do(t, e.as(newTenant(), authz.RoleComplianceOfficer), http.MethodGet, base, nil, &other))
	assert.Empty(t, other.Checks)

	// Rectificar el documento invalida el KYC.
	e.v.prints[party] = []byte{2}
	assert.False(t, approved())
	var st struct {
		Approved bool `json:"approved"`
		Stale    bool `json:"stale"`
	}
	require.Equal(t, http.StatusOK, e.do(t, officer, http.MethodGet, base, nil, &st))
	assert.False(t, st.Approved)
	assert.True(t, st.Stale)

	// Vale la más reciente: un rechazo posterior bloquea.
	require.Equal(t, http.StatusCreated, e.do(t, officer, http.MethodPost, base, map[string]any{"approved": true, "source": "ONU", "checkedOn": today}, nil))
	assert.True(t, approved())
	require.Equal(t, http.StatusCreated, e.do(t, officer, http.MethodPost, base, map[string]any{"approved": false, "source": "OFAC", "checkedOn": today}, nil))
	assert.False(t, approved())
}

func TestFlagOffAndPermissions(t *testing.T) {
	e := newEnv(t)
	off := newTenant()
	e.off[off] = true
	assert.Equal(t, http.StatusNotFound, e.do(t, e.as(off, authz.RoleComplianceOfficer), http.MethodGet, "/api/v1/compliance/complaints", nil, nil))
	assert.Equal(t, http.StatusForbidden, e.do(t, e.as(newTenant(), authz.RoleSalesAgent), http.MethodGet, "/api/v1/compliance/complaints", nil, nil))
}

func TestComplaintLifecycle(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	officer := e.as(tenant, authz.RoleComplianceOfficer)
	today := time.Now().UTC().Format("2006-01-02") //nolint:forbidigo // el reloj falso parte de ahora
	var c struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	require.Equal(t, http.StatusCreated, e.do(t, officer, http.MethodPost, "/api/v1/compliance/complaints", map[string]any{"channel": "Correo", "subject": "Demora", "description": "Sin respuesta", "receivedOn": today}, &c))
	base := "/api/v1/compliance/complaints/" + c.ID
	assert.Equal(t, http.StatusNotFound, e.do(t, e.as(newTenant(), authz.RoleComplianceOfficer), http.MethodPost, base+"/start", nil, nil))
	require.Equal(t, http.StatusOK, e.do(t, officer, http.MethodPost, base+"/start", nil, nil))
	assert.Equal(t, http.StatusConflict, e.do(t, officer, http.MethodPost, base+"/start", nil, nil))
	require.Equal(t, http.StatusOK, e.do(t, officer, http.MethodPost, base+"/resolve", map[string]any{"resolution": "Atendida"}, &c))
	assert.Equal(t, "resolved", c.Status)
}

// El exporte queda auditado; la supresión se registra y no borra nada (N33).
func TestHabeas(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	officer := e.as(tenant, authz.RoleComplianceOfficer)
	party := uuid.Must(uuid.NewV7())
	e.v.prints[party] = []byte{1}
	base := "/api/v1/compliance/parties/" + party.String()
	var rec struct {
		DocumentLast4 string `json:"documentLast4"`
	}
	require.Equal(t, http.StatusOK, e.do(t, officer, http.MethodPost, base+"/habeas-export", nil, &rec))
	assert.Equal(t, "1234", rec.DocumentLast4)
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM audit.events WHERE tenant_id = $1 AND action = 'compliance.habeas.exported'`, uuid.UUID(tenant)))
	var created struct {
		ID string `json:"id"`
	}
	require.Equal(t, http.StatusCreated, e.do(t, officer, http.MethodPost, base+"/suppression-requests", nil, &created))
	resolve := "/api/v1/compliance/suppression-requests/" + created.ID + "/resolve"
	require.Equal(t, http.StatusNoContent, e.do(t, officer, http.MethodPost, resolve, map[string]any{"accepted": false, "resolution": "Retención legal"}, nil))
	assert.Equal(t, http.StatusConflict, e.do(t, officer, http.MethodPost, resolve, map[string]any{"accepted": true, "resolution": "Otra"}, nil))
}

// El resultado y la fuente se guardan cifrados (auditoría de la Fase 5).
func TestKycResultIsStoredSealed(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	party := uuid.Must(uuid.NewV7())
	e.v.prints[party] = []byte{1}
	today := time.Now().UTC().Format("2006-01-02") //nolint:forbidigo // el reloj falso parte de ahora
	var check struct {
		ID string `json:"id"`
	}
	require.Equal(t, http.StatusCreated, e.do(t, e.as(tenant, authz.RoleComplianceOfficer), http.MethodPost, "/api/v1/compliance/parties/"+party.String()+"/kyc", map[string]any{"approved": true, "source": "Lista ONU", "checkedOn": today}, &check))
	var plain int
	require.NoError(t, super.QueryRow(context.Background(), `SELECT count(*) FROM compliance.kyc_checks WHERE id = $1 AND result IS NULL AND source IS NULL AND position('ONU' in convert_from(source_ciphertext, 'LATIN1')) = 0`, check.ID).Scan(&plain))
	assert.Equal(t, 1, plain)
	var st struct {
		Approved bool `json:"approved"`
		Checks   []struct {
			Source string `json:"source"`
		} `json:"checks"`
	}
	require.Equal(t, http.StatusOK, e.do(t, e.as(tenant, authz.RoleOperations), http.MethodGet, "/api/v1/compliance/parties/"+party.String()+"/kyc", nil, &st))
	assert.True(t, st.Approved)
	require.Len(t, st.Checks, 1)
	assert.Equal(t, "Lista ONU", st.Checks[0].Source)
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM audit.events WHERE tenant_id = $1 AND action = 'compliance.kyc.history_read'`, uuid.UUID(tenant)))
}

// Cada operación sobre datos de otro tenant responde 404 (rules/testing.md).
func TestOtherTenantGetsNotFound(t *testing.T) {
	e := newEnv(t)
	tenant, other := newTenant(), newTenant()
	officer, stranger := e.as(tenant, authz.RoleComplianceOfficer), e.as(other, authz.RoleComplianceOfficer)
	party := uuid.Must(uuid.NewV7())
	e.v.prints[party] = []byte{1}
	e.v.owner[party] = tenant
	today := time.Now().UTC().Format("2006-01-02") //nolint:forbidigo // el reloj falso parte de ahora
	base := "/api/v1/compliance/parties/" + party.String()
	var check, supp, complaint struct {
		ID string `json:"id"`
	}
	require.Equal(t, http.StatusCreated, e.do(t, officer, http.MethodPost, base+"/kyc", map[string]any{"approved": false, "source": "OFAC", "checkedOn": today, "details": "Coincidencia"}, &check))
	require.Equal(t, http.StatusCreated, e.do(t, officer, http.MethodPost, base+"/suppression-requests", nil, &supp))
	require.Equal(t, http.StatusCreated, e.do(t, officer, http.MethodPost, "/api/v1/compliance/complaints", map[string]any{"partyId": party.String(), "channel": "Correo", "subject": "Demora", "description": "Sin respuesta", "receivedOn": today}, &complaint))

	assert.Equal(t, http.StatusNotFound, e.do(t, stranger, http.MethodGet, base+"/kyc", nil, nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, stranger, http.MethodPost, base+"/kyc", map[string]any{"approved": true, "source": "ONU", "checkedOn": today}, nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, stranger, http.MethodPost, base+"/kyc/"+check.ID+"/details", nil, nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, stranger, http.MethodPost, base+"/habeas-export", nil, nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, stranger, http.MethodPost, base+"/suppression-requests", nil, nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, stranger, http.MethodPost, "/api/v1/compliance/suppression-requests/"+supp.ID+"/resolve", map[string]any{"accepted": true, "resolution": "Aceptada"}, nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, stranger, http.MethodPost, "/api/v1/compliance/complaints/"+complaint.ID+"/resolve", map[string]any{"resolution": "Atendida"}, nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, stranger, http.MethodPost, "/api/v1/compliance/complaints", map[string]any{"partyId": party.String(), "channel": "Correo", "subject": "Ajena", "description": "Otro tenant", "receivedOn": today}, nil))
	var page struct {
		Items []any `json:"items"`
	}
	require.Equal(t, http.StatusOK, e.do(t, stranger, http.MethodGet, "/api/v1/compliance/suppression-requests", nil, &page))
	assert.Empty(t, page.Items)
	require.Equal(t, http.StatusOK, e.do(t, stranger, http.MethodGet, "/api/v1/compliance/complaints", nil, &page))
	assert.Empty(t, page.Items)
}

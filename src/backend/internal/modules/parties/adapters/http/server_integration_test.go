//go:build integration

package partieshttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

	"github.com/marvinrepremarca/seguros/internal/modules/parties"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/crypto"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
	"github.com/marvinrepremarca/seguros/test/pgtest"
)

var database *db.DB //nolint:gochecknoglobals // compartido por la suite (TestMain)

func TestMain(m *testing.M) { os.Exit(runMain(m)) }

func runMain(m *testing.M) int {
	ctx := context.Background()
	env, err := pgtest.Start(ctx)
	if err != nil {
		slog.Error("iniciando PostgreSQL", "error", err)
		return 1
	}
	defer env.Terminate() //nolint:errcheck // limpieza al final de la suite
	if err := env.MigrateAll(ctx); err != nil {
		slog.Error("migrando", "error", err)
		return 1
	}
	database, err = env.OpenApp(ctx)
	if err != nil {
		slog.Error("abriendo BD", "error", err)
		return 1
	}
	defer database.Close()
	return m.Run()
}

type principalKey struct{}

type env struct {
	router *chi.Mux
	policy *authz.Policy
}

func newEnv(t *testing.T) *env {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	clock := kernel.NewFakeClock(time.Now().UTC()) //nolint:forbidigo // partición de auditoría del mes actual
	policy, err := authz.DefaultPolicy()
	require.NoError(t, err)
	kek, err := crypto.NewLocalKEK(map[string]string{"v1": "kek-de-prueba-de-al-menos-32-caracteres"}, "v1")
	require.NoError(t, err)
	cipher, err := crypto.NewService(database, kek, "indice-de-prueba-de-al-menos-32-caracteres", clock)
	require.NoError(t, err)
	mod := parties.New(parties.Deps{DB: database, Audit: audit.NewRecorder(clock), Cipher: cipher, Policy: policy, Clock: clock, Logger: logger})
	root := httpx.NewRouter(httpx.RouterOptions{Logger: logger, Clock: clock, MaxBodyBytes: 1 << 20})
	var mountErr error
	root.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ctx := req.Context()
				if p, ok := ctx.Value(principalKey{}).(authz.Principal); ok {
					ctx = authz.WithPrincipal(ctx, p)
				}
				next.ServeHTTP(w, req.WithContext(ctx))
			})
		})
		mountErr = mod.Mount(r)
	})
	require.NoError(t, mountErr)
	return &env{router: root, policy: policy}
}

func newTenant() kernel.TenantID { return kernel.TenantID(uuid.Must(uuid.NewV7())) }

func (e *env) as(tenant kernel.TenantID, roles ...authz.Role) *authz.Principal {
	p := authz.NewPrincipal(kernel.UserID(uuid.Must(uuid.NewV7())), tenant, roles, e.policy)
	return &p
}

type resp struct {
	status int
	body   map[string]any
}

func (e *env) do(t *testing.T, who *authz.Principal, method, path string, body any) resp {
	t.Helper()
	ctx := context.Background()
	if who != nil {
		ctx = context.WithValue(ctx, principalKey{}, *who)
	}
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
	out := resp{status: rec.Code, body: map[string]any{}}
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out.body), rec.Body.String())
	}
	return out
}

const base = "/api/v1/parties/parties"

func field(t *testing.T, r resp, name string) string {
	t.Helper()
	v, ok := r.body[name].(string)
	require.True(t, ok, "%s en %v", name, r.body)
	return v
}

func uniqueCC() string { return fmt.Sprintf("1%09d", uuid.New().ID()%1_000_000_000) }

func person(number string) map[string]any {
	return map[string]any{"kind": "person", "name": "Ana Pérez", "documentType": "CC", "documentNumber": number, "email": "ana@example.com"}
}

func TestCreateMasksDocumentAndEncryptsIt(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tenant := newTenant()
	ops := e.as(tenant, authz.RoleOperations)
	number := uniqueCC()

	created := e.do(t, ops, http.MethodPost, base, person(number))
	require.Equal(t, http.StatusCreated, created.status, created.body)
	assert.Equal(t, number[len(number)-4:], created.body["documentLast4"])
	raw, _ := json.Marshal(created.body)
	assert.NotContains(t, string(raw), number, "la respuesta nunca trae el número completo")

	id := field(t, created, "id")
	got := e.do(t, ops, http.MethodGet, base+"/"+id, nil)
	require.Equal(t, http.StatusOK, got.status)
	assert.Equal(t, "Ana Pérez", got.body["name"])

	ctx := reqctx.WithTenantID(context.Background(), tenant)
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		var ciphertext []byte
		if err := tx.QueryRow(ctx, `SELECT document_ciphertext FROM parties.parties WHERE id = $1`, uuid.MustParse(id)).Scan(&ciphertext); err != nil {
			return err
		}
		assert.NotContains(t, string(ciphertext), number, "en la BD el número está cifrado")
		return nil
	}))
}

func TestDocumentIsUniquePerTenant(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	number := uniqueCC()
	a := e.as(newTenant(), authz.RoleOperations)
	require.Equal(t, http.StatusCreated, e.do(t, a, http.MethodPost, base, person(number)).status)
	dup := e.do(t, a, http.MethodPost, base, person(number))
	assert.Equal(t, http.StatusConflict, dup.status)
	assert.Equal(t, "parties.party.document_taken", dup.body["type"])
	b := e.as(newTenant(), authz.RoleOperations)
	assert.Equal(t, http.StatusCreated, e.do(t, b, http.MethodPost, base, person(number)).status, "otro tenant puede tener el mismo cliente")
}

func TestValidationErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ops := e.as(newTenant(), authz.RoleOperations)

	assert.Equal(t, http.StatusBadRequest, e.do(t, ops, http.MethodPost, base, map[string]any{"kind": "person"}).status, "faltan campos")
	unknown := person(uniqueCC())
	unknown["tenantId"] = uuid.NewString()
	assert.Equal(t, http.StatusBadRequest, e.do(t, ops, http.MethodPost, base, unknown).status, "campo desconocido")

	bad := person("ABC")
	r := e.do(t, ops, http.MethodPost, base, bad)
	assert.Equal(t, http.StatusUnprocessableEntity, r.status)
	assert.Equal(t, "parties.party.invalid_document", r.body["type"])
	assert.NotContains(t, fmt.Sprint(r.body), "ABC", "el error no repite el documento")
}

func TestTenantIsolation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	a := e.as(newTenant(), authz.RoleOperations, authz.RoleComplianceOfficer)
	b := e.as(newTenant(), authz.RoleOperations, authz.RoleComplianceOfficer)
	number := uniqueCC()
	created := e.do(t, a, http.MethodPost, base, person(number))
	require.Equal(t, http.StatusCreated, created.status)
	id := field(t, created, "id")

	assert.Equal(t, http.StatusNotFound, e.do(t, b, http.MethodGet, base+"/"+id, nil).status)
	assert.Equal(t, http.StatusNotFound, e.do(t, b, http.MethodPost, base+"/"+id+"/document/reveal", nil).status)
	assert.Equal(t, http.StatusNotFound, e.do(t, b, http.MethodGet, base+"/"+id+"/consents", nil).status)
	assert.Equal(t, http.StatusNotFound, e.do(t, b, http.MethodPut, base+"/"+id, map[string]any{"name": "Otro", "version": 1}).status)
	assert.Equal(t, http.StatusNotFound, e.do(t, b, http.MethodPost, base+"/search-by-document", map[string]any{"documentType": "CC", "documentNumber": number}).status)
	assert.Empty(t, e.do(t, b, http.MethodGet, base, nil).body["items"])
}

func TestPermissions(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tenant := newTenant()
	ops := e.as(tenant, authz.RoleOperations)
	created := e.do(t, ops, http.MethodPost, base, person(uniqueCC()))
	require.Equal(t, http.StatusCreated, created.status)
	id := field(t, created, "id")

	admin := e.as(tenant, authz.RoleTenantAdmin) // configura; no ve clientes
	sales := e.as(tenant, authz.RoleSalesAgent)  // registra clientes; no revela documentos
	collections := e.as(tenant, authz.RoleCollections)
	cases := []struct {
		name         string
		who          *authz.Principal
		method, path string
		body         any
		want         int
	}{
		{"sin sesión", nil, http.MethodGet, base, nil, http.StatusUnauthorized},
		{"admin no lista", admin, http.MethodGet, base, nil, http.StatusForbidden},
		{"asesor no revela", sales, http.MethodPost, base + "/" + id + "/document/reveal", nil, http.StatusForbidden},
		{"cartera no crea", collections, http.MethodPost, base, person(uniqueCC()), http.StatusForbidden},
		{"cartera no rectifica", collections, http.MethodPut, base + "/" + id, map[string]any{"name": "Otro", "version": 1}, http.StatusForbidden},
		{"cartera no ve autorizaciones", collections, http.MethodGet, base + "/" + id + "/consents", nil, http.StatusForbidden},
		{"cartera lee", collections, http.MethodGet, base + "/" + id, nil, http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, e.do(t, c.who, c.method, c.path, c.body).status)
		})
	}
}

func auditActions(t *testing.T, tenant kernel.TenantID, action string) (n int) {
	t.Helper()
	ctx := reqctx.WithTenantID(context.Background(), tenant)
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit.events WHERE action = $1`, action).Scan(&n)
	}))
	return n
}

func TestRevealAndSearchAreAudited(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tenant := newTenant()
	ops := e.as(tenant, authz.RoleOperations)
	number := uniqueCC()
	created := e.do(t, ops, http.MethodPost, base, person(number))
	require.Equal(t, http.StatusCreated, created.status)
	id := field(t, created, "id")
	assert.Equal(t, 1, auditActions(t, tenant, "parties.party.created"))

	revealed := e.do(t, ops, http.MethodPost, base+"/"+id+"/document/reveal", nil)
	require.Equal(t, http.StatusOK, revealed.status)
	assert.Equal(t, number, revealed.body["documentNumber"])
	assert.Equal(t, 1, auditActions(t, tenant, "parties.document.revealed"))

	found := e.do(t, ops, http.MethodPost, base+"/search-by-document", map[string]any{"documentType": "CC", "documentNumber": number})
	require.Equal(t, http.StatusOK, found.status)
	assert.Equal(t, id, found.body["id"])
	missing := e.do(t, ops, http.MethodPost, base+"/search-by-document", map[string]any{"documentType": "CC", "documentNumber": "999111"})
	assert.Equal(t, http.StatusNotFound, missing.status)
	assert.Equal(t, 2, auditActions(t, tenant, "parties.party.document_searched"), "la búsqueda sin resultado también queda")

	ctx := reqctx.WithTenantID(context.Background(), tenant)
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		var n int
		err := tx.QueryRow(ctx, `SELECT count(*) FROM audit.events WHERE details::text LIKE '%' || $1 || '%' OR details::text LIKE '%999111%'`, number).Scan(&n)
		assert.Zero(t, n, "la bitácora no contiene el número")
		return err
	}))
}

// Copiar el documento cifrado de un tercero a otro no permite leerlo (el AAD incluye el ID).
func TestCiphertextIsBoundToItsRecord(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tenant := newTenant()
	ops := e.as(tenant, authz.RoleOperations)
	first := field(t, e.do(t, ops, http.MethodPost, base, person(uniqueCC())), "id")
	second := field(t, e.do(t, ops, http.MethodPost, base, person(uniqueCC())), "id")
	ctx := reqctx.WithTenantID(context.Background(), tenant)
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE parties.parties SET document_ciphertext = (SELECT document_ciphertext FROM parties.parties WHERE id = $1) WHERE id = $2`, uuid.MustParse(first), uuid.MustParse(second))
		return err
	}))
	assert.Equal(t, http.StatusInternalServerError, e.do(t, ops, http.MethodPost, base+"/"+second+"/document/reveal", nil).status)
}

func TestUpdateWithOptimisticConcurrencyAndDocumentChange(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ops := e.as(newTenant(), authz.RoleOperations)
	created := e.do(t, ops, http.MethodPost, base, person(uniqueCC()))
	id := field(t, created, "id")

	newNumber := uniqueCC()
	updated := e.do(t, ops, http.MethodPut, base+"/"+id, map[string]any{
		"version": 1, "name": "Ana María", "city": "Bogotá",
		"document": map[string]any{"documentType": "CC", "documentNumber": newNumber},
	})
	require.Equal(t, http.StatusOK, updated.status, updated.body)
	assert.InDelta(t, 2, updated.body["version"], 0)
	assert.Nil(t, updated.body["email"], "el PUT reemplaza: lo omitido queda vacío")

	stale := e.do(t, ops, http.MethodPut, base+"/"+id, map[string]any{"version": 1, "name": "Otra"})
	assert.Equal(t, http.StatusConflict, stale.status)
	assert.Equal(t, "parties.party.concurrent_update", stale.body["type"])

	revealed := e.do(t, ops, http.MethodPost, base+"/"+id+"/document/reveal", nil)
	assert.Equal(t, newNumber, revealed.body["documentNumber"])
}

func TestListPaginatesAndSearchesByName(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ops := e.as(newTenant(), authz.RoleOperations)
	for i := range 3 {
		p := person(uniqueCC())
		p["name"] = fmt.Sprintf("Cliente %d", i)
		require.Equal(t, http.StatusCreated, e.do(t, ops, http.MethodPost, base, p).status)
	}
	p := person(uniqueCC())
	p["name"] = "100% Seguro"
	require.Equal(t, http.StatusCreated, e.do(t, ops, http.MethodPost, base, p).status)

	first := e.do(t, ops, http.MethodGet, base+"?limit=3", nil)
	require.Equal(t, http.StatusOK, first.status)
	assert.Len(t, first.body["items"], 3)
	cursor := field(t, first, "nextCursor")
	second := e.do(t, ops, http.MethodGet, base+"?limit=3&cursor="+cursor, nil)
	assert.Len(t, second.body["items"], 1)
	assert.Nil(t, second.body["nextCursor"])

	assert.Len(t, e.do(t, ops, http.MethodGet, base+"?q=cliente", nil).body["items"], 3)
	assert.Len(t, e.do(t, ops, http.MethodGet, base+"?q=0%25", nil).body["items"], 1, "% es un carácter, no un comodín")
	assert.Equal(t, http.StatusUnprocessableEntity, e.do(t, ops, http.MethodGet, base+"?cursor=%23%23", nil).status)
}

func TestConsentsAreAppendOnly(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tenant := newTenant()
	ops := e.as(tenant, authz.RoleOperations)
	id := field(t, e.do(t, ops, http.MethodPost, base, person(uniqueCC())), "id")
	today := kernel.Today(kernel.SystemClock{})

	body := map[string]any{"status": "granted", "purpose": "Gestión de pólizas", "textVersion": "v1", "consentDate": today.String()}
	require.Equal(t, http.StatusCreated, e.do(t, ops, http.MethodPost, base+"/"+id+"/consents", body).status)
	body["status"] = "revoked"
	require.Equal(t, http.StatusCreated, e.do(t, ops, http.MethodPost, base+"/"+id+"/consents", body).status)
	body["consentDate"] = today.AddDays(2).String()
	future := e.do(t, ops, http.MethodPost, base+"/"+id+"/consents", body)
	assert.Equal(t, http.StatusUnprocessableEntity, future.status)
	assert.Equal(t, "parties.consent.future_date", future.body["type"])

	list := e.do(t, ops, http.MethodGet, base+"/"+id+"/consents", nil)
	items, ok := list.body["items"].([]any)
	require.True(t, ok)
	require.Len(t, items, 2)
	newest, ok := items[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "revoked", newest["status"], "el más reciente primero")
	assert.Equal(t, 2, auditActions(t, tenant, "parties.consent.registered"))

	ctx := reqctx.WithTenantID(context.Background(), tenant)
	err := database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM parties.consents`)
		return err
	})
	require.ErrorContains(t, err, "permission denied")
}

func TestPartiesRLS(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	a := newTenant()
	require.Equal(t, http.StatusCreated, e.do(t, e.as(a, authz.RoleOperations), http.MethodPost, base, person(uniqueCC())).status)
	other := reqctx.WithTenantID(context.Background(), newTenant())
	require.NoError(t, database.WithTenantTx(other, func(tx pgx.Tx) error {
		for _, table := range []string{"parties", "consents"} {
			var n int
			if err := tx.QueryRow(other, `SELECT count(*) FROM parties.`+table).Scan(&n); err != nil { //nolint:gosec // nombres fijos
				return err
			}
			assert.Zero(t, n, table)
		}
		return nil
	}))
	require.Error(t, database.WithoutTenantTx(context.Background(), func(tx pgx.Tx) error {
		var n int
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM parties.parties`).Scan(&n)
	}), "sin tenant no hay lectura")
}

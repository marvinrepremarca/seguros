//go:build integration

// Package documentsapi prueba documentos contra PostgreSQL real, con almacenamiento y antivirus falsos.
package documentsapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/documents"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
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

type checker struct{ off map[kernel.TenantID]bool }

func (c checker) IsEnabled(ctx context.Context, _ flags.Key) bool {
	t, ok := reqctx.TenantID(ctx)
	return ok && !c.off[t]
}

type storage struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (s *storage) Put(_ context.Context, key string, content []byte, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = content
	return nil
}

func (s *storage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return io.NopCloser(bytes.NewReader(s.objects[key])), nil
}

func (s *storage) Remove(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

func (s *storage) PresignGet(_ context.Context, key, _, _ string, _ time.Duration) (string, error) {
	return "https://s3.test/" + key, nil
}

// scanner marca como virus lo que contenga EICAR; falla si se le pide.
type scanner struct{ down bool }

func (s *scanner) Scan(_ context.Context, r io.Reader) (bool, error) {
	if s.down {
		return false, errors.New("clamd caído")
	}
	b, err := io.ReadAll(r)
	return bytes.Contains(b, []byte("EICAR")), err
}

// fakeCipher invierte los bytes: basta para comprobar que nada se guarda en claro.
type fakeCipher struct{}

func reversed(b []byte) []byte {
	out := slices.Clone(b)
	slices.Reverse(out)
	return out
}

func (fakeCipher) EncryptFor(_ context.Context, _, _ string, p []byte) ([]byte, error) {
	return reversed(p), nil
}

func (fakeCipher) DecryptFor(_ context.Context, _, _ string, c []byte) ([]byte, error) {
	return reversed(c), nil
}

type env struct {
	router *chi.Mux
	policy *authz.Policy
	off    map[kernel.TenantID]bool
	store  *storage
	scan   *scanner
	mod    *documents.Module
	clock  *kernel.FakeClock
}

func newEnv(t *testing.T) *env {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	clock := kernel.NewFakeClock(time.Now().UTC()) //nolint:forbidigo // partición de auditoría del mes actual
	policy, err := authz.DefaultPolicy()
	require.NoError(t, err)
	e := &env{policy: policy, off: map[kernel.TenantID]bool{}, store: &storage{objects: map[string][]byte{}}, scan: &scanner{}, clock: clock}
	e.mod = documents.New(documents.Deps{DB: database, Audit: audit.NewRecorder(clock), Storage: e.store, Scanner: e.scan, Cipher: fakeCipher{}, Checker: checker{off: e.off}, Policy: policy, Clock: clock, Logger: logger, MaxBytes: 1 << 20, RetentionDays: 365, DownloadTTL: time.Minute})
	root := httpx.NewRouter(httpx.RouterOptions{Logger: logger, Clock: clock, MaxBodyBytes: 4 << 20})
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

func newTenant() kernel.TenantID { return kernel.TenantID(uuid.Must(uuid.NewV7())) }

func (e *env) serve(t *testing.T, who authz.Principal, req *http.Request, out any) int {
	t.Helper()
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req.WithContext(context.WithValue(req.Context(), principalKey{}, who)))
	if out != nil && rec.Code < 300 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), out), rec.Body.String())
	}
	return rec.Code
}

func (e *env) do(t *testing.T, who authz.Principal, method, path string, out any) int {
	t.Helper()
	return e.serve(t, who, httptest.NewRequestWithContext(context.Background(), method, path, nil), out)
}

type document struct {
	ID            string `json:"id"`
	CurrentNumber int    `json:"currentNumber"`
}

func form(t *testing.T, fields map[string]string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		require.NoError(t, w.WriteField(k, v))
	}
	part, err := w.CreateFormFile("file", "cliente con nombre.pdf")
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return &buf, w.FormDataContentType()
}

func (e *env) upload(t *testing.T, who authz.Principal, owner uuid.UUID, sensitive string, content []byte, out any) int {
	t.Helper()
	body, ct := form(t, map[string]string{"ownerType": "party", "ownerId": owner.String(), "title": "Denuncia", "sensitive": sensitive}, content)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/documents/documents", body)
	req.Header.Set("Content-Type", ct)
	return e.serve(t, who, req, out)
}

var pdf = []byte("%PDF-1.7 contenido")

func versionOf(t *testing.T, e *env, who authz.Principal, doc string) string {
	t.Helper()
	var d struct {
		Versions []struct {
			ID string `json:"id"`
		} `json:"versions"`
	}
	require.Equal(t, http.StatusOK, e.do(t, who, http.MethodGet, "/api/v1/documents/documents/"+doc, &d))
	require.NotEmpty(t, d.Versions)
	return d.Versions[0].ID
}

func TestUploadScanDownload(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	ops := e.as(tenant, authz.RoleOperations)
	owner := uuid.Must(uuid.NewV7())
	var d document
	require.Equal(t, http.StatusCreated, e.upload(t, ops, owner, "false", pdf, &d))
	version := versionOf(t, e, ops, d.ID)
	download := "/api/v1/documents/documents/" + d.ID + "/versions/" + version + "/download"
	ctx := reqctx.WithTenantID(context.Background(), tenant)

	// Sin revisar no se descarga; con el antivirus caído sigue bloqueado.
	assert.Equal(t, http.StatusConflict, e.do(t, ops, http.MethodPost, download, nil))
	e.scan.down = true
	require.Error(t, e.mod.Service.ScanPending(ctx))
	assert.Equal(t, http.StatusConflict, e.do(t, ops, http.MethodPost, download, nil))
	e.scan.down = false
	require.NoError(t, e.mod.Service.ScanPending(ctx))

	var link struct {
		URL string `json:"url"`
	}
	require.Equal(t, http.StatusOK, e.do(t, ops, http.MethodPost, download, &link))
	assert.Contains(t, link.URL, "tenants/"+uuid.UUID(tenant).String()+"/")
	assert.NotContains(t, link.URL, "cliente", "el nombre se regenera")
	var n int
	require.NoError(t, super.QueryRow(context.Background(), `SELECT count(*) FROM audit.events WHERE tenant_id = $1 AND action = 'documents.version.downloaded'`, uuid.UUID(tenant)).Scan(&n))
	assert.Equal(t, 1, n)

	// Una versión nueva conserva la anterior.
	body, ct := form(t, nil, pdf)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/documents/documents/"+d.ID+"/versions", body)
	req.Header.Set("Content-Type", ct)
	require.Equal(t, http.StatusCreated, e.serve(t, ops, req, nil))
	assert.Len(t, e.store.objects, 2)

	// Otro tenant no lo ve.
	other := e.as(newTenant(), authz.RoleOperations)
	assert.Equal(t, http.StatusNotFound, e.do(t, other, http.MethodGet, "/api/v1/documents/documents/"+d.ID, nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, other, http.MethodPost, download, nil))
	var page struct {
		Items []document `json:"items"`
	}
	path := "/api/v1/documents/documents?ownerType=party&ownerId=" + owner.String()
	require.Equal(t, http.StatusOK, e.do(t, other, http.MethodGet, path, &page))
	assert.Empty(t, page.Items)
	require.Equal(t, http.StatusOK, e.do(t, ops, http.MethodGet, path, &page))
	assert.Len(t, page.Items, 1)
}

func TestInfectedNeverDownloads(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	ops := e.as(tenant, authz.RoleOperations)
	var d document
	require.Equal(t, http.StatusCreated, e.upload(t, ops, uuid.Must(uuid.NewV7()), "false", []byte("%PDF-EICAR"), &d))
	require.NoError(t, e.mod.Service.ScanPending(reqctx.WithTenantID(context.Background(), tenant)))
	version := versionOf(t, e, ops, d.ID)
	assert.Equal(t, http.StatusConflict, e.do(t, ops, http.MethodPost, "/api/v1/documents/documents/"+d.ID+"/versions/"+version+"/download", nil))
}

func TestRejectsBadFiles(t *testing.T) {
	e := newEnv(t)
	ops := e.as(newTenant(), authz.RoleOperations)
	owner := uuid.Must(uuid.NewV7())
	assert.Equal(t, http.StatusUnprocessableEntity, e.upload(t, ops, owner, "false", []byte("<html><script>"), nil))
	assert.Equal(t, http.StatusUnprocessableEntity, e.upload(t, ops, owner, "false", append([]byte("%PDF-"), make([]byte, 1<<20)...), nil))
	assert.Empty(t, e.store.objects)
}

func TestSensitiveAndPermissions(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	analyst := e.as(tenant, authz.RoleClaimsAnalyst)
	ops := e.as(tenant, authz.RoleOperations)
	owner := uuid.Must(uuid.NewV7())
	var loaded document
	require.Equal(t, http.StatusCreated, e.upload(t, ops, owner, "true", pdf, &loaded), "Operaciones carga sensibles")
	assert.Equal(t, http.StatusNotFound, e.do(t, ops, http.MethodGet, "/api/v1/documents/documents/"+loaded.ID, nil), "pero no los lee")
	assert.Equal(t, http.StatusForbidden, e.upload(t, e.as(tenant, authz.RoleSalesAgent), owner, "false", pdf, nil))
	var d document
	require.Equal(t, http.StatusCreated, e.upload(t, analyst, owner, "true", pdf, &d))
	assert.Equal(t, http.StatusNotFound, e.do(t, ops, http.MethodGet, "/api/v1/documents/documents/"+d.ID, nil))
	var page struct {
		Items []document `json:"items"`
	}
	require.Equal(t, http.StatusOK, e.do(t, ops, http.MethodGet, "/api/v1/documents/documents?ownerType=party&ownerId="+owner.String(), &page))
	assert.Empty(t, page.Items)
	assert.Equal(t, http.StatusOK, e.do(t, analyst, http.MethodGet, "/api/v1/documents/documents/"+d.ID, nil))
	assert.Equal(t, http.StatusForbidden, e.do(t, e.as(tenant, authz.RoleSalesAgent), http.MethodGet, "/api/v1/documents/documents/"+d.ID, nil))
}

func TestIdempotentUpload(t *testing.T) {
	e := newEnv(t)
	ops := e.as(newTenant(), authz.RoleOperations)
	owner := uuid.Must(uuid.NewV7())
	send := func() string {
		body, ct := form(t, map[string]string{"ownerType": "party", "ownerId": owner.String(), "title": "Cédula"}, pdf)
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/documents/documents", body)
		req.Header.Set("Content-Type", ct)
		req.Header.Set("Idempotency-Key", "abc")
		var d document
		require.Equal(t, http.StatusCreated, e.serve(t, ops, req, &d))
		return d.ID
	}
	assert.Equal(t, send(), send())
	assert.Len(t, e.store.objects, 1)
}

func TestRetentionOnlyListsAndDeletesNothing(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	officer := e.as(tenant, authz.RoleComplianceOfficer)
	var d document
	require.Equal(t, http.StatusCreated, e.upload(t, officer, uuid.Must(uuid.NewV7()), "false", pdf, &d))
	var page struct {
		Items []document `json:"items"`
	}
	path := "/api/v1/documents/retention-review"
	require.Equal(t, http.StatusOK, e.do(t, officer, http.MethodGet, path, &page))
	assert.Empty(t, page.Items)
	e.clock.Advance(366 * 24 * time.Hour)
	require.Equal(t, http.StatusOK, e.do(t, officer, http.MethodGet, path, &page))
	require.Len(t, page.Items, 1)
	assert.Len(t, e.store.objects, 1, "el plazo no borra el archivo")
	assert.Equal(t, http.StatusForbidden, e.do(t, e.as(tenant, authz.RoleOperations), http.MethodGet, path, nil))
}

func TestFlagOff(t *testing.T) {
	e := newEnv(t)
	on, off := newTenant(), newTenant()
	e.off[off] = true
	path := "/api/v1/documents/documents?ownerType=party&ownerId=" + uuid.NewString()
	assert.Equal(t, http.StatusOK, e.do(t, e.as(on, authz.RoleOperations), http.MethodGet, path, nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, e.as(off, authz.RoleOperations), http.MethodGet, path, nil))
	assert.Equal(t, http.StatusNotFound, e.upload(t, e.as(off, authz.RoleOperations), uuid.Must(uuid.NewV7()), "false", pdf, nil))
}

func TestSensitiveIsEncryptedAndServedByTheAPI(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	analyst := e.as(tenant, authz.RoleClaimsAnalyst)
	var d document
	require.Equal(t, http.StatusCreated, e.upload(t, analyst, uuid.Must(uuid.NewV7()), "true", pdf, &d))
	for _, stored := range e.store.objects {
		assert.NotContains(t, string(stored), "contenido", "en el almacenamiento va cifrado")
	}
	version := versionOf(t, e, analyst, d.ID)
	base := "/api/v1/documents/documents/" + d.ID + "/versions/" + version
	assert.Equal(t, http.StatusConflict, e.do(t, analyst, http.MethodGet, base+"/content", nil), "sin antivirus no se entrega")
	require.NoError(t, e.mod.Service.ScanPending(reqctx.WithTenantID(context.Background(), tenant)))
	var link struct {
		URL string `json:"url"`
	}
	require.Equal(t, http.StatusOK, e.do(t, analyst, http.MethodPost, base+"/download", &link))
	assert.Equal(t, base+"/content", link.URL)

	req := httptest.NewRequestWithContext(context.WithValue(context.Background(), principalKey{}, analyst), http.MethodGet, base+"/content", nil)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, pdf, rec.Body.Bytes())
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "attachment")
	assert.Equal(t, http.StatusNotFound, e.do(t, e.as(tenant, authz.RoleOperations), http.MethodGet, base+"/content", nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, e.as(newTenant(), authz.RoleClaimsAnalyst), http.MethodGet, base+"/content", nil))
}

func TestClaimSupportsAreAlwaysSensitive(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	body, ct := form(t, map[string]string{"ownerType": "claim", "ownerId": uuid.NewString(), "title": "Historia"}, pdf)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/documents/documents", body)
	req.Header.Set("Content-Type", ct)
	var d struct {
		Sensitive bool `json:"sensitive"`
	}
	require.Equal(t, http.StatusCreated, e.serve(t, e.as(tenant, authz.RoleOperations), req, &d))
	assert.True(t, d.Sensitive)
}

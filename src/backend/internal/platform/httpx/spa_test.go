package httpx_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

const spaIndex = `<!doctype html><html><head>` +
	`<meta property="csp-nonce" nonce="__CSP_NONCE__" />` +
	`<script type="module" nonce="__CSP_NONCE__" src="/assets/main-abc.js"></script>` +
	`</head><body><div id="root"></div></body></html>`

func spaFiles() fstest.MapFS {
	return fstest.MapFS{
		"index.html":         {Data: []byte(spaIndex)},
		"assets/main-abc.js": {Data: []byte("console.warn('hola')")},
		"favicon.svg":        {Data: []byte("<svg/>")},
		".env":               {Data: []byte("SECRETO=1")},
		"assets/.hidden":     {Data: []byte("oculto")},
	}
}

func spaRouter(t *testing.T) http.Handler {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	h, err := httpx.NewSPAHandler(spaFiles(), logger)
	require.NoError(t, err)
	r := httpx.NewRouter(httpx.RouterOptions{
		Logger: logger, Clock: kernel.NewFakeClock(time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)), MaxBodyBytes: 1 << 10,
	})
	r.NotFound(h.ServeHTTP)
	return r
}

func get(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), method, target, nil))
	return rec
}

var cspNonce = regexp.MustCompile(`script-src 'nonce-([A-Za-z0-9+/]+)' 'strict-dynamic'`)

func TestSPAIndexHasPerRequestNonce(t *testing.T) {
	t.Parallel()
	h := spaRouter(t)
	first := get(h, http.MethodGet, "/")
	second := get(h, http.MethodGet, "/")
	require.Equal(t, http.StatusOK, first.Code)

	m1 := cspNonce.FindStringSubmatch(first.Header().Get("Content-Security-Policy"))
	m2 := cspNonce.FindStringSubmatch(second.Header().Get("Content-Security-Policy"))
	require.NotNil(t, m1)
	require.NotNil(t, m2)
	assert.NotEqual(t, m1[1], m2[1], "el nonce cambia en cada petición")

	body := first.Body.String()
	assert.NotContains(t, body, "__CSP_NONCE__")
	assert.Equal(t, 2, strings.Count(body, `nonce="`+m1[1]+`"`), "el mismo nonce en todas las etiquetas")
	assert.Equal(t, "no-store", first.Header().Get("Cache-Control"))
	assert.Contains(t, first.Header().Get("Content-Type"), "text/html")
}

func TestSPACSPDirectives(t *testing.T) {
	t.Parallel()
	csp := get(spaRouter(t), http.MethodGet, "/polizas").Header().Get("Content-Security-Policy")
	for _, want := range []string{
		"default-src 'none'", "base-uri 'none'", "frame-ancestors 'none'", "connect-src 'self'",
		"require-trusted-types-for 'script'", "trusted-types 'none'",
	} {
		assert.Contains(t, csp, want)
	}
	assert.NotContains(t, csp, "unsafe-inline")
	assert.NotContains(t, csp, "unsafe-eval")
}

func TestSPAKeepsPlatformSecurityHeaders(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"/", "/assets/main-abc.js"} {
		h := get(spaRouter(t), http.MethodGet, target).Header()
		assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"), target)
		assert.Equal(t, "DENY", h.Get("X-Frame-Options"), target)
		assert.NotEmpty(t, h.Get("Strict-Transport-Security"), target)
		assert.Equal(t, "strict-origin-when-cross-origin", h.Get("Referrer-Policy"), target)
		assert.NotEmpty(t, h.Get("Permissions-Policy"), target)
	}
}

func TestSPARouting(t *testing.T) {
	t.Parallel()
	h := spaRouter(t)
	cases := []struct {
		name, method, target string
		status               int
		html                 bool
	}{
		{"raíz", http.MethodGet, "/", http.StatusOK, true},
		{"ruta del cliente", http.MethodGet, "/polizas/123", http.StatusOK, true},
		{"index explícito con nonce", http.MethodGet, "/index.html", http.StatusOK, true},
		{"HEAD", http.MethodHead, "/login", http.StatusOK, true},
		{"asset", http.MethodGet, "/assets/main-abc.js", http.StatusOK, false},
		{"asset inexistente", http.MethodGet, "/assets/otro.js", http.StatusNotFound, false},
		{"archivo inexistente", http.MethodGet, "/robots.txt", http.StatusNotFound, false},
		{"dotfile", http.MethodGet, "/.env", http.StatusNotFound, false},
		{"dotfile anidado", http.MethodGet, "/assets/.hidden", http.StatusNotFound, false},
		{"traversal", http.MethodGet, "/assets/../../etc/passwd.txt", http.StatusNotFound, false},
		{"API inexistente", http.MethodGet, "/api/v1/nada", http.StatusNotFound, false},
		{"BFF inexistente", http.MethodGet, "/bff/nada", http.StatusNotFound, false},
		{"POST", http.MethodPost, "/polizas", http.StatusNotFound, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			rec := get(h, c.method, c.target)
			assert.Equal(t, c.status, rec.Code)
			isHTML := strings.Contains(rec.Header().Get("Content-Type"), "text/html")
			assert.Equal(t, c.html, isHTML)
			if c.status == http.StatusNotFound {
				assert.Equal(t, httpx.ContentTypeProblem, rec.Header().Get("Content-Type"))
			}
		})
	}
}

func TestSPAAssetCaching(t *testing.T) {
	t.Parallel()
	h := spaRouter(t)
	assert.Contains(t, get(h, http.MethodGet, "/assets/main-abc.js").Header().Get("Cache-Control"), "immutable")
	assert.Equal(t, "no-cache", get(h, http.MethodGet, "/favicon.svg").Header().Get("Cache-Control"))
}

func TestNewSPAHandlerFailsWithBrokenBuild(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.DiscardHandler)
	_, err := httpx.NewSPAHandler(fstest.MapFS{}, logger)
	require.Error(t, err, "sin index.html")
	_, err = httpx.NewSPAHandler(fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}, logger)
	require.ErrorContains(t, err, "marcador de nonce")
}

func TestRateLimitByUser(t *testing.T) {
	t.Parallel()
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := httpx.RateLimitByUser(func(r *http.Request) bool { return strings.HasSuffix(r.URL.Path, "/reveal") }, 2, time.Hour)(ok)
	status := func(ctx context.Context, path string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodPost, path, nil))
		return rec.Code
	}
	ana := reqctx.WithUserID(context.Background(), kernel.UserID(uuid.Must(uuid.NewV7())))
	luis := reqctx.WithUserID(context.Background(), kernel.UserID(uuid.Must(uuid.NewV7())))
	assert.Equal(t, http.StatusOK, status(ana, "/x/reveal"))
	assert.Equal(t, http.StatusOK, status(ana, "/x/reveal"))
	assert.Equal(t, http.StatusTooManyRequests, status(ana, "/x/reveal"))
	assert.Equal(t, http.StatusOK, status(luis, "/x/reveal"), "el contador es por usuario")
	assert.Equal(t, http.StatusOK, status(ana, "/otra"), "las demás rutas no cuentan")
}

func TestRateLimitByTenant(t *testing.T) {
	t.Parallel()
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := httpx.RateLimitByTenant(func(r *http.Request) bool { return strings.HasSuffix(r.URL.Path, "/reveal") }, 2, time.Hour)(ok)
	status := func(ctx context.Context) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodPost, "/x/reveal", nil))
		return rec.Code
	}
	a := reqctx.WithTenantID(context.Background(), kernel.TenantID(uuid.Must(uuid.NewV7())))
	b := reqctx.WithTenantID(context.Background(), kernel.TenantID(uuid.Must(uuid.NewV7())))
	assert.Equal(t, http.StatusOK, status(a))
	assert.Equal(t, http.StatusOK, status(a))
	assert.Equal(t, http.StatusTooManyRequests, status(a))
	assert.Equal(t, http.StatusOK, status(b), "el contador es por tenant")
}

func TestCanceledClientIsNotAServerError(t *testing.T) {
	t.Parallel()
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/x", nil)
	httpx.WriteError(rec, req, logger, fmt.Errorf("consultando: %w", context.Canceled))
	assert.Equal(t, 499, rec.Code)
	assert.NotContains(t, logs.String(), "ERROR")

	// Un error de cancelación sin que el cliente se haya ido sí es una falla del servidor.
	rec = httptest.NewRecorder()
	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil)
	httpx.WriteError(rec, req, logger, fmt.Errorf("consultando: %w", context.Canceled))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, logs.String(), "ERROR")
}

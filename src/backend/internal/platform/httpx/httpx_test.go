package httpx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/marvinrepremarca/seguros/internal/platform/config"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

type fixture struct {
	router http.Handler
	logs   *bytes.Buffer
}

func newFixture(t *testing.T, checks ...httpx.ReadinessCheck) fixture {
	t.Helper()
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	r := httpx.NewRouter(httpx.RouterOptions{
		Logger:       logger,
		Clock:        kernel.NewFakeClock(time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)),
		MaxBodyBytes: 16,
		Readiness:    checks,
	})
	r.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("detalle interno secreto") })
	r.Get("/items/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r.Post("/echo", func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			httpx.WriteError(w, r, logger, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return fixture{router: r, logs: logs}
}

func (f fixture) do(t *testing.T, method, target string, body io.Reader, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, target, body)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func problem(t *testing.T, rec *httptest.ResponseRecorder) httpx.Problem {
	t.Helper()
	assert.Equal(t, httpx.ContentTypeProblem, rec.Header().Get("Content-Type"))
	var p httpx.Problem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p), rec.Body.String())
	return p
}

func TestHealthLive(t *testing.T) {
	t.Parallel()
	rec := newFixture(t).do(t, http.MethodGet, "/health/live", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()
	h := newFixture(t).do(t, http.MethodGet, "/health/live", nil).Header()
	assert.Contains(t, h.Get("Strict-Transport-Security"), "max-age=")
	assert.Contains(t, h.Get("Content-Security-Policy"), "frame-ancestors 'none'")
	assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", h.Get("X-Frame-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", h.Get("Referrer-Policy"))
	assert.NotEmpty(t, h.Get("Permissions-Policy"))
	assert.Equal(t, "no-store", h.Get("Cache-Control"))
}

func TestHealthReady(t *testing.T) {
	t.Parallel()
	ok := httpx.ReadinessCheck{Name: "db", Check: func(context.Context) error { return nil }}
	rec := newFixture(t, ok).do(t, http.MethodGet, "/health/ready", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	failing := httpx.ReadinessCheck{Name: "valkey", Check: func(context.Context) error {
		return errors.New("dial tcp 10.0.0.5:6379: connection refused")
	}}
	f := newFixture(t, ok, failing)
	rec = f.do(t, http.MethodGet, "/health/ready", nil)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	p := problem(t, rec)
	assert.Equal(t, httpx.CodeUnavailable, p.Type)
	assert.NotContains(t, rec.Body.String(), "10.0.0.5", "sin detalles internos en la respuesta")
	assert.Contains(t, f.logs.String(), "valkey: dial tcp", "el detalle va al log")
}

func TestHealthReadyTimesOut(t *testing.T) {
	t.Parallel()
	slow := httpx.ReadinessCheck{Name: "slow", Check: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	rec := newFixture(t, slow).do(t, http.MethodGet, "/health/ready", nil)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestNotFoundAndMethodNotAllowed(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	rec := f.do(t, http.MethodGet, "/nope", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	p := problem(t, rec)
	assert.Equal(t, httpx.CodeNotFound, p.Type)
	assert.Equal(t, http.StatusNotFound, p.Status)
	assert.Equal(t, "/nope", p.Instance)
	assert.NotEmpty(t, p.RequestID)

	rec = f.do(t, http.MethodDelete, "/health/live", nil)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, httpx.CodeMethodNotAllowed, problem(t, rec).Type)
}

func TestRecovererReturnsGeneric500(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	rec := f.do(t, http.MethodGet, "/boom", nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	p := problem(t, rec)
	assert.Equal(t, httpx.CodeInternal, p.Type)
	assert.NotContains(t, rec.Body.String(), "secreto")
	assert.Contains(t, f.logs.String(), "detalle interno secreto")
	assert.Contains(t, f.logs.String(), "stack")
}

func TestRecovererRepanicsAbortHandler(t *testing.T) {
	t.Parallel()
	h := httpx.Recoverer(slog.New(slog.DiscardHandler))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	assert.PanicsWithValue(t, http.ErrAbortHandler, func() {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	})
}

func TestRequestID(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	rec := f.do(t, http.MethodGet, "/health/live", nil)
	generated := rec.Header().Get(httpx.HeaderRequestID)
	assert.Len(t, generated, 36)

	rec = f.do(t, http.MethodGet, "/health/live", nil, httpx.HeaderRequestID, "traefik-abc_123.x")
	assert.Equal(t, "traefik-abc_123.x", rec.Header().Get(httpx.HeaderRequestID))

	for _, bad := range []string{"<script>", strings.Repeat("a", 65), "a b", "x\ny"} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health/live", nil)
		req.Header[httpx.HeaderRequestID] = []string{bad}
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)
		assert.NotEqual(t, bad, rec.Header().Get(httpx.HeaderRequestID))
		assert.Len(t, rec.Header().Get(httpx.HeaderRequestID), 36)
	}
}

func TestMaxBodyBytes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	assert.Equal(t, http.StatusNoContent, f.do(t, http.MethodPost, "/echo", strings.NewReader("pequeño")).Code)

	rec := f.do(t, http.MethodPost, "/echo", strings.NewReader(strings.Repeat("x", 17)))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Equal(t, httpx.CodeBodyTooLarge, problem(t, rec).Type)

	// Sin Content-Length (chunked): el límite se aplica al leer.
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/echo", io.MultiReader(strings.NewReader(strings.Repeat("x", 40))))
	req.ContentLength = -1
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestAccessLogUsesRoutePattern(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.do(t, http.MethodGet, "/items/0192d6a0-0000-7000-8000-000000000001?email=x@y.co", nil)
	out := f.logs.String()
	assert.Contains(t, out, `"route":"/items/{id}"`)
	assert.Contains(t, out, `"status":204`)
	assert.NotContains(t, out, "0192d6a0", "sin IDs concretos")
	assert.NotContains(t, out, "x@y.co", "sin query string")
}

func TestWriteErrorMapsDomainErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind   kernel.ErrorKind
		status int
	}{
		{kernel.KindValidation, http.StatusUnprocessableEntity},
		{kernel.KindNotFound, http.StatusNotFound},
		{kernel.KindConflict, http.StatusConflict},
		{kernel.KindForbidden, http.StatusForbidden},
		{kernel.KindUnauthorized, http.StatusUnauthorized},
		{kernel.ErrorKind(99), http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			de := kernel.NewError(tc.kind, "policies.policy.some_code", "mensaje interno")
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil)
			rec := httptest.NewRecorder()
			httpx.WriteError(rec, req, slog.New(slog.NewJSONHandler(&logs, nil)), fmt.Errorf("envuelto: %w", de))
			assert.Equal(t, tc.status, rec.Code)
			p := problem(t, rec)
			assert.Equal(t, "policies.policy.some_code", p.Type)
			assert.NotEmpty(t, p.Title)
			assert.NotContains(t, rec.Body.String(), "mensaje interno")
		})
	}
}

func TestWriteErrorGenericIs500(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	httpx.WriteError(rec, req, slog.New(slog.NewJSONHandler(&logs, nil)), errors.New(`pq: relation "x" does not exist`))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, httpx.CodeInternal, problem(t, rec).Type)
	assert.NotContains(t, rec.Body.String(), "relation")
	assert.Contains(t, logs.String(), "relation")
}

func TestRouteTelemetryNamesSpanWithPattern(t *testing.T) {
	t.Parallel()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	f := newFixture(t)
	h := otelhttp.NewHandler(f.router, "http.server", otelhttp.WithTracerProvider(tp))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/items/abc", nil))
	require.NoError(t, tp.ForceFlush(context.Background()))
	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "GET /items/{id}", spans[0].Name())
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestServeGracefulShutdown(t *testing.T) {
	t.Parallel()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	cfg := config.HTTP{ReadHeaderTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second}
	srv := httpx.NewServer(cfg, newFixture(t).router, slog.New(slog.DiscardHandler))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- httpx.Serve(ctx, srv, ln, 5*time.Second) }()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+ln.Addr().String()+"/health/live", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, time.Second, srv.ReadHeaderTimeout)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Serve no terminó tras cancelar el contexto")
	}
}

func TestServeReturnsListenerErrors(t *testing.T) {
	t.Parallel()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, ln.Close())
	srv := httpx.NewServer(config.HTTP{ReadHeaderTimeout: time.Second}, http.NotFoundHandler(), slog.New(slog.DiscardHandler))
	require.Error(t, httpx.Serve(context.Background(), srv, ln, time.Second))
}

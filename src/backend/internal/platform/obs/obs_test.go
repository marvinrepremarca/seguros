package obs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/obs"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

func logLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &m), buf.String())
	return m
}

func newLogger(buf *bytes.Buffer, level slog.Level) *slog.Logger {
	return obs.NewLogger(buf, obs.LoggerOptions{Level: level, Service: "svc", Version: "1.0", Env: "test"})
}

func TestLoggerAddsServiceFields(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	newLogger(&buf, slog.LevelInfo).Info("hola", "count", 3)
	m := logLine(t, &buf)
	assert.Equal(t, "hola", m["msg"])
	assert.Equal(t, "svc", m["service"])
	assert.Equal(t, "1.0", m["version"])
	assert.Equal(t, "test", m["env"])
	assert.InDelta(t, 3, m["count"], 0)
}

func TestLoggerRespectsLevel(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := newLogger(&buf, slog.LevelWarn)
	l.Info("oculto")
	assert.Zero(t, buf.Len())
	l.Warn("visible")
	assert.Contains(t, buf.String(), "visible")
}

func TestLoggerRedactsSensitiveKeys(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := newLogger(&buf, slog.LevelInfo)
	l.Info("x",
		"password", "p1", "db_dsn", "p2", "Authorization", "p3", "access_token", "p4",
		"email", "p5", "document_number", "p6", "nit", "p7", "telefono", "p8",
		slog.Group("party", "cedula", "p9", "name_len", 5),
		"limit", 50, "unit", "u",
	)
	out := buf.String()
	for _, secret := range []string{"p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9"} {
		assert.NotContains(t, out, `"`+secret+`"`)
	}
	m := logLine(t, &buf)
	assert.Equal(t, "[REDACTED]", m["password"])
	party, ok := m["party"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "[REDACTED]", party["cedula"])
	assert.InDelta(t, 5, party["name_len"], 0)
	assert.InDelta(t, 50, m["limit"], 0, "no redacta claves que solo contienen 'nit'")
	assert.Equal(t, "u", m["unit"])
}

func TestLoggerRedactsLogValuers(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	premium, err := kernel.ParseMoney("1500000", kernel.COP)
	require.NoError(t, err)
	newLogger(&buf, slog.LevelInfo).Info("x", "premium", premium)
	assert.NotContains(t, buf.String(), "1500000")
	assert.Equal(t, "COP [REDACTED]", logLine(t, &buf)["premium"])
}

func TestLoggerAddsCorrelationFromContext(t *testing.T) {
	t.Parallel()
	tenant := kernel.TenantID(uuid.Must(uuid.NewV7()))
	user := kernel.UserID(uuid.Must(uuid.NewV7()))
	traceID := trace.TraceID{1, 2, 3}
	spanID := trace.SpanID{4, 5}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	}))
	ctx = reqctx.WithTenantID(ctx, tenant)
	ctx = reqctx.WithUserID(ctx, user)
	ctx = reqctx.WithRequestID(ctx, "req-1")

	var buf bytes.Buffer
	newLogger(&buf, slog.LevelInfo).With("k", "v").WithGroup("g").With("b", 2).WithGroup("h").
		InfoContext(ctx, "x", "a", 1, "email", "x@y.co")
	m := logLine(t, &buf)
	assert.Equal(t, traceID.String(), m["trace_id"], "correlación en la raíz aunque haya grupos")
	assert.Equal(t, spanID.String(), m["span_id"])
	assert.Equal(t, tenant.String(), m["tenant_id"])
	assert.Equal(t, user.String(), m["user_id"])
	assert.Equal(t, "req-1", m["request_id"])
	assert.Equal(t, "v", m["k"])
	g, ok := m["g"].(map[string]any)
	require.True(t, ok, buf.String())
	assert.InDelta(t, 2, g["b"], 0)
	h, ok := g["h"].(map[string]any)
	require.True(t, ok, buf.String())
	assert.InDelta(t, 1, h["a"], 0)
	assert.Equal(t, "[REDACTED]", h["email"], "redacta también dentro de grupos")
}

func TestLoggerWithoutContextFields(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	newLogger(&buf, slog.LevelInfo).WithGroup("").WithGroup("empty").InfoContext(context.Background(), "x")
	m := logLine(t, &buf)
	assert.NotContains(t, m, "empty", "los grupos vacíos se omiten")
	for _, k := range []string{"trace_id", "tenant_id", "user_id", "request_id"} {
		assert.NotContains(t, m, k)
	}
}

// Las pruebas de telemetría modifican los proveedores globales de OTel: no son paralelas.

func TestSetupTelemetryWithoutEndpoint(t *testing.T) {
	ctx := context.Background()
	shutdown, err := obs.SetupTelemetry(ctx, obs.TelemetryOptions{Service: "svc", SampleRatio: 1})
	require.NoError(t, err)

	_, span := otel.Tracer("test").Start(ctx, "op")
	assert.True(t, span.SpanContext().IsValid(), "genera trazas aunque no exporte")
	span.End()
	require.NoError(t, shutdown(ctx))
}

func TestSetupTelemetryExportsToOTLP(t *testing.T) {
	var mu sync.Mutex
	paths := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths[r.URL.Path]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	shutdown, err := obs.SetupTelemetry(ctx, obs.TelemetryOptions{Service: "svc", Endpoint: srv.URL + "/", SampleRatio: 1})
	require.NoError(t, err)

	_, span := otel.Tracer("test").Start(ctx, "op")
	span.End()
	counter, err := otel.Meter("test").Int64Counter("requests")
	require.NoError(t, err)
	counter.Add(ctx, 1)

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	require.NoError(t, shutdown(shutdownCtx))

	mu.Lock()
	defer mu.Unlock()
	assert.Positive(t, paths["/v1/traces"], "trazas exportadas: %v", paths)
	assert.Positive(t, paths["/v1/metrics"], "métricas exportadas: %v", paths)
}

func TestSetupTelemetryRespectsSampling(t *testing.T) {
	ctx := context.Background()
	shutdown, err := obs.SetupTelemetry(ctx, obs.TelemetryOptions{Service: "svc", SampleRatio: 0})
	require.NoError(t, err)
	_, span := otel.Tracer("test").Start(ctx, "op")
	assert.False(t, span.SpanContext().IsSampled())
	span.End()
	require.NoError(t, shutdown(ctx))
}

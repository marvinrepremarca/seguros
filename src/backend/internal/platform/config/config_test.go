package config_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/config"
)

// sentinel es un valor ficticio (no es una credencial) para comprobar que nunca aparece en
// errores ni logs.
const sentinel = "zz-sentinel-value"

func dsn(query string) string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword("seguros_app", sentinel),
		Host:     "localhost:5432",
		Path:     "/seguros",
		RawQuery: query,
	}
	return u.String()
}

func env(kv map[string]string) config.LookupFunc {
	return func(k string) (string, bool) {
		v, ok := kv[k]
		return v, ok
	}
}

func TestLoadAPIDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := config.LoadAPI(env(map[string]string{"SEGUROS_DB_URL": dsn("")}))
	require.NoError(t, err)
	assert.Equal(t, config.EnvDevelopment, cfg.Env)
	assert.Equal(t, "seguros-api", cfg.ServiceName)
	assert.Equal(t, slog.LevelInfo, cfg.LogLevel)
	assert.Equal(t, ":8080", cfg.HTTP.Addr)
	assert.Equal(t, 5*time.Second, cfg.HTTP.ReadHeaderTimeout)
	assert.Equal(t, int64(1<<20), cfg.HTTP.MaxBodyBytes)
	assert.Equal(t, int32(20), cfg.DB.MaxConns)
	assert.Equal(t, dsn(""), cfg.DB.URL.Reveal())
	assert.Empty(t, cfg.OTLPEndpoint)
	assert.InDelta(t, 1.0, cfg.TraceSampleRatio, 0)
}

func TestLoadAPIOverrides(t *testing.T) {
	t.Parallel()
	cfg, err := config.LoadAPI(env(map[string]string{
		"SEGUROS_ENV":                 "production",
		"SEGUROS_DB_URL":              dsn("sslmode=verify-full"),
		"SEGUROS_HTTP_ADDR":           ":9090",
		"SEGUROS_HTTP_READ_TIMEOUT":   "3s",
		"SEGUROS_LOG_LEVEL":           "debug",
		"SEGUROS_DB_MAX_CONNS":        "50",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "https://otel.example.com:4318",
		"SEGUROS_TRACE_SAMPLE_RATIO":  "0.25",
	}))
	require.NoError(t, err)
	assert.Equal(t, config.EnvProduction, cfg.Env)
	assert.True(t, cfg.Env.IsProductionLike())
	assert.Equal(t, ":9090", cfg.HTTP.Addr)
	assert.Equal(t, 3*time.Second, cfg.HTTP.ReadTimeout)
	assert.Equal(t, slog.LevelDebug, cfg.LogLevel)
	assert.Equal(t, int32(50), cfg.DB.MaxConns)
	assert.InDelta(t, 0.25, cfg.TraceSampleRatio, 0)
}

func TestLoadAPIInvalid(t *testing.T) {
	t.Parallel()
	db := dsn("")
	tests := []struct {
		name    string
		env     map[string]string
		wantKey string
	}{
		{"sin DB", map[string]string{}, "SEGUROS_DB_URL: es obligatoria"},
		{"DB en blanco", map[string]string{"SEGUROS_DB_URL": "   "}, "SEGUROS_DB_URL: es obligatoria"},
		{"DB no postgres", map[string]string{"SEGUROS_DB_URL": "mysql://h/db"}, "SEGUROS_DB_URL: debe ser una URL"},
		{"prod sin TLS a BD", map[string]string{"SEGUROS_ENV": "production", "SEGUROS_DB_URL": db}, "sslmode"},
		{"prod con sslmode=disable", map[string]string{"SEGUROS_ENV": "staging", "SEGUROS_DB_URL": dsn("sslmode=disable")}, "sslmode"},
		{"entorno desconocido", map[string]string{"SEGUROS_ENV": "prod", "SEGUROS_DB_URL": db}, "SEGUROS_ENV"},
		{"nivel de log", map[string]string{"SEGUROS_LOG_LEVEL": "verbose", "SEGUROS_DB_URL": db}, "SEGUROS_LOG_LEVEL"},
		{"duración inválida", map[string]string{"SEGUROS_HTTP_READ_TIMEOUT": "15", "SEGUROS_DB_URL": db}, "SEGUROS_HTTP_READ_TIMEOUT: duración inválida"},
		{"duración cero", map[string]string{"SEGUROS_HTTP_IDLE_TIMEOUT": "0s", "SEGUROS_DB_URL": db}, "SEGUROS_HTTP_IDLE_TIMEOUT: debe ser > 0"},
		{"body cero", map[string]string{"SEGUROS_HTTP_MAX_BODY_BYTES": "0", "SEGUROS_DB_URL": db}, "SEGUROS_HTTP_MAX_BODY_BYTES"},
		{"body no numérico", map[string]string{"SEGUROS_HTTP_MAX_BODY_BYTES": "1MB", "SEGUROS_DB_URL": db}, "SEGUROS_HTTP_MAX_BODY_BYTES: entero"},
		{"pool incoherente", map[string]string{"SEGUROS_DB_MIN_CONNS": "30", "SEGUROS_DB_URL": db}, "MAX_CONNS"},
		{"pool no numérico", map[string]string{"SEGUROS_DB_MAX_CONNS": "x", "SEGUROS_DB_URL": db}, "SEGUROS_DB_MAX_CONNS: entero"},
		{"ratio fuera de rango", map[string]string{"SEGUROS_TRACE_SAMPLE_RATIO": "2", "SEGUROS_DB_URL": db}, "SEGUROS_TRACE_SAMPLE_RATIO"},
		{"otlp sin esquema", map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "collector:4318", "SEGUROS_DB_URL": db}, "OTEL_EXPORTER_OTLP_ENDPOINT"},
		{"otlp http en prod", map[string]string{"SEGUROS_ENV": "production", "OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "SEGUROS_DB_URL": dsn("sslmode=require")}, "https"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := config.LoadAPI(env(tc.env))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantKey)
			assert.NotContains(t, err.Error(), sentinel, "el error no debe revelar la contraseña")
		})
	}
}

func TestLoadAPIReportsAllErrors(t *testing.T) {
	t.Parallel()
	_, err := config.LoadAPI(env(map[string]string{"SEGUROS_ENV": "x", "SEGUROS_LOG_LEVEL": "y"}))
	require.Error(t, err)
	for _, k := range []string{"SEGUROS_ENV", "SEGUROS_LOG_LEVEL", "SEGUROS_DB_URL"} {
		assert.Contains(t, err.Error(), k)
	}
}

func TestLoadMigrator(t *testing.T) {
	t.Parallel()
	cfg, err := config.LoadMigrator(env(map[string]string{"SEGUROS_MIGRATOR_DB_URL": dsn("")}))
	require.NoError(t, err)
	assert.Equal(t, "seguros-migrate", cfg.ServiceName)
	assert.Equal(t, dsn(""), cfg.DatabaseURL.Reveal())

	_, err = config.LoadMigrator(env(map[string]string{}))
	require.ErrorContains(t, err, "SEGUROS_MIGRATOR_DB_URL")
}

func TestSecretIsRedacted(t *testing.T) {
	t.Parallel()
	s := config.NewSecret(sentinel)
	assert.Equal(t, sentinel, s.Reveal())
	assert.False(t, s.IsEmpty())
	assert.True(t, config.NewSecret("").IsEmpty())

	for _, out := range []string{
		fmt.Sprint(s), fmt.Sprintf("%v %+v %#v %s", s, s, s, s),
		fmt.Sprintf("%+v", config.Database{URL: s}),
	} {
		assert.NotContains(t, out, sentinel)
	}

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "pw", s)
	assert.NotContains(t, buf.String(), sentinel)
	assert.Contains(t, buf.String(), "[REDACTED]")
}

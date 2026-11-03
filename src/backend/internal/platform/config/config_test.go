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

// apiEnv completa kv con las variables obligatorias de auth y Valkey de cmd/api (desarrollo),
// sin sobrescribir las que la prueba fije.
func apiEnv(kv map[string]string) config.LookupFunc {
	full := map[string]string{
		"SEGUROS_OIDC_ISSUER":        "http://localhost:8081/realms/seguros-dev",
		"SEGUROS_OIDC_CLIENT_SECRET": sentinel,
		"SEGUROS_PUBLIC_URL":         "http://localhost:5173",
		"SEGUROS_VALKEY_URL":         "redis://:" + sentinel + "@localhost:6379/0",
	}
	for k, v := range kv {
		full[k] = v
	}
	return env(full)
}

// prodAuth son las variables de auth/Valkey válidas en staging/producción.
func prodAuth(kv map[string]string) map[string]string {
	kv["SEGUROS_OIDC_ISSUER"] = "https://id.seguros.example/realms/seguros"
	kv["SEGUROS_PUBLIC_URL"] = "https://app.seguros.example"
	kv["SEGUROS_VALKEY_URL"] = "rediss://:" + sentinel + "@valkey:6379/0"
	return kv
}

func TestLoadAPIDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := config.LoadAPI(apiEnv(map[string]string{"SEGUROS_DB_URL": dsn("")}))
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
	cfg, err := config.LoadAPI(apiEnv(prodAuth(map[string]string{
		"SEGUROS_ENV":                 "production",
		"SEGUROS_DB_URL":              dsn("sslmode=verify-full"),
		"SEGUROS_HTTP_ADDR":           ":9090",
		"SEGUROS_HTTP_READ_TIMEOUT":   "3s",
		"SEGUROS_LOG_LEVEL":           "debug",
		"SEGUROS_DB_MAX_CONNS":        "50",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "https://otel.example.com:4318",
		"SEGUROS_TRACE_SAMPLE_RATIO":  "0.25",
	})))
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
			_, err := config.LoadAPI(apiEnv(tc.env))
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

func TestLoadPlatformCtl(t *testing.T) {
	t.Parallel()
	cfg, err := config.LoadPlatformCtl(env(map[string]string{"SEGUROS_DB_URL": dsn("")}))
	require.NoError(t, err)
	assert.Equal(t, "seguros-platformctl", cfg.ServiceName)
	assert.Equal(t, dsn(""), cfg.DB.URL.Reveal())

	_, err = config.LoadPlatformCtl(env(map[string]string{}))
	require.ErrorContains(t, err, "SEGUROS_DB_URL")
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

func TestLoadAPIAuthDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := config.LoadAPI(apiEnv(map[string]string{"SEGUROS_DB_URL": dsn("")}))
	require.NoError(t, err)
	assert.Equal(t, "seguros-bff", cfg.Auth.ClientID)
	assert.True(t, cfg.Auth.MFARequired, "MFA activa por defecto")
	assert.Equal(t, 30*time.Minute, cfg.Auth.SessionIdle)
	assert.Equal(t, 8*time.Hour, cfg.Auth.SessionLifetime)
	assert.Equal(t, sentinel, cfg.Auth.ClientSecret.Reveal())
	assert.NotContains(t, fmt.Sprintf("%v %+v", cfg, cfg), sentinel, "los secretos no se imprimen")
}

func TestLoadAPIAuthInvalid(t *testing.T) {
	t.Parallel()
	db := dsn("")
	prodDB := dsn("sslmode=verify-full")
	tests := []struct {
		name    string
		env     map[string]string
		wantKey string
	}{
		{"sin issuer", map[string]string{"SEGUROS_DB_URL": db, "SEGUROS_OIDC_ISSUER": ""}, "SEGUROS_OIDC_ISSUER: es obligatoria"},
		{"sin secreto", map[string]string{"SEGUROS_DB_URL": db, "SEGUROS_OIDC_CLIENT_SECRET": ""}, "SEGUROS_OIDC_CLIENT_SECRET: es obligatoria"},
		{"public url con ruta", map[string]string{"SEGUROS_DB_URL": db, "SEGUROS_PUBLIC_URL": "http://localhost:5173/app"}, "SEGUROS_PUBLIC_URL: debe ser una origin"},
		{"issuer no http", map[string]string{"SEGUROS_DB_URL": db, "SEGUROS_OIDC_ISSUER": "ftp://kc/realms/x"}, "SEGUROS_OIDC_ISSUER: URL"},
		{"mfa no booleano", map[string]string{"SEGUROS_DB_URL": db, "SEGUROS_AUTH_MFA_REQUIRED": "si"}, "SEGUROS_AUTH_MFA_REQUIRED"},
		{"idle mayor que lifetime", map[string]string{"SEGUROS_DB_URL": db, "SEGUROS_SESSION_IDLE_TIMEOUT": "9h"}, "SEGUROS_SESSION_IDLE_TIMEOUT"},
		{"valkey no redis", map[string]string{"SEGUROS_DB_URL": db, "SEGUROS_VALKEY_URL": "http://v:6379"}, "SEGUROS_VALKEY_URL"},
		{"prod sin mfa", prodAuth(map[string]string{"SEGUROS_ENV": "production", "SEGUROS_DB_URL": prodDB, "SEGUROS_AUTH_MFA_REQUIRED": "false"}), "SEGUROS_AUTH_MFA_REQUIRED"},
		{"prod con discovery", prodAuth(map[string]string{"SEGUROS_ENV": "production", "SEGUROS_DB_URL": prodDB, "SEGUROS_OIDC_DISCOVERY_URL": "https://kc:8443/realms/x"}), "SEGUROS_OIDC_DISCOVERY_URL"},
		{"prod issuer http", map[string]string{"SEGUROS_ENV": "staging", "SEGUROS_DB_URL": prodDB}, "SEGUROS_OIDC_ISSUER: debe usar https"},
		{"prod valkey sin tls", map[string]string{"SEGUROS_ENV": "staging", "SEGUROS_DB_URL": prodDB}, "rediss"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := config.LoadAPI(apiEnv(tc.env))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantKey)
			assert.NotContains(t, err.Error(), sentinel)
		})
	}
}

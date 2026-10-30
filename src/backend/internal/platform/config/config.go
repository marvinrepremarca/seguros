// Package config carga y valida la configuración desde variables de entorno al arrancar. Una
// configuración inválida impide arrancar (fail-fast); los secretos se envuelven en Secret para que
// no lleguen a logs.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// LookupFunc obtiene una variable de entorno (os.LookupEnv en producción, un mapa en pruebas).
type LookupFunc func(key string) (string, bool)

// Env es el entorno de ejecución.
type Env string

// Entornos soportados.
const (
	EnvDevelopment Env = "development"
	EnvTest        Env = "test"
	EnvStaging     Env = "staging"
	EnvProduction  Env = "production"
)

// IsProductionLike indica staging o producción, donde se exigen controles estrictos.
func (e Env) IsProductionLike() bool { return e == EnvStaging || e == EnvProduction }

// Secret es un valor sensible (contraseñas, DSN). Se redacta en String, fmt y slog.
type Secret struct{ value string }

// NewSecret envuelve un valor sensible.
func NewSecret(v string) Secret { return Secret{value: v} }

// Reveal devuelve el valor real. Úsalo solo al entregarlo a la librería que lo necesita.
func (s Secret) Reveal() string { return s.value }

// IsEmpty indica si no hay valor.
func (s Secret) IsEmpty() bool { return s.value == "" }

// String implementa fmt.Stringer sin revelar el valor.
func (Secret) String() string { return "[REDACTED]" }

// GoString implementa fmt.GoStringer (%#v) sin revelar el valor.
func (Secret) GoString() string { return "config.Secret{[REDACTED]}" }

// LogValue implementa slog.LogValuer sin revelar el valor.
func (Secret) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

// Common es la configuración compartida por todos los binarios.
type Common struct {
	Env            Env
	ServiceName    string
	ServiceVersion string
	LogLevel       slog.Level
	// OTLPEndpoint vacío = sin exportación de telemetría (las trazas se generan igual para los logs).
	OTLPEndpoint     string
	TraceSampleRatio float64
}

// HTTP configura el servidor HTTP.
type HTTP struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxBodyBytes      int64
}

// Database configura el pool de la aplicación (rol seguros_app, sujeto a RLS).
type Database struct {
	URL              Secret
	MaxConns         int32
	MinConns         int32
	MaxConnLifetime  time.Duration
	StatementTimeout time.Duration
}

// API es la configuración de cmd/api.
type API struct {
	Common
	HTTP HTTP
	DB   Database
}

// Migrator es la configuración de cmd/migrate (rol seguros_migrator, dueño de los esquemas).
type Migrator struct {
	Common
	DatabaseURL Secret
}

// LoadAPI carga y valida la configuración de cmd/api.
func LoadAPI(lookup LookupFunc) (API, error) {
	r := reader{lookup: lookup}
	cfg := API{
		Common: r.common("seguros-api"),
		HTTP: HTTP{
			Addr:              r.str("SEGUROS_HTTP_ADDR", ":8080"),
			ReadHeaderTimeout: r.duration("SEGUROS_HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
			ReadTimeout:       r.duration("SEGUROS_HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:      r.duration("SEGUROS_HTTP_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:       r.duration("SEGUROS_HTTP_IDLE_TIMEOUT", 120*time.Second),
			ShutdownTimeout:   r.duration("SEGUROS_HTTP_SHUTDOWN_TIMEOUT", 20*time.Second),
			MaxBodyBytes:      r.int64("SEGUROS_HTTP_MAX_BODY_BYTES", 1<<20),
		},
		DB: r.database("SEGUROS_DB_URL"),
	}
	r.positiveDurations(map[string]time.Duration{
		"SEGUROS_HTTP_READ_HEADER_TIMEOUT": cfg.HTTP.ReadHeaderTimeout,
		"SEGUROS_HTTP_READ_TIMEOUT":        cfg.HTTP.ReadTimeout,
		"SEGUROS_HTTP_WRITE_TIMEOUT":       cfg.HTTP.WriteTimeout,
		"SEGUROS_HTTP_IDLE_TIMEOUT":        cfg.HTTP.IdleTimeout,
		"SEGUROS_HTTP_SHUTDOWN_TIMEOUT":    cfg.HTTP.ShutdownTimeout,
	})
	if cfg.HTTP.MaxBodyBytes <= 0 {
		r.fail("SEGUROS_HTTP_MAX_BODY_BYTES", "debe ser > 0")
	}
	r.postgresURL("SEGUROS_DB_URL", cfg.DB.URL, cfg.Env)
	return cfg, r.err()
}

// LoadMigrator carga y valida la configuración de cmd/migrate.
func LoadMigrator(lookup LookupFunc) (Migrator, error) {
	r := reader{lookup: lookup}
	cfg := Migrator{
		Common:      r.common("seguros-migrate"),
		DatabaseURL: NewSecret(r.required("SEGUROS_MIGRATOR_DB_URL")),
	}
	r.postgresURL("SEGUROS_MIGRATOR_DB_URL", cfg.DatabaseURL, cfg.Env)
	return cfg, r.err()
}

// reader acumula errores para reportarlos todos juntos al arrancar.
type reader struct {
	lookup LookupFunc
	errs   []error
}

func (r *reader) fail(key, msg string) {
	r.errs = append(r.errs, fmt.Errorf("%s: %s", key, msg))
}

func (r *reader) err() error {
	if len(r.errs) == 0 {
		return nil
	}
	return fmt.Errorf("configuración inválida: %w", errors.Join(r.errs...))
}

func (r *reader) str(key, def string) string {
	if v, ok := r.lookup(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func (r *reader) required(key string) string {
	v := r.str(key, "")
	if v == "" {
		r.fail(key, "es obligatoria")
	}
	return v
}

func (r *reader) duration(key string, def time.Duration) time.Duration {
	v := r.str(key, "")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		r.fail(key, "duración inválida (ej. 15s)")
		return def
	}
	return d
}

func (r *reader) positiveDurations(ds map[string]time.Duration) {
	for k, d := range ds {
		if d <= 0 {
			r.fail(k, "debe ser > 0")
		}
	}
}

func (r *reader) int64(key string, def int64) int64 {
	v := r.str(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		r.fail(key, "entero inválido")
		return def
	}
	return n
}

func (r *reader) int32(key string, def int32) int32 {
	v := r.str(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil {
		r.fail(key, "entero inválido")
		return def
	}
	return int32(n)
}

func (r *reader) ratio(key string, def float64) float64 {
	v := r.str(key, "")
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 || f > 1 {
		r.fail(key, "debe ser un número entre 0 y 1")
		return def
	}
	return f
}

func (r *reader) common(defaultService string) Common {
	c := Common{
		Env:              Env(r.str("SEGUROS_ENV", string(EnvDevelopment))),
		ServiceName:      r.str("OTEL_SERVICE_NAME", defaultService),
		ServiceVersion:   r.str("SEGUROS_VERSION", "dev"),
		OTLPEndpoint:     r.str("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		TraceSampleRatio: r.ratio("SEGUROS_TRACE_SAMPLE_RATIO", 1),
	}
	switch c.Env {
	case EnvDevelopment, EnvTest, EnvStaging, EnvProduction:
	default:
		r.fail("SEGUROS_ENV", "debe ser development, test, staging o production")
	}
	if err := c.LogLevel.UnmarshalText([]byte(r.str("SEGUROS_LOG_LEVEL", "info"))); err != nil {
		r.fail("SEGUROS_LOG_LEVEL", "debe ser debug, info, warn o error")
	}
	if c.OTLPEndpoint != "" {
		if u, err := url.Parse(c.OTLPEndpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			r.fail("OTEL_EXPORTER_OTLP_ENDPOINT", "URL http(s) inválida")
		} else if c.Env.IsProductionLike() && u.Scheme != "https" {
			r.fail("OTEL_EXPORTER_OTLP_ENDPOINT", "debe usar https en staging/producción")
		}
	}
	return c
}

func (r *reader) database(key string) Database {
	db := Database{
		URL:              NewSecret(r.required(key)),
		MaxConns:         r.int32("SEGUROS_DB_MAX_CONNS", 20),
		MinConns:         r.int32("SEGUROS_DB_MIN_CONNS", 2),
		MaxConnLifetime:  r.duration("SEGUROS_DB_MAX_CONN_LIFETIME", time.Hour),
		StatementTimeout: r.duration("SEGUROS_DB_STATEMENT_TIMEOUT", 10*time.Second),
	}
	if db.MaxConns < 1 || db.MinConns < 0 || db.MinConns > db.MaxConns {
		r.fail("SEGUROS_DB_MAX_CONNS", "requiere 0 ≤ MIN_CONNS ≤ MAX_CONNS y MAX_CONNS ≥ 1")
	}
	r.positiveDurations(map[string]time.Duration{
		"SEGUROS_DB_MAX_CONN_LIFETIME": db.MaxConnLifetime,
		"SEGUROS_DB_STATEMENT_TIMEOUT": db.StatementTimeout,
	})
	return db
}

// postgresURL valida el DSN sin incluirlo en el mensaje de error (contiene la contraseña).
func (r *reader) postgresURL(key string, s Secret, env Env) {
	if s.IsEmpty() {
		return
	}
	u, err := url.Parse(s.Reveal())
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		r.fail(key, "debe ser una URL postgres://usuario:clave@host:puerto/bd")
		return
	}
	if env.IsProductionLike() {
		switch u.Query().Get("sslmode") {
		case "require", "verify-ca", "verify-full":
		default:
			r.fail(key, "staging/producción exige sslmode=require|verify-ca|verify-full")
		}
	}
}

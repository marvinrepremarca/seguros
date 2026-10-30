// Package obs configura la observabilidad: logs estructurados (slog JSON) con redacción de datos
// sensibles y correlación (trace_id, tenant_id, user_id, request_id), y OpenTelemetry
// (trazas y métricas vía OTLP).
package obs

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

const redacted = "[REDACTED]"

// sensitiveKeyParts: si la clave de un atributo contiene alguna de estas partes, su valor se
// redacta. Es una red de seguridad: los tipos sensibles además implementan slog.LogValuer.
var sensitiveKeyParts = [...]string{ //nolint:gochecknoglobals // arreglo inmutable de solo lectura
	"password", "passwd", "secret", "token", "authorization", "cookie", "session",
	"api_key", "apikey", "dsn", "credential", "private_key",
	"document", "documento", "cedula", "email", "phone", "telefono", "address", "direccion",
	"health", "salud", "account_number", "cuenta",
}

// LoggerOptions configura NewLogger.
type LoggerOptions struct {
	Level   slog.Leveler
	Service string
	Version string
	Env     string
}

// NewLogger crea el logger JSON del proceso.
func NewLogger(w io.Writer, opts LoggerOptions) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       opts.Level,
		ReplaceAttr: redactAttr,
	})
	return slog.New(contextHandler{inner: h}).With(
		slog.String("service", opts.Service),
		slog.String("version", opts.Version),
		slog.String("env", opts.Env),
	)
}

func redactAttr(_ []string, a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindGroup {
		return a
	}
	if isSensitiveKey(a.Key) {
		return slog.String(a.Key, redacted)
	}
	return a
}

func isSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	if k == "nit" || k == "cc" { // claves cortas: coincidencia exacta para no redactar "limit" o "unit"
		return true
	}
	for _, part := range sensitiveKeyParts {
		if strings.Contains(k, part) {
			return true
		}
	}
	return false
}

// contextHandler añade los campos de correlación presentes en el contexto, siempre en la raíz
// del JSON aunque el logger tenga grupos (los grupos se aplican aquí y no en el handler interno).
type contextHandler struct {
	inner  slog.Handler // solo con los atributos fijados antes de abrir cualquier grupo
	groups []group
}

type group struct {
	name  string
	attrs []slog.Attr
}

func (h contextHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		out.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
	}
	if id, ok := reqctx.TenantID(ctx); ok {
		out.AddAttrs(slog.String("tenant_id", id.String()))
	}
	if id, ok := reqctx.UserID(ctx); ok {
		out.AddAttrs(slog.String("user_id", id.String()))
	}
	if id := reqctx.RequestID(ctx); id != "" {
		out.AddAttrs(slog.String("request_id", id))
	}

	attrs := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	for i := len(h.groups) - 1; i >= 0; i-- {
		g := h.groups[i]
		nested := append(append(make([]slog.Attr, 0, len(g.attrs)+len(attrs)), g.attrs...), attrs...)
		attrs = []slog.Attr{{Key: g.name, Value: slog.GroupValue(nested...)}}
	}
	out.AddAttrs(attrs...)
	return h.inner.Handle(ctx, out)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(h.groups) == 0 {
		return contextHandler{inner: h.inner.WithAttrs(attrs)}
	}
	groups := clone(h.groups)
	last := &groups[len(groups)-1]
	last.attrs = append(append(make([]slog.Attr, 0, len(last.attrs)+len(attrs)), last.attrs...), attrs...)
	return contextHandler{inner: h.inner, groups: groups}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return contextHandler{inner: h.inner, groups: append(clone(h.groups), group{name: name})}
}

func clone(gs []group) []group {
	return append(make([]group, 0, len(gs)+1), gs...)
}

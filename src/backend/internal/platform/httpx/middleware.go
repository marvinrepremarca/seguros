package httpx

import (
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// HeaderRequestID es la cabecera de correlación de peticiones.
const HeaderRequestID = "X-Request-Id"

// requestIDPattern limita los request-id entrantes (vienen de Traefik o del cliente) a un formato
// inocuo para logs y cabeceras.
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`) //nolint:gochecknoglobals // regexp inmutable

// Recoverer convierte un panic en un 500 problem+json genérico y registra el stack en el log.
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer recoverPanic(logger, w, r)
			next.ServeHTTP(w, r)
		})
	}
}

// recoverPanic debe invocarse directamente con defer para que recover() funcione.
func recoverPanic(logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	rec := recover()
	if rec == nil {
		return
	}
	if rec == http.ErrAbortHandler { //nolint:errorlint // comparación de identidad documentada por net/http
		panic(rec) // el servidor aborta la respuesta en silencio
	}
	logger.ErrorContext(r.Context(), "panic en handler HTTP", "panic", rec, "stack", string(debug.Stack()))
	WriteProblem(w, r, http.StatusInternalServerError, CodeInternal, titleForStatus(http.StatusInternalServerError))
}

// RequestID reutiliza un X-Request-Id entrante válido o genera un UUIDv7, lo pone en el
// contexto y lo devuelve en la respuesta.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderRequestID)
		if !requestIDPattern.MatchString(id) {
			u, err := uuid.NewV7()
			if err != nil {
				u = uuid.New()
			}
			id = u.String()
		}
		w.Header().Set(HeaderRequestID, id)
		next.ServeHTTP(w, r.WithContext(reqctx.WithRequestID(r.Context(), id)))
	})
}

// SecurityHeaders fija las cabeceras de seguridad de la API (rules/security.md). La SPA define
// su propia CSP con nonces al servir el HTML.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// MaxBodyBytes limita el tamaño del cuerpo de las peticiones. Al exceder el límite, la lectura
// devuelve *http.MaxBytesError, que WriteError traduce a 413.
func MaxBodyBytes(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				WriteProblem(w, r, http.StatusRequestEntityTooLarge, CodeBodyTooLarge, "El cuerpo de la petición excede el tamaño permitido")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

// RouteTelemetry nombra el span HTTP con el patrón de ruta de chi (no con la URL concreta, que
// puede contener IDs) una vez resuelto el ruteo.
func RouteTelemetry(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		if pattern := routePattern(r); pattern != "" {
			trace.SpanFromContext(r.Context()).SetName(r.Method + " " + pattern)
		}
	})
}

// AccessLog registra método, ruta (patrón, sin query string ni IDs), estado y duración.
func AccessLog(logger *slog.Logger, clock kernel.Clock) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := clock.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			level := slog.LevelInfo
			if status >= http.StatusInternalServerError {
				level = slog.LevelError
			}
			logger.LogAttrs(r.Context(), level, "http request",
				slog.String("method", r.Method),
				slog.String("route", routePattern(r)),
				slog.Int("status", status),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Duration("duration", clock.Now().Sub(start)),
			)
		})
	}
}

func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		return rc.RoutePattern()
	}
	return ""
}

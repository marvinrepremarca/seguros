// Package httpx contiene el servidor HTTP de plataforma: router chi con los middlewares base
// (recover, request-id, OpenTelemetry, cabeceras de seguridad, límite de body, access log),
// respuestas de error problem+json (RFC 9457) y endpoints de salud.
package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// ContentTypeProblem es el media type de RFC 9457.
const ContentTypeProblem = "application/problem+json"

// Códigos estables de errores de plataforma (campo `type`).
const (
	CodeInternal         = "platform.internal"
	CodeNotFound         = "platform.not_found"
	CodeMethodNotAllowed = "platform.method_not_allowed"
	CodeBodyTooLarge     = "platform.body_too_large"
	CodeUnavailable      = "platform.unavailable"
)

// Problem es el cuerpo problem+json. `type` es el código estable del error; nunca incluye
// detalles internos (stack, SQL, mensajes de librerías).
type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail,omitempty"`
	Instance  string `json:"instance,omitempty"`
	TraceID   string `json:"traceId,omitempty"`
	RequestID string `json:"requestId,omitempty"`
}

// WriteProblem responde un problem+json con trazabilidad.
func WriteProblem(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	p := Problem{
		Type:      code,
		Title:     title,
		Status:    status,
		Instance:  r.URL.Path,
		RequestID: reqctx.RequestID(r.Context()),
	}
	if sc := trace.SpanContextFromContext(r.Context()); sc.HasTraceID() {
		p.TraceID = sc.TraceID().String()
	}
	w.Header().Set("Content-Type", ContentTypeProblem)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(p); err != nil {
		slog.DebugContext(r.Context(), "escribiendo problem+json", "error", err)
	}
}

// WriteError traduce un error a problem+json. Los errores de dominio se mapean por su clase y
// exponen solo su código estable; cualquier otro error es un 500 genérico y su detalle va al log.
func WriteError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		WriteProblem(w, r, http.StatusRequestEntityTooLarge, CodeBodyTooLarge, "El cuerpo de la petición excede el tamaño permitido")
		return
	}
	if de, ok := kernel.AsDomainError(err); ok {
		status := statusForKind(de.Kind())
		if status >= http.StatusInternalServerError {
			logger.ErrorContext(r.Context(), "error de dominio sin clase HTTP", "code", de.Code(), "error", err)
		}
		WriteProblem(w, r, status, de.Code(), titleForStatus(status))
		return
	}
	logger.ErrorContext(r.Context(), "error no controlado", "error", err)
	WriteProblem(w, r, http.StatusInternalServerError, CodeInternal, titleForStatus(http.StatusInternalServerError))
}

func statusForKind(k kernel.ErrorKind) int {
	switch k {
	case kernel.KindValidation:
		return http.StatusUnprocessableEntity
	case kernel.KindNotFound:
		return http.StatusNotFound
	case kernel.KindConflict:
		return http.StatusConflict
	case kernel.KindForbidden:
		return http.StatusForbidden
	case kernel.KindUnauthorized:
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}

// titleForStatus devuelve un título genérico en es-CO (sin detalles del error).
func titleForStatus(status int) string {
	switch status {
	case http.StatusUnprocessableEntity:
		return "La solicitud no es válida"
	case http.StatusNotFound:
		return "Recurso no encontrado"
	case http.StatusConflict:
		return "La operación entra en conflicto con el estado actual"
	case http.StatusForbidden:
		return "No tiene permiso para esta operación"
	case http.StatusUnauthorized:
		return "Se requiere autenticación"
	default:
		return "Error interno"
	}
}

package httpx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

// AuthorizeFunc autoriza una operación del contrato (ver authz.OperationGuard.Check).
type AuthorizeFunc func(ctx context.Context, operationID string) error

// Contract aplica el contrato OpenAPI de un módulo a sus rutas: primero autoriza la operación
// (x-permission; 401/403 antes de revelar nada sobre la forma de la petición) y luego valida la
// petición contra el esquema (parámetros, longitudes, patrones, cuerpo sin campos desconocidos →
// 400). Las peticiones que no corresponden a ninguna operación pasan sin tocar (chi responde 404/405).
func Contract(spec *openapi3.T, authorize AuthorizeFunc, logger *slog.Logger) (func(http.Handler) http.Handler, error) {
	if spec == nil {
		return nil, errors.New("httpx: contrato vacío")
	}
	spec.Servers = nil // el contrato no fija host: se valida solo la ruta
	router, err := gorillamux.NewRouter(spec)
	if err != nil {
		return nil, fmt.Errorf("httpx: router del contrato: %w", err)
	}
	opts := &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route, params, err := router.FindRoute(r)
			if err != nil {
				if !errors.Is(err, routers.ErrPathNotFound) && !errors.Is(err, routers.ErrMethodNotAllowed) {
					logger.WarnContext(r.Context(), "resolviendo operación del contrato", "error", err)
				}
				next.ServeHTTP(w, r)
				return
			}
			if err := authorize(r.Context(), route.Operation.OperationID); err != nil {
				WriteError(w, r, logger, err)
				return
			}
			in := &openapi3filter.RequestValidationInput{Request: r, PathParams: params, Route: route, Options: opts}
			if err := openapi3filter.ValidateRequest(r.Context(), in); err != nil {
				var maxBytes *http.MaxBytesError
				if errors.As(err, &maxBytes) {
					WriteProblem(w, r, http.StatusRequestEntityTooLarge, CodeBodyTooLarge, "El cuerpo de la petición excede el tamaño permitido")
					return
				}
				logger.DebugContext(r.Context(), "petición fuera de contrato", "operation", route.Operation.OperationID)
				WriteProblem(w, r, http.StatusBadRequest, CodeBadRequest, "La solicitud no es válida")
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

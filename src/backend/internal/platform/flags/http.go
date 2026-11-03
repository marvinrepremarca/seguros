package flags

import (
	"net/http"

	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Require protege un grupo de rutas con un flag. Va después de la autenticación: sin tenant
// responde 401; con el flag apagado responde 404 problem+json, idéntico a una ruta inexistente
// (no revela que el módulo existe). El backend es siempre la autoridad (rules/feature-flags.md).
func Require(checker Checker, key Key) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := reqctx.TenantID(r.Context()); !ok {
				httpx.WriteProblem(w, r, http.StatusUnauthorized, "authz.unauthenticated", "Se requiere autenticación")
				return
			}
			if !checker.IsEnabled(r.Context(), key) {
				httpx.WriteProblem(w, r, http.StatusNotFound, httpx.CodeNotFound, "Recurso no encontrado")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

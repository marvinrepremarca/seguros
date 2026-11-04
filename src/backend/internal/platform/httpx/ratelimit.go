package httpx

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/httprate"

	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// ClientIP devuelve la IP del cliente. Con trustedHeader vacío usa RemoteAddr (no falsificable).
// Detrás de Traefik se configura la cabecera que fija el proxy de confianza (ej. X-Real-IP, que
// Traefik sobrescribe); nunca una cabecera que el cliente pueda inyectar sin que el proxy la pise.
func ClientIP(r *http.Request, trustedHeader string) string {
	if trustedHeader != "" {
		if v := strings.TrimSpace(r.Header.Get(trustedHeader)); v != "" && net.ParseIP(v) != nil {
			return httprate.CanonicalizeIP(v)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return httprate.CanonicalizeIP(host)
}

// RateLimitByIP limita las peticiones por IP de cliente a las rutas que empiezan por prefix; el
// resto pasa sin límite. Al exceder responde 429 problem+json.
func RateLimitByIP(prefix string, requests int, window time.Duration, trustedHeader string) func(http.Handler) http.Handler {
	limiter := httprate.LimitBy(requests, window,
		func(r *http.Request) (string, error) { return ClientIP(r, trustedHeader), nil },
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			WriteProblem(w, r, http.StatusTooManyRequests, CodeRateLimited, "Demasiadas solicitudes; intente más tarde")
		}),
	)
	return func(next http.Handler) http.Handler {
		limited := limiter(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, prefix) {
				limited.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitByUser limita por usuario autenticado las rutas que cumplan match (va después de la
// autenticación). Sin usuario no aplica: esas peticiones las rechaza la autorización. El contador es
// por réplica hasta mover el rate limit a Valkey.
func RateLimitByUser(match func(*http.Request) bool, requests int, window time.Duration) func(http.Handler) http.Handler {
	limiter := httprate.LimitBy(requests, window,
		func(r *http.Request) (string, error) {
			user, _ := reqctx.UserID(r.Context())
			return user.String(), nil
		},
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			WriteProblem(w, r, http.StatusTooManyRequests, CodeRateLimited, "Demasiadas solicitudes; intente más tarde")
		}),
	)
	return func(next http.Handler) http.Handler {
		limited := limiter(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := reqctx.UserID(r.Context()); ok && match(r) {
				limited.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitByTenant limita por tenant autenticado las rutas que cumplan match: varios usuarios de una
// agencia no suman más que este tope. El contador es por réplica hasta mover el rate limit a Valkey.
func RateLimitByTenant(match func(*http.Request) bool, requests int, window time.Duration) func(http.Handler) http.Handler {
	limiter := httprate.LimitBy(requests, window,
		func(r *http.Request) (string, error) {
			tenant, _ := reqctx.TenantID(r.Context())
			return tenant.String(), nil
		},
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			WriteProblem(w, r, http.StatusTooManyRequests, CodeRateLimited, "Demasiadas solicitudes; intente más tarde")
		}),
	)
	return func(next http.Handler) http.Handler {
		limited := limiter(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := reqctx.TenantID(r.Context()); ok && match(r) {
				limited.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

package main

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marvinrepremarca/seguros/internal/modules/catalog"
	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt"
	"github.com/marvinrepremarca/seguros/internal/modules/identity"
	"github.com/marvinrepremarca/seguros/internal/modules/parties"
	"github.com/marvinrepremarca/seguros/internal/modules/policies"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// routerDeps son los módulos ya compuestos que expone la API.
type routerDeps struct {
	Logger       *slog.Logger
	Clock        kernel.Clock
	MaxBodyBytes int64
	Readiness    []httpx.ReadinessCheck
	ClientIPHdr  string
	Identity     *identity.Module
	FeatureMgmt  *featuremgmt.Module
	Parties      *parties.Module
	Catalog      *catalog.Module
	Policies     *policies.Module
	// SPA sirve la aplicación web con su CSP; nil en desarrollo con Vite.
	SPA http.Handler
}

// isDocumentLookup indica las rutas que leen o confirman un número de documento: revelarlo, buscarlo,
// y registrar o rectificar un tercero (un documento repetido responde 409).
func isDocumentLookup(r *http.Request) bool {
	const parties = "/api/v1/parties/parties"
	p := r.URL.Path
	switch {
	case r.Method == http.MethodPost && (p == parties || strings.HasSuffix(p, "/document/reveal") || strings.HasSuffix(p, "/search-by-document")):
		return strings.HasPrefix(p, parties)
	case r.Method == http.MethodPut:
		return strings.HasPrefix(p, parties+"/")
	default:
		return false
	}
}

// buildRouter compone el router de la API. Orden de middlewares (rules/architecture.md §API):
// plataforma (recover, request-id/otel, cabeceras, límite de body) → sesión/autenticación →
// CSRF → rate limit → rutas de cada módulo, cuyas operaciones aplican su x-permission.
func buildRouter(d routerDeps) (*chi.Mux, error) {
	root := httpx.NewRouter(httpx.RouterOptions{
		Logger: d.Logger, Clock: d.Clock, MaxBodyBytes: d.MaxBodyBytes, Readiness: d.Readiness,
	})
	var mountErr error
	root.Group(func(r chi.Router) {
		// Rate limit antes de cargar sesión o consultar la BD. Login más estricto (cada intento
		// cuesta un viaje al IdP); el callback admite reintentos legítimos.
		r.Use(httpx.RateLimitByIP("/bff/auth/login", 20, time.Minute, d.ClientIPHdr))
		r.Use(httpx.RateLimitByIP("/bff/auth/callback", 30, time.Minute, d.ClientIPHdr))
		// Búsqueda de personas: más estricta (rules/security.md §Rate limiting).
		r.Use(httpx.RateLimitByIP("/api/v1/parties/parties/search-by-document", 60, time.Minute, d.ClientIPHdr))
		r.Use(d.Identity.Middlewares()...)
		// Datos Restringidos: por usuario, para frenar la extracción masiva de documentos.
		r.Use(httpx.RateLimitByUser(isDocumentLookup, 30, time.Minute))
		r.Use(httpx.RateLimitByTenant(isDocumentLookup, 300, time.Minute))
		mountErr = errors.Join(d.Identity.Server.Mount(r), d.FeatureMgmt.Mount(r), d.Parties.Mount(r), d.Catalog.Mount(r), d.Policies.Mount(r))
	})
	if mountErr != nil {
		return nil, mountErr
	}
	if d.SPA != nil {
		root.NotFound(d.SPA.ServeHTTP) // pública; rutas no declaradas: reserva /api, /bff y /health
	}
	return root, nil
}

package main

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marvinrepremarca/seguros/internal/modules/billing"
	"github.com/marvinrepremarca/seguros/internal/modules/catalog"
	"github.com/marvinrepremarca/seguros/internal/modules/claims"
	"github.com/marvinrepremarca/seguros/internal/modules/commissions"
	"github.com/marvinrepremarca/seguros/internal/modules/compliance"
	"github.com/marvinrepremarca/seguros/internal/modules/crm"
	"github.com/marvinrepremarca/seguros/internal/modules/documents"
	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt"
	"github.com/marvinrepremarca/seguros/internal/modules/identity"
	"github.com/marvinrepremarca/seguros/internal/modules/integrations"
	"github.com/marvinrepremarca/seguros/internal/modules/notifications"
	"github.com/marvinrepremarca/seguros/internal/modules/parties"
	"github.com/marvinrepremarca/seguros/internal/modules/policies"
	"github.com/marvinrepremarca/seguros/internal/modules/quoting"
	"github.com/marvinrepremarca/seguros/internal/modules/renewals"
	"github.com/marvinrepremarca/seguros/internal/modules/reporting"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// routerDeps son los módulos ya compuestos que expone la API.
type routerDeps struct {
	Logger        *slog.Logger
	Clock         kernel.Clock
	MaxBodyBytes  int64
	Readiness     []httpx.ReadinessCheck
	ClientIPHdr   string
	Identity      *identity.Module
	FeatureMgmt   *featuremgmt.Module
	Parties       *parties.Module
	Catalog       *catalog.Module
	Policies      *policies.Module
	Billing       *billing.Module
	Commissions   *commissions.Module
	Renewals      *renewals.Module
	Crm           *crm.Module
	Quoting       *quoting.Module
	Claims        *claims.Module
	Compliance    *compliance.Module
	Documents     *documents.Module
	Notifications *notifications.Module
	Reporting     *reporting.Module
	Integrations  *integrations.Module
	// DocumentMaxBytes es el tamaño máximo de un archivo cargado.
	DocumentMaxBytes int64
	// SPA sirve la aplicación web con su CSP; nil en desarrollo con Vite.
	SPA http.Handler
}

// isAgentChange indica los traspasos y cambios de asesor.
func isAgentChange(r *http.Request) bool {
	p := r.URL.Path
	return r.Method == http.MethodPost && (p == "/api/v1/policies/portfolio-transfers" ||
		(strings.HasPrefix(p, "/api/v1/policies/policies/") && strings.HasSuffix(p, "/agent")) ||
		strings.HasSuffix(p, "/responsible-agent"))
}

// isMoneyWrite indica el registro y reverso de pagos y de liquidaciones.
func isMoneyWrite(r *http.Request) bool {
	p := r.URL.Path
	return r.Method == http.MethodPost && (p == "/api/v1/commissions/statements" ||
		(strings.HasPrefix(p, "/api/v1/billing/plans/") && (strings.HasSuffix(p, "/payments") || strings.HasSuffix(p, "/reverse"))))
}

// isHealthRead indica la lectura de datos de salud (Restringidos).
func isHealthRead(r *http.Request) bool {
	return r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/claims/claims/") && strings.HasSuffix(r.URL.Path, "/health-details")
}

// isNameSearch indica la búsqueda de clientes por nombre.
func isNameSearch(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v1/parties/parties" && r.URL.Query().Has("q")
}

// isAgentList indica el listado de asesores, que consulta al proveedor de identidad.
func isAgentList(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v1/policies/agents"
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

// isFileOrExport cubre carga y descarga de documentos, exporte de habeas data y detalle KYC.
func isFileOrExport(r *http.Request) bool {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/api/v1/documents/documents"):
		return r.Method == http.MethodPost || strings.HasSuffix(p, "/content")
	case strings.HasPrefix(p, "/api/v1/compliance/parties/"):
		return strings.HasSuffix(p, "/habeas-export") || strings.HasSuffix(p, "/details")
	default:
		return false
	}
}

// uploadBody admite archivos en las cargas de documentos; el sobre multipart suma 1 MiB.
func uploadBody(maxBytes int64) *httpx.LargeBody {
	if maxBytes <= 0 {
		return nil
	}
	const base = "/api/v1/documents/documents"
	return &httpx.LargeBody{
		Bytes:   maxBytes + 1<<20,
		Timeout: 2 * time.Minute,
		Match: func(r *http.Request) bool {
			if r.Method != http.MethodPost {
				return false
			}
			p := r.URL.Path
			return p == base || (strings.HasPrefix(p, base+"/") && strings.HasSuffix(p, "/versions"))
		},
	}
}

// buildRouter compone el router de la API. Orden de middlewares (rules/architecture.md §API):
// plataforma (recover, request-id/otel, cabeceras, límite de body) → sesión/autenticación →
// CSRF → rate limit → rutas de cada módulo, cuyas operaciones aplican su x-permission.
func buildRouter(d routerDeps) (*chi.Mux, error) {
	root := httpx.NewRouter(httpx.RouterOptions{
		Logger: d.Logger, Clock: d.Clock, MaxBodyBytes: d.MaxBodyBytes, Readiness: d.Readiness,
		LargeBody: uploadBody(d.DocumentMaxBytes),
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
		// Cada traspaso escribe y dispara lotes; la búsqueda por nombre permite extraer clientes.
		r.Use(httpx.RateLimitByUser(isAgentChange, 10, time.Minute))
		r.Use(httpx.RateLimitByUser(isNameSearch, 60, time.Minute))
		r.Use(httpx.RateLimitByUser(isAgentList, 60, time.Minute))
		r.Use(httpx.RateLimitByUser(isMoneyWrite, 120, time.Minute))
		r.Use(httpx.RateLimitByUser(isHealthRead, 30, time.Minute))
		// Archivos y exportes (rules/security.md §Rate limiting).
		r.Use(httpx.RateLimitByUser(isFileOrExport, 30, time.Minute))
		r.Use(httpx.RateLimitByTenant(isFileOrExport, 300, time.Minute))
		mountErr = errors.Join(d.Identity.Server.Mount(r), d.FeatureMgmt.Mount(r), d.Parties.Mount(r), d.Catalog.Mount(r), d.Policies.Mount(r), d.Billing.Mount(r), d.Commissions.Mount(r), d.Renewals.Mount(r), d.Crm.Mount(r), d.Quoting.Mount(r), d.Claims.Mount(r), d.Compliance.Mount(r), d.Documents.Mount(r), d.Notifications.Mount(r), d.Reporting.Mount(r), d.Integrations.Mount(r))
	})
	if mountErr != nil {
		return nil, mountErr
	}
	if d.SPA != nil {
		root.NotFound(d.SPA.ServeHTTP) // pública; rutas no declaradas: reserva /api, /bff y /health
	}
	return root, nil
}

package main

import (
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marvinrepremarca/seguros/internal/modules/identity"
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
}

// buildRouter compone el router de la API. Orden de middlewares (rules/architecture.md §API):
// plataforma (recover, request-id/otel, cabeceras, límite de body) → sesión/autenticación →
// CSRF → rate limit → rutas de cada módulo, cuyas operaciones aplican su x-permission.
func buildRouter(d routerDeps) *chi.Mux {
	root := httpx.NewRouter(httpx.RouterOptions{
		Logger: d.Logger, Clock: d.Clock, MaxBodyBytes: d.MaxBodyBytes, Readiness: d.Readiness,
	})
	root.Group(func(r chi.Router) {
		r.Use(d.Identity.Middlewares()...)
		r.Use(httpx.RateLimitByIP("/bff/auth/", 30, time.Minute, d.ClientIPHdr))
		d.Identity.Server.Mount(r)
	})
	return root
}

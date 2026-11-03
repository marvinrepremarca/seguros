package main

import (
	"errors"
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt"
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
	FeatureMgmt  *featuremgmt.Module
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
		r.Use(d.Identity.Middlewares()...)
		r.Use(httpx.RateLimitByIP("/bff/auth/", 30, time.Minute, d.ClientIPHdr))
		mountErr = errors.Join(d.Identity.Server.Mount(r), d.FeatureMgmt.Mount(r))
	})
	if mountErr != nil {
		return nil, mountErr
	}
	return root, nil
}

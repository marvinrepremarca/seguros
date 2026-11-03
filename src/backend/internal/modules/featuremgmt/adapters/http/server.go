// Package featuremgmthttp expone los flags del tenant: /bff/features para la SPA y la consulta y
// cambio de overrides en /api/v1/featuremgmt/flags. Solo mapea HTTP ↔ casos de uso.
package featuremgmthttp

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt/app"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
)

// Options configura el servidor.
type Options struct {
	List    *app.ListTenantFlagsHandler
	Set     *app.SetTenantFlagHandler
	Checker flags.Checker
	Reg     *flags.Registry
	Policy  *authz.Policy
	Logger  *slog.Logger
}

// Server implementa StrictServerInterface.
type Server struct {
	opts Options
}

var _ StrictServerInterface = (*Server)(nil)

// NewServer construye el servidor.
func NewServer(opts Options) *Server { return &Server{opts: opts} }

// Mount registra las rutas con el contrato (autorización por x-permission + validación).
func (s *Server) Mount(r chi.Router) error {
	spec, err := GetSpec()
	if err != nil {
		return err
	}
	guard, err := authz.NewOperationGuard(spec, s.opts.Policy)
	if err != nil {
		return err
	}
	contract, err := httpx.Contract(spec, guard.Check, s.opts.Logger)
	if err != nil {
		return err
	}
	strictGuard := func(f StrictHandlerFunc, operationID string) StrictHandlerFunc {
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
			if err := guard.Check(ctx, operationID); err != nil {
				return nil, err
			}
			return f(ctx, w, r, req)
		}
	}
	h := NewStrictHandlerWithOptions(s, []StrictMiddlewareFunc{strictGuard}, StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			httpx.WriteProblem(w, r, http.StatusBadRequest, httpx.CodeBadRequest, "La solicitud no es válida")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) { httpx.WriteError(w, r, s.opts.Logger, err) },
	})
	r.Group(func(g chi.Router) {
		g.Use(contract)
		HandlerWithOptions(h, ChiServerOptions{BaseRouter: g})
	})
	return nil
}

// GetFeatures implementa StrictServerInterface: evalúa vía el Checker (OpenFeature), con el contexto
// autenticado, los flags que la SPA necesita (módulos, funcionalidades y releases). Los flags ops
// son operativos del backend y no se exponen.
func (s *Server) GetFeatures(ctx context.Context, _ GetFeaturesRequestObject) (GetFeaturesResponseObject, error) {
	out := map[string]bool{}
	for _, k := range s.opts.Reg.Keys() {
		if def, _ := s.opts.Reg.Get(k); def.Kind == flags.KindOps {
			continue
		}
		out[string(k)] = s.opts.Checker.IsEnabled(ctx, k)
	}
	return GetFeatures200JSONResponse{Flags: out}, nil
}

// ListFlags implementa StrictServerInterface.
func (s *Server) ListFlags(ctx context.Context, _ ListFlagsRequestObject) (ListFlagsResponseObject, error) {
	views, err := s.opts.List.Handle(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]Flag, 0, len(views))
	for _, v := range views {
		items = append(items, toFlag(v))
	}
	return ListFlags200JSONResponse{Items: items}, nil
}

// SetFlag implementa StrictServerInterface.
func (s *Server) SetFlag(ctx context.Context, req SetFlagRequestObject) (SetFlagResponseObject, error) {
	var enabled *bool
	switch req.Body.Action {
	case Enable:
		v := true
		enabled = &v
	case Disable:
		v := false
		enabled = &v
	case Clear:
	}
	v, err := s.opts.Set.Handle(ctx, app.SetTenantFlagCommand{Key: flags.Key(req.Key), Enabled: enabled, Reason: req.Body.Reason})
	if err != nil {
		return nil, err
	}
	return SetFlag200JSONResponse(toFlag(v)), nil
}

func toFlag(v app.FlagView) Flag {
	requires := make([]string, 0, len(v.Definition.Requires))
	for _, k := range v.Definition.Requires {
		requires = append(requires, string(k))
	}
	f := Flag{
		Key: string(v.Definition.Key), Kind: FlagKind(v.Definition.Kind), Description: v.Definition.Description,
		Enabled: v.Resolution.Enabled, Source: FlagSource(v.Resolution.Source),
		TenantEditable: v.Definition.TenantEditable, Requires: requires,
	}
	if v.Override != nil {
		f.Override = &Override{Enabled: v.Override.Enabled, Reason: v.Override.Reason, UpdatedAt: v.Override.UpdatedAt}
	}
	return f
}

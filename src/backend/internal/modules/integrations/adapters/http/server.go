// Package integrationshttp expone integraciones. Solo mapea HTTP ↔ casos de uso.
package integrationshttp

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/marvinrepremarca/seguros/internal/modules/integrations/app"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
)

// Server implementa StrictServerInterface.
type Server struct {
	svc    *app.Service
	policy *authz.Policy
	logger *slog.Logger
}

var _ StrictServerInterface = (*Server)(nil)

// NewServer construye el servidor.
func NewServer(svc *app.Service, policy *authz.Policy, logger *slog.Logger) *Server {
	return &Server{svc: svc, policy: policy, logger: logger}
}

// Mount registra las rutas con el contrato.
func (s *Server) Mount(r chi.Router) error {
	spec, err := GetSpec()
	if err != nil {
		return err
	}
	guard, err := authz.NewOperationGuard(spec, s.policy)
	if err != nil {
		return err
	}
	contract, err := httpx.Contract(spec, guard.Check, s.logger)
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
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) { httpx.WriteError(w, r, s.logger, err) },
	})
	r.Group(func(g chi.Router) {
		g.Use(contract)
		HandlerWithOptions(h, ChiServerOptions{BaseRouter: g, ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			httpx.WriteProblem(w, r, http.StatusBadRequest, httpx.CodeBadRequest, "La solicitud no es válida")
		}})
	})
	return nil
}

// ListConnectors implementa StrictServerInterface.
func (s *Server) ListConnectors(ctx context.Context, _ ListConnectorsRequestObject) (ListConnectorsResponseObject, error) {
	rows, err := s.svc.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(ListConnectors200JSONResponse, 0, len(rows))
	for _, c := range rows {
		out = append(out, Connector{Key: c.Key, Name: c.Name, Simulated: c.Simulated, Enabled: c.Enabled})
	}
	return out, nil
}

// RequestConnectorQuote implementa StrictServerInterface.
func (s *Server) RequestConnectorQuote(ctx context.Context, req RequestConnectorQuoteRequestObject) (RequestConnectorQuoteResponseObject, error) {
	r, err := s.svc.Quote(ctx, req.Connector, req.Body.LineOfBusiness, req.Body.SumInsured)
	if err != nil {
		return nil, err
	}
	return RequestConnectorQuote200JSONResponse{Reference: r.Reference, NetPremium: r.NetPremium.Amount(), Currency: string(r.NetPremium.Currency()), Simulated: r.Simulated}, nil
}

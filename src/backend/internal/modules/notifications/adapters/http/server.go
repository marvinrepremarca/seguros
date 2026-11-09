// Package notificationshttp expone notificaciones. Solo mapea HTTP ↔ casos de uso.
package notificationshttp

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/notifications/app"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
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

// ListMessages implementa StrictServerInterface.
func (s *Server) ListMessages(ctx context.Context, req ListMessagesRequestObject) (ListMessagesResponseObject, error) {
	var before uuid.NullUUID
	if req.Params.Cursor != nil && *req.Params.Cursor != "" {
		id, err := uuid.Parse(*req.Params.Cursor)
		if err != nil {
			return nil, kernel.NewError(kernel.KindValidation, "notifications.cursor.invalid", "el cursor no es válido")
		}
		before = uuid.NullUUID{UUID: id, Valid: true}
	}
	var limit int32
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	p, err := s.svc.List(ctx, before, limit)
	if err != nil {
		return nil, err
	}
	out := ListMessages200JSONResponse{Items: make([]Message, 0, len(p.Items))}
	for _, m := range p.Items {
		item := Message{Id: m.ID, Template: m.Template, Channel: m.Channel, PolicyId: m.PolicyID, Status: MessageStatus(m.Status), Attempts: m.Attempts, CreatedAt: m.CreatedAt, SentAt: m.SentAt}
		if m.SkipReason != "" {
			r := MessageSkipReason(m.SkipReason)
			item.SkipReason = &r
		}
		out.Items = append(out.Items, item)
	}
	if p.Next.Valid {
		next := p.Next.UUID.String()
		out.NextCursor = &next
	}
	return out, nil
}

// RetryMessage implementa StrictServerInterface.
func (s *Server) RetryMessage(ctx context.Context, req RetryMessageRequestObject) (RetryMessageResponseObject, error) {
	if err := s.svc.Retry(ctx, req.MessageId); err != nil {
		return nil, err
	}
	return RetryMessage204Response{}, nil
}

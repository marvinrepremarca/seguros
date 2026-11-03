// Package partieshttp expone terceros y autorizaciones. Solo mapea HTTP ↔ casos de uso.
package partieshttp

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/parties/app"
	"github.com/marvinrepremarca/seguros/internal/modules/parties/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
)

// Handlers son los casos de uso que expone el servidor.
type Handlers struct {
	Create   *app.CreatePartyHandler
	Update   *app.UpdatePartyHandler
	Get      *app.GetPartyHandler
	List     *app.ListPartiesHandler
	Search   *app.SearchByDocumentHandler
	Reveal   *app.RevealDocumentHandler
	Register *app.RegisterConsentHandler
	Consents *app.ListConsentsHandler
}

// Server implementa StrictServerInterface.
type Server struct {
	h      Handlers
	policy *authz.Policy
	logger *slog.Logger
}

var _ StrictServerInterface = (*Server)(nil)

// NewServer construye el servidor.
func NewServer(h Handlers, policy *authz.Policy, logger *slog.Logger) *Server {
	return &Server{h: h, policy: policy, logger: logger}
}

// Mount registra las rutas con el contrato (autorización por x-permission + validación).
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
		HandlerWithOptions(h, ChiServerOptions{BaseRouter: g})
	})
	return nil
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toParty(s domain.PartySnapshot) Party {
	return Party{
		Id: uuid.UUID(s.ID), Kind: PartyKind(s.Kind), Name: s.Name, DocumentType: DocumentType(s.DocumentType), DocumentLast4: s.DocumentLast4,
		Email: optional(s.Contact.Email), Phone: optional(s.Contact.Phone), Address: optional(s.Contact.Address), City: optional(s.Contact.City),
		Version: s.Version, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

func toConsent(c domain.Consent) Consent {
	return Consent{
		Id: c.ID, Status: ConsentStatus(c.Status), Purpose: c.Purpose, TextVersion: c.TextVersion,
		ConsentDate: c.ConsentDate, RecordedAt: c.RecordedAt,
	}
}

// ListParties implementa StrictServerInterface.
func (s *Server) ListParties(ctx context.Context, req ListPartiesRequestObject) (ListPartiesResponseObject, error) {
	q := app.ListPartiesQuery{Q: deref(req.Params.Q), Cursor: deref(req.Params.Cursor)}
	if req.Params.Limit != nil {
		q.Limit = *req.Params.Limit
	}
	page, err := s.h.List.Handle(ctx, q)
	if err != nil {
		return nil, err
	}
	items := make([]Party, 0, len(page.Items))
	for _, p := range page.Items {
		items = append(items, toParty(p))
	}
	return ListParties200JSONResponse{Items: items, NextCursor: optional(page.NextCursor)}, nil
}

// CreateParty implementa StrictServerInterface.
func (s *Server) CreateParty(ctx context.Context, req CreatePartyRequestObject) (CreatePartyResponseObject, error) {
	b := req.Body
	out, err := s.h.Create.Handle(ctx, app.CreatePartyCommand{
		Kind: domain.Kind(b.Kind), Name: b.Name, DocumentType: domain.DocumentType(b.DocumentType), DocumentNumber: b.DocumentNumber,
		Contact: domain.Contact{Email: deref(b.Email), Phone: deref(b.Phone), Address: deref(b.Address), City: deref(b.City)},
	})
	if err != nil {
		return nil, err
	}
	return CreateParty201JSONResponse(toParty(out)), nil
}

// SearchPartyByDocument implementa StrictServerInterface.
func (s *Server) SearchPartyByDocument(ctx context.Context, req SearchPartyByDocumentRequestObject) (SearchPartyByDocumentResponseObject, error) {
	out, err := s.h.Search.Handle(ctx, app.DocumentInput{Type: domain.DocumentType(req.Body.DocumentType), Number: req.Body.DocumentNumber})
	if err != nil {
		return nil, err
	}
	return SearchPartyByDocument200JSONResponse(toParty(out)), nil
}

// GetParty implementa StrictServerInterface.
func (s *Server) GetParty(ctx context.Context, req GetPartyRequestObject) (GetPartyResponseObject, error) {
	out, err := s.h.Get.Handle(ctx, domain.PartyID(req.PartyId))
	if err != nil {
		return nil, err
	}
	return GetParty200JSONResponse(toParty(out)), nil
}

// UpdateParty implementa StrictServerInterface.
func (s *Server) UpdateParty(ctx context.Context, req UpdatePartyRequestObject) (UpdatePartyResponseObject, error) {
	b := req.Body
	cmd := app.UpdatePartyCommand{
		ID: domain.PartyID(req.PartyId), Version: b.Version, Name: b.Name,
		Contact: domain.Contact{Email: deref(b.Email), Phone: deref(b.Phone), Address: deref(b.Address), City: deref(b.City)},
	}
	if b.Document != nil {
		cmd.Document = &app.DocumentInput{Type: domain.DocumentType(b.Document.DocumentType), Number: b.Document.DocumentNumber}
	}
	out, err := s.h.Update.Handle(ctx, cmd)
	if err != nil {
		return nil, err
	}
	return UpdateParty200JSONResponse(toParty(out)), nil
}

// RevealPartyDocument implementa StrictServerInterface.
func (s *Server) RevealPartyDocument(ctx context.Context, req RevealPartyDocumentRequestObject) (RevealPartyDocumentResponseObject, error) {
	docType, number, err := s.h.Reveal.Handle(ctx, domain.PartyID(req.PartyId))
	if err != nil {
		return nil, err
	}
	return RevealPartyDocument200JSONResponse{DocumentType: DocumentType(docType), DocumentNumber: number}, nil
}

// ListPartyConsents implementa StrictServerInterface.
func (s *Server) ListPartyConsents(ctx context.Context, req ListPartyConsentsRequestObject) (ListPartyConsentsResponseObject, error) {
	rows, err := s.h.Consents.Handle(ctx, domain.PartyID(req.PartyId))
	if err != nil {
		return nil, err
	}
	items := make([]Consent, 0, len(rows))
	for _, c := range rows {
		items = append(items, toConsent(c))
	}
	return ListPartyConsents200JSONResponse{Items: items}, nil
}

// RegisterPartyConsent implementa StrictServerInterface.
func (s *Server) RegisterPartyConsent(ctx context.Context, req RegisterPartyConsentRequestObject) (RegisterPartyConsentResponseObject, error) {
	b := req.Body
	out, err := s.h.Register.Handle(ctx, app.RegisterConsentCommand{
		PartyID: domain.PartyID(req.PartyId), Status: domain.ConsentStatus(b.Status), Purpose: b.Purpose,
		TextVersion: b.TextVersion, ConsentDate: b.ConsentDate,
	})
	if err != nil {
		return nil, err
	}
	return RegisterPartyConsent201JSONResponse(toConsent(out)), nil
}

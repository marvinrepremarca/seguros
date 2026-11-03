// Package cataloghttp expone el catálogo. Solo mapea HTTP ↔ casos de uso.
package cataloghttp

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/catalog/app"
	"github.com/marvinrepremarca/seguros/internal/modules/catalog/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/catalog/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
)

// Server implementa StrictServerInterface.
type Server struct {
	svc     *app.Service
	schemas contracts.RiskSchemas
	policy  *authz.Policy
	logger  *slog.Logger
}

var _ StrictServerInterface = (*Server)(nil)

// NewServer construye el servidor.
func NewServer(svc *app.Service, schemas contracts.RiskSchemas, policy *authz.Policy, logger *slog.Logger) *Server {
	return &Server{svc: svc, schemas: schemas, policy: policy, logger: logger}
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

func pageQuery(cursor *string, limit *int32) app.PageQuery {
	var q app.PageQuery
	if cursor != nil {
		q.Cursor = *cursor
	}
	if limit != nil {
		q.Limit = *limit
	}
	return q
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func mapItems[T, R any](items []T, f func(T) R) []R {
	out := make([]R, 0, len(items))
	for _, i := range items {
		out = append(out, f(i))
	}
	return out
}

func toInsurer(i domain.Item) Insurer {
	return Insurer{Id: i.ID, Name: i.Name, Active: i.Active, Version: i.Version}
}

func toLine(l domain.LineOfBusiness) LineOfBusiness {
	return LineOfBusiness{Id: l.ID, Name: l.Name, Active: l.Active, Version: l.Version, RiskSchemaKey: RiskSchemaKey(l.RiskSchemaKey)}
}

func toProduct(p domain.InsuranceProduct) Product {
	return Product{Id: p.ID, Name: p.Name, Active: p.Active, Version: p.Version, InsurerId: p.InsurerID, LineOfBusinessId: p.LineOfBusinessID}
}

func toAgreement(a domain.CommissionAgreement) CommissionAgreement {
	out := CommissionAgreement{
		Id: a.ID, InsurerId: a.InsurerID, LineOfBusinessId: a.LineOfBusinessID, Basis: CommissionBasis(a.Basis),
		Percentage: a.Percentage, ValidFrom: a.Validity.From(), ValidTo: a.Validity.To(), Version: a.Version,
	}
	if a.ProductID.Valid {
		out.ProductId = &a.ProductID.UUID
	}
	return out
}

func update(id uuid.UUID, b UpdateNamedRequest) app.UpdateItemCommand {
	return app.UpdateItemCommand{ID: id, Name: b.Name, Active: b.Active, Version: b.Version}
}

// ListInsurers implementa StrictServerInterface.
func (s *Server) ListInsurers(ctx context.Context, req ListInsurersRequestObject) (ListInsurersResponseObject, error) {
	p, err := s.svc.ListInsurers(ctx, pageQuery(req.Params.Cursor, req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return ListInsurers200JSONResponse{Items: mapItems(p.Items, toInsurer), NextCursor: optional(p.NextCursor)}, nil
}

// CreateInsurer implementa StrictServerInterface.
func (s *Server) CreateInsurer(ctx context.Context, req CreateInsurerRequestObject) (CreateInsurerResponseObject, error) {
	out, err := s.svc.CreateInsurer(ctx, req.Body.Name)
	if err != nil {
		return nil, err
	}
	return CreateInsurer201JSONResponse(toInsurer(out)), nil
}

// UpdateInsurer implementa StrictServerInterface.
func (s *Server) UpdateInsurer(ctx context.Context, req UpdateInsurerRequestObject) (UpdateInsurerResponseObject, error) {
	out, err := s.svc.UpdateInsurer(ctx, update(req.Id, *req.Body))
	if err != nil {
		return nil, err
	}
	return UpdateInsurer200JSONResponse(toInsurer(out)), nil
}

// ListLinesOfBusiness implementa StrictServerInterface.
func (s *Server) ListLinesOfBusiness(ctx context.Context, req ListLinesOfBusinessRequestObject) (ListLinesOfBusinessResponseObject, error) {
	p, err := s.svc.ListLines(ctx, pageQuery(req.Params.Cursor, req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return ListLinesOfBusiness200JSONResponse{Items: mapItems(p.Items, toLine), NextCursor: optional(p.NextCursor)}, nil
}

// CreateLineOfBusiness implementa StrictServerInterface.
func (s *Server) CreateLineOfBusiness(ctx context.Context, req CreateLineOfBusinessRequestObject) (CreateLineOfBusinessResponseObject, error) {
	out, err := s.svc.CreateLine(ctx, req.Body.Name, string(req.Body.RiskSchemaKey))
	if err != nil {
		return nil, err
	}
	return CreateLineOfBusiness201JSONResponse(toLine(out)), nil
}

// UpdateLineOfBusiness implementa StrictServerInterface.
func (s *Server) UpdateLineOfBusiness(ctx context.Context, req UpdateLineOfBusinessRequestObject) (UpdateLineOfBusinessResponseObject, error) {
	out, err := s.svc.UpdateLine(ctx, update(req.Id, *req.Body))
	if err != nil {
		return nil, err
	}
	return UpdateLineOfBusiness200JSONResponse(toLine(out)), nil
}

// ListProducts implementa StrictServerInterface.
func (s *Server) ListProducts(ctx context.Context, req ListProductsRequestObject) (ListProductsResponseObject, error) {
	p, err := s.svc.ListProducts(ctx, pageQuery(req.Params.Cursor, req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return ListProducts200JSONResponse{Items: mapItems(p.Items, toProduct), NextCursor: optional(p.NextCursor)}, nil
}

// CreateProduct implementa StrictServerInterface.
func (s *Server) CreateProduct(ctx context.Context, req CreateProductRequestObject) (CreateProductResponseObject, error) {
	out, err := s.svc.CreateProduct(ctx, app.CreateProductCommand{Name: req.Body.Name, InsurerID: req.Body.InsurerId, LineOfBusinessID: req.Body.LineOfBusinessId})
	if err != nil {
		return nil, err
	}
	return CreateProduct201JSONResponse(toProduct(out)), nil
}

// UpdateProduct implementa StrictServerInterface.
func (s *Server) UpdateProduct(ctx context.Context, req UpdateProductRequestObject) (UpdateProductResponseObject, error) {
	out, err := s.svc.UpdateProduct(ctx, update(req.Id, *req.Body))
	if err != nil {
		return nil, err
	}
	return UpdateProduct200JSONResponse(toProduct(out)), nil
}

// ListRiskSchemas implementa StrictServerInterface.
func (s *Server) ListRiskSchemas(ctx context.Context, _ ListRiskSchemasRequestObject) (ListRiskSchemasResponseObject, error) {
	if err := authz.Require(ctx, authz.CatalogRead); err != nil {
		return nil, err
	}
	items := mapItems(s.schemas.List(), func(r contracts.RiskSchema) RiskSchema {
		return RiskSchema{Key: RiskSchemaKey(r.Key), Version: r.Version, Label: r.Label, Schema: r.Schema}
	})
	return ListRiskSchemas200JSONResponse{Items: items}, nil
}

// ListCommissionAgreements implementa StrictServerInterface.
func (s *Server) ListCommissionAgreements(ctx context.Context, req ListCommissionAgreementsRequestObject) (ListCommissionAgreementsResponseObject, error) {
	p, err := s.svc.ListAgreements(ctx, pageQuery(req.Params.Cursor, req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return ListCommissionAgreements200JSONResponse{Items: mapItems(p.Items, toAgreement), NextCursor: optional(p.NextCursor)}, nil
}

// CreateCommissionAgreement implementa StrictServerInterface.
func (s *Server) CreateCommissionAgreement(ctx context.Context, req CreateCommissionAgreementRequestObject) (CreateCommissionAgreementResponseObject, error) {
	b := req.Body
	cmd := app.CreateAgreementCommand{
		InsurerID: b.InsurerId, LineOfBusinessID: b.LineOfBusinessId, Percentage: b.Percentage, ValidFrom: b.ValidFrom, ValidTo: b.ValidTo,
	}
	if b.ProductId != nil {
		cmd.ProductID = uuid.NullUUID{UUID: *b.ProductId, Valid: true}
	}
	if b.Basis != nil {
		cmd.Basis = domain.Basis(*b.Basis)
	}
	out, err := s.svc.CreateAgreement(ctx, cmd)
	if err != nil {
		return nil, err
	}
	return CreateCommissionAgreement201JSONResponse(toAgreement(out)), nil
}

// UpdateCommissionAgreement implementa StrictServerInterface.
func (s *Server) UpdateCommissionAgreement(ctx context.Context, req UpdateCommissionAgreementRequestObject) (UpdateCommissionAgreementResponseObject, error) {
	b := req.Body
	out, err := s.svc.UpdateAgreement(ctx, app.UpdateAgreementCommand{
		ID: req.Id, Basis: domain.Basis(b.Basis), Percentage: b.Percentage, ValidFrom: b.ValidFrom, ValidTo: b.ValidTo, Version: b.Version,
	})
	if err != nil {
		return nil, err
	}
	return UpdateCommissionAgreement200JSONResponse(toAgreement(out)), nil
}

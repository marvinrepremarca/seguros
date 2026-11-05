// Package billinghttp expone cartera y cobro. Solo mapea HTTP ↔ casos de uso.
package billinghttp

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/billing/app"
	"github.com/marvinrepremarca/seguros/internal/modules/billing/domain"
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
		HandlerWithOptions(h, ChiServerOptions{BaseRouter: g, ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			httpx.WriteProblem(w, r, http.StatusBadRequest, httpx.CodeBadRequest, "La solicitud no es válida")
		}})
	})
	return nil
}

func toPlan(v app.PlanView) Plan {
	out := Plan{
		Id: v.ID, PolicyId: v.PolicyID, PolicyNumber: v.PolicyNumber, InsurerId: v.InsurerID, HolderPartyId: v.HolderPartyID,
		Currency: string(v.Total.Currency()), Total: v.Total.Amount(), Credit: v.Credit.Amount(), Cancelled: v.Cancelled, InArrears: v.InArrears,
		Version: v.Version, Installments: make([]Installment, 0, len(v.Installments)),
	}
	if v.Refund != nil {
		date, amount := v.Refund.EffectiveDate, v.Refund.Amount.Amount()
		out.RefundDate, out.RefundAmount = &date, &amount
	}
	for _, i := range v.Installments {
		out.Installments = append(out.Installments, Installment{
			Id: i.ID, Number: i.Number, DueDate: i.DueDate, Amount: i.Amount.Amount(), Paid: i.Paid.Amount(), Status: InstallmentStatus(i.Status),
		})
	}
	return out
}

func toPayment(p app.Payment) Payment {
	return Payment{
		Id: p.ID, Amount: p.Amount.Amount(), Surplus: p.Surplus.Amount(), Currency: string(p.Amount.Currency()), PaidOn: p.PaidOn,
		Method: PaymentMethod(p.Method), ReportedBy: ReportedBy(p.ReportedBy), SupportReference: p.SupportReference, CreatedAt: p.CreatedAt, ReversedAt: p.ReversedAt,
	}
}

func toActivity(a app.Activity) CollectionActivity {
	return CollectionActivity{Id: a.ID, Channel: CollectionChannel(a.Channel), Note: a.Note, CreatedAt: a.CreatedAt}
}

// GetPlan implementa StrictServerInterface.
func (s *Server) GetPlan(ctx context.Context, req GetPlanRequestObject) (GetPlanResponseObject, error) {
	v, err := s.svc.GetPlan(ctx, req.PolicyId)
	if err != nil {
		return nil, err
	}
	return GetPlan200JSONResponse(toPlan(v)), nil
}

// AdjustSchedule implementa StrictServerInterface.
func (s *Server) AdjustSchedule(ctx context.Context, req AdjustScheduleRequestObject) (AdjustScheduleResponseObject, error) {
	in := make([]app.ScheduleInput, 0, len(req.Body.Installments))
	for _, i := range req.Body.Installments {
		in = append(in, app.ScheduleInput{DueDate: i.DueDate, Amount: i.Amount})
	}
	v, err := s.svc.AdjustSchedule(ctx, req.PolicyId, req.Body.Version, req.Body.SupportReference, in)
	if err != nil {
		return nil, err
	}
	return AdjustSchedule200JSONResponse(toPlan(v)), nil
}

// ListPayments implementa StrictServerInterface.
func (s *Server) ListPayments(ctx context.Context, req ListPaymentsRequestObject) (ListPaymentsResponseObject, error) {
	rows, err := s.svc.ListPayments(ctx, req.PolicyId)
	if err != nil {
		return nil, err
	}
	out := make(ListPayments200JSONResponse, 0, len(rows))
	for _, p := range rows {
		out = append(out, toPayment(p))
	}
	return out, nil
}

// RegisterPayment implementa StrictServerInterface.
func (s *Server) RegisterPayment(ctx context.Context, req RegisterPaymentRequestObject) (RegisterPaymentResponseObject, error) {
	p, err := s.svc.RegisterPayment(ctx, req.PolicyId, app.PaymentInput{
		Key: req.Params.IdempotencyKey, Amount: req.Body.Amount, PaidOn: req.Body.PaidOn, Method: string(req.Body.Method),
		ReportedBy: string(req.Body.ReportedBy), SupportReference: req.Body.SupportReference,
	})
	if err != nil {
		return nil, err
	}
	return RegisterPayment201JSONResponse(toPayment(p)), nil
}

// ReversePayment implementa StrictServerInterface.
func (s *Server) ReversePayment(ctx context.Context, req ReversePaymentRequestObject) (ReversePaymentResponseObject, error) {
	if err := s.svc.ReversePayment(ctx, req.PolicyId, req.PaymentId, req.Body.Reason); err != nil {
		return nil, err
	}
	return ReversePayment204Response{}, nil
}

// ListCollectionActivities implementa StrictServerInterface.
func (s *Server) ListCollectionActivities(ctx context.Context, req ListCollectionActivitiesRequestObject) (ListCollectionActivitiesResponseObject, error) {
	rows, err := s.svc.ListActivities(ctx, req.PolicyId)
	if err != nil {
		return nil, err
	}
	out := make(ListCollectionActivities200JSONResponse, 0, len(rows))
	for _, a := range rows {
		out = append(out, toActivity(a))
	}
	return out, nil
}

// AddCollectionActivity implementa StrictServerInterface.
func (s *Server) AddCollectionActivity(ctx context.Context, req AddCollectionActivityRequestObject) (AddCollectionActivityResponseObject, error) {
	a, err := s.svc.AddActivity(ctx, req.PolicyId, string(req.Body.Channel), req.Body.Note)
	if err != nil {
		return nil, err
	}
	return AddCollectionActivity201JSONResponse(toActivity(a)), nil
}

// ListReceivables implementa StrictServerInterface.
func (s *Server) ListReceivables(ctx context.Context, req ListReceivablesRequestObject) (ListReceivablesResponseObject, error) {
	var bucket, cursor string
	var limit int32
	if req.Params.Bucket != nil {
		bucket = string(*req.Params.Bucket)
	}
	if req.Params.Cursor != nil {
		cursor = *req.Params.Cursor
	}
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	page, err := s.svc.ListReceivables(ctx, bucket, cursor, limit)
	if err != nil {
		return nil, err
	}
	out := ListReceivables200JSONResponse{Items: make([]Receivable, 0, len(page.Items))}
	if page.NextCursor != "" {
		out.NextCursor = &page.NextCursor
	}
	for _, r := range page.Items {
		out.Items = append(out.Items, Receivable{
			InstallmentId: r.InstallmentID, PolicyId: r.PolicyID, PolicyNumber: r.PolicyNumber, InsurerId: r.InsurerID, HolderPartyId: r.HolderPartyID,
			HolderName: r.HolderName, Currency: r.Currency, DueDate: r.DueDate, Balance: r.Balance, DaysOverdue: r.DaysOverdue,
		})
	}
	return out, nil
}

// GetReceivablesSummary implementa StrictServerInterface.
func (s *Server) GetReceivablesSummary(ctx context.Context, _ GetReceivablesSummaryRequestObject) (GetReceivablesSummaryResponseObject, error) {
	rows, err := s.svc.Summary(ctx)
	if err != nil {
		return nil, err
	}
	out := make(GetReceivablesSummary200JSONResponse, 0, len(rows))
	for _, b := range rows {
		out = append(out, ReceivableBucket{Currency: b.Currency, Bucket: AgingBucket(b.Bucket), Balance: b.Balance, Installments: b.Installments})
	}
	return out, nil
}

// ListArrearsSettings implementa StrictServerInterface.
func (s *Server) ListArrearsSettings(ctx context.Context, _ ListArrearsSettingsRequestObject) (ListArrearsSettingsResponseObject, error) {
	rows, err := s.svc.ListSettings(ctx)
	if err != nil {
		return nil, err
	}
	out := ListArrearsSettings200JSONResponse{DefaultGraceDays: domain.DefaultGraceDays, Settings: make([]ArrearsSetting, 0, len(rows))}
	for _, r := range rows {
		item := ArrearsSetting{GraceDays: r.GraceDays}
		if r.InsurerID.Valid {
			id := r.InsurerID.UUID
			item.InsurerId = &id
		}
		out.Settings = append(out.Settings, item)
	}
	return out, nil
}

// SetArrearsSetting implementa StrictServerInterface.
func (s *Server) SetArrearsSetting(ctx context.Context, req SetArrearsSettingRequestObject) (SetArrearsSettingResponseObject, error) {
	in := app.ArrearsSetting{GraceDays: req.Body.GraceDays}
	if req.Body.InsurerId != nil {
		in.InsurerID = uuid.NullUUID{UUID: *req.Body.InsurerId, Valid: true}
	}
	if err := s.svc.SetSetting(ctx, in); err != nil {
		return nil, err
	}
	return SetArrearsSetting204Response{}, nil
}

// GetPaymentSettings implementa StrictServerInterface.
func (s *Server) GetPaymentSettings(ctx context.Context, _ GetPaymentSettingsRequestObject) (GetPaymentSettingsResponseObject, error) {
	days, err := s.svc.LeadDays(ctx)
	if err != nil {
		return nil, err
	}
	return GetPaymentSettings200JSONResponse{LeadDays: days}, nil
}

// SetPaymentSettings implementa StrictServerInterface.
func (s *Server) SetPaymentSettings(ctx context.Context, req SetPaymentSettingsRequestObject) (SetPaymentSettingsResponseObject, error) {
	if err := s.svc.SetLeadDays(ctx, req.Body.LeadDays); err != nil {
		return nil, err
	}
	return SetPaymentSettings204Response{}, nil
}

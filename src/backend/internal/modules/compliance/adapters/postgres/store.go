// Package postgres implementa la persistencia de cumplimiento con pgx + sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	compliancesqlc "github.com/marvinrepremarca/seguros/internal/modules/compliance/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/modules/compliance/app"
	"github.com/marvinrepremarca/seguros/internal/modules/compliance/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Store implementa app.Store.
type Store struct {
	db       *db.DB
	recorder *audit.Recorder
	outbox   *outbox.Writer
}

// NewStore construye el Store.
func NewStore(database *db.DB, recorder *audit.Recorder, writer *outbox.Writer) *Store {
	return &Store{db: database, recorder: recorder, outbox: writer}
}

// WithinTenant implementa app.Store.
func (s *Store) WithinTenant(ctx context.Context, fn func(ctx context.Context, uow app.UnitOfWork) error) error {
	return s.db.WithTenantTx(ctx, func(tx pgx.Tx) error {
		tenant, ok := reqctx.TenantID(ctx)
		if !ok {
			return db.ErrNoTenant
		}
		return fn(ctx, &unitOfWork{tx: tx, store: s, repo: &repository{q: compliancesqlc.New(tx), tenant: uuid.UUID(tenant)}})
	})
}

type unitOfWork struct {
	tx    pgx.Tx
	store *Store
	repo  *repository
}

func (u *unitOfWork) Records() app.Repository { return u.repo }

func (u *unitOfWork) Audit(ctx context.Context, e audit.Entry) error {
	return u.store.recorder.Record(ctx, u.tx, e)
}

func (u *unitOfWork) Publish(ctx context.Context, topic string, payload any) error {
	_, err := u.store.outbox.Add(ctx, u.tx, topic, payload)
	return err
}

type repository struct {
	q      *compliancesqlc.Queries
	tenant uuid.UUID
}

func actor(ctx context.Context) uuid.NullUUID {
	if u, ok := reqctx.UserID(ctx); ok {
		return uuid.NullUUID{UUID: uuid.UUID(u), Valid: true}
	}
	return uuid.NullUUID{}
}

func wrap(err error, op string) error {
	if err != nil {
		return fmt.Errorf("compliance: %s: %w", op, err)
	}
	return nil
}

func (r *repository) InsertCheck(ctx context.Context, c domain.KycCheck, sealed app.SealedCheck, at time.Time) error {
	return wrap(r.q.InsertKycCheck(ctx, compliancesqlc.InsertKycCheckParams{
		ID: c.ID, TenantID: r.tenant, PartyID: c.PartyID, DocumentBidx: c.Fingerprint, ResultCiphertext: sealed.Result, SourceCiphertext: sealed.Source, CheckedOn: c.CheckedOn, DetailsCiphertext: sealed.Details, At: at, Actor: actor(ctx),
	}), "registrando verificación")
}

func stored(id, party uuid.UUID, bidx []byte, result, source pgtype.Text, resultCT, sourceCT []byte, on kernel.Date, details bool) app.StoredCheck {
	return app.StoredCheck{
		Check:        domain.KycCheck{ID: id, PartyID: party, Fingerprint: bidx, Result: domain.Result(result.String), Source: source.String, CheckedOn: on, HasDetails: details},
		ResultSealed: resultCT, SourceSealed: sourceCT,
	}
}

func (r *repository) LatestCheck(ctx context.Context, party uuid.UUID) (app.StoredCheck, bool, error) {
	x, err := r.q.LatestKycCheck(ctx, compliancesqlc.LatestKycCheckParams{TenantID: r.tenant, PartyID: party})
	if errors.Is(err, pgx.ErrNoRows) {
		return app.StoredCheck{}, false, nil
	}
	if err != nil {
		return app.StoredCheck{}, false, wrap(err, "leyendo verificación")
	}
	return stored(x.ID, x.PartyID, x.DocumentBidx, x.Result, x.Source, x.ResultCiphertext, x.SourceCiphertext, x.CheckedOn, x.HasDetails), true, nil
}

func (r *repository) Checks(ctx context.Context, party uuid.UUID) ([]app.StoredCheck, error) {
	rows, err := r.q.ListKycChecks(ctx, compliancesqlc.ListKycChecksParams{TenantID: r.tenant, PartyID: party})
	if err != nil {
		return nil, wrap(err, "listando verificaciones")
	}
	out := make([]app.StoredCheck, 0, len(rows))
	for _, x := range rows {
		out = append(out, stored(x.ID, x.PartyID, x.DocumentBidx, x.Result, x.Source, x.ResultCiphertext, x.SourceCiphertext, x.CheckedOn, x.HasDetails))
	}
	return out, nil
}

func (r *repository) CheckDetails(ctx context.Context, party, check uuid.UUID) ([]byte, error) {
	out, err := r.q.GetKycDetails(ctx, compliancesqlc.GetKycDetailsParams{TenantID: r.tenant, PartyID: party, ID: check})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && len(out) == 0) {
		return nil, domain.ErrNotFound
	}
	return out, wrap(err, "leyendo detalle")
}

func (r *repository) InsertComplaint(ctx context.Context, s domain.ComplaintData, at time.Time) error {
	return wrap(r.q.InsertComplaint(ctx, compliancesqlc.InsertComplaintParams{
		ID: s.ID, TenantID: r.tenant, PartyID: s.PartyID, Channel: s.Channel, Subject: s.Subject, Description: s.Description, ReceivedOn: s.ReceivedOn, At: at, Actor: actor(ctx),
	}), "registrando queja")
}

func (r *repository) GetComplaint(ctx context.Context, id uuid.UUID) (*domain.Complaint, error) {
	x, err := r.q.GetComplaint(ctx, compliancesqlc.GetComplaintParams{TenantID: r.tenant, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrap(err, "leyendo queja")
	}
	return domain.RehydrateComplaint(domain.ComplaintData{ID: x.ID, PartyID: x.PartyID, Channel: x.Channel, Subject: x.Subject, Description: x.Description, Status: domain.ComplaintStatus(x.Status), Resolution: x.Resolution.String, ReceivedOn: x.ReceivedOn, Version: x.Version}), nil
}

func (r *repository) SaveComplaint(ctx context.Context, c *domain.Complaint, at time.Time) error {
	s := c.Data()
	n, err := r.q.UpdateComplaint(ctx, compliancesqlc.UpdateComplaintParams{
		TenantID: r.tenant, ID: s.ID, Status: string(s.Status), Resolution: pgtype.Text{String: s.Resolution, Valid: s.Resolution != ""}, At: at, Actor: actor(ctx), Version: s.Version,
	})
	if err != nil {
		return wrap(err, "guardando queja")
	}
	if n == 0 {
		return domain.ErrConcurrentUpdate
	}
	return nil
}

func (r *repository) Complaints(ctx context.Context, before uuid.NullUUID, limit int32) ([]domain.ComplaintData, error) {
	rows, err := r.q.ListComplaints(ctx, compliancesqlc.ListComplaintsParams{TenantID: r.tenant, Before: before, PageSize: limit})
	if err != nil {
		return nil, wrap(err, "listando quejas")
	}
	out := make([]domain.ComplaintData, 0, len(rows))
	for _, x := range rows {
		out = append(out, domain.ComplaintData{ID: x.ID, PartyID: x.PartyID, Channel: x.Channel, Subject: x.Subject, Description: x.Description, Status: domain.ComplaintStatus(x.Status), Resolution: x.Resolution.String, ReceivedOn: x.ReceivedOn, Version: x.Version})
	}
	return out, nil
}

func (r *repository) InsertSuppression(ctx context.Context, id, party uuid.UUID, at time.Time) error {
	return wrap(r.q.InsertSuppression(ctx, compliancesqlc.InsertSuppressionParams{ID: id, TenantID: r.tenant, PartyID: party, At: at, Actor: actor(ctx)}), "registrando solicitud")
}

func (r *repository) GetSuppression(ctx context.Context, id uuid.UUID) (domain.SuppressionStatus, int32, error) {
	x, err := r.q.GetSuppression(ctx, compliancesqlc.GetSuppressionParams{TenantID: r.tenant, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, domain.ErrNotFound
	}
	if err != nil {
		return "", 0, wrap(err, "leyendo solicitud")
	}
	return domain.SuppressionStatus(x.Status), x.Version, nil
}

func (r *repository) SaveSuppression(ctx context.Context, id uuid.UUID, status domain.SuppressionStatus, resolution string, version int32, at time.Time) error {
	n, err := r.q.ResolveSuppression(ctx, compliancesqlc.ResolveSuppressionParams{TenantID: r.tenant, ID: id, Status: string(status), Resolution: pgtype.Text{String: resolution, Valid: true}, At: at, Actor: actor(ctx), Version: version})
	if err != nil {
		return wrap(err, "resolviendo solicitud")
	}
	if n == 0 {
		return domain.ErrConcurrentUpdate
	}
	return nil
}

func (r *repository) Suppressions(ctx context.Context, before uuid.NullUUID, limit int32) ([]app.Suppression, error) {
	rows, err := r.q.ListSuppressions(ctx, compliancesqlc.ListSuppressionsParams{TenantID: r.tenant, Before: before, PageSize: limit})
	if err != nil {
		return nil, wrap(err, "listando solicitudes")
	}
	out := make([]app.Suppression, 0, len(rows))
	for _, x := range rows {
		out = append(out, app.Suppression{ID: x.ID, PartyID: x.PartyID, Status: x.Status, Resolution: x.Resolution.String, CreatedAt: x.CreatedAt})
	}
	return out, nil
}

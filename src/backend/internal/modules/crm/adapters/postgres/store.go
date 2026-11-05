// Package postgres implementa la persistencia de crm con pgx + sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	crmsqlc "github.com/marvinrepremarca/seguros/internal/modules/crm/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/modules/crm/app"
	"github.com/marvinrepremarca/seguros/internal/modules/crm/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Store implementa app.Store.
type Store struct {
	db       *db.DB
	recorder *audit.Recorder
}

// NewStore construye el Store.
func NewStore(database *db.DB, recorder *audit.Recorder) *Store {
	return &Store{db: database, recorder: recorder}
}

// WithinTenant implementa app.Store.
func (s *Store) WithinTenant(ctx context.Context, fn func(ctx context.Context, uow app.UnitOfWork) error) error {
	return s.db.WithTenantTx(ctx, func(tx pgx.Tx) error {
		uow, err := s.Bind(ctx, tx)
		if err != nil {
			return err
		}
		return fn(ctx, uow)
	})
}

// Bind da la unidad de trabajo sobre la transacción de un consumidor.
func (s *Store) Bind(ctx context.Context, tx pgx.Tx) (app.UnitOfWork, error) {
	tenant, ok := reqctx.TenantID(ctx)
	if !ok {
		return nil, db.ErrNoTenant
	}
	return &unitOfWork{tx: tx, recorder: s.recorder, repo: &repository{q: crmsqlc.New(tx), tenant: uuid.UUID(tenant)}}, nil
}

type unitOfWork struct {
	tx       pgx.Tx
	recorder *audit.Recorder
	repo     *repository
}

func (u *unitOfWork) Leads() app.Repository { return u.repo }

func (u *unitOfWork) Audit(ctx context.Context, e audit.Entry) error {
	return u.recorder.Record(ctx, u.tx, e)
}

type repository struct {
	q      *crmsqlc.Queries
	tenant uuid.UUID
}

func actor(ctx context.Context) uuid.NullUUID {
	if u, ok := reqctx.UserID(ctx); ok {
		return uuid.NullUUID{UUID: uuid.UUID(u), Valid: true}
	}
	return uuid.NullUUID{}
}

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

func (r *repository) Insert(ctx context.Context, s domain.Snapshot, at time.Time) error {
	err := r.q.InsertLead(ctx, crmsqlc.InsertLeadParams{
		ID: s.ID, TenantID: r.tenant, Name: s.Name, Email: text(s.Email), Phone: text(s.Phone), Source: s.Source,
		LineOfBusinessID: s.LineOfBusinessID, AgentID: s.AgentID, AssignedAt: s.AssignedAt, At: at, Actor: actor(ctx),
	})
	if err != nil {
		return fmt.Errorf("crm: creando prospecto: %w", err)
	}
	return nil
}

func (r *repository) Get(ctx context.Context, id uuid.UUID) (*domain.Lead, error) {
	x, err := r.q.GetLead(ctx, crmsqlc.GetLeadParams{TenantID: r.tenant, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("crm: leyendo prospecto: %w", err)
	}
	return domain.Rehydrate(domain.Snapshot{
		ID: x.ID, Name: x.Name, Email: x.Email.String, Phone: x.Phone.String, Source: x.Source, LineOfBusinessID: x.LineOfBusinessID, AgentID: x.AgentID, PartyID: x.PartyID,
		Status: domain.Status(x.Status), LostReason: x.LostReason.String, FollowUpOn: x.FollowUpOn, ContactedAt: x.ContactedAt, AssignedAt: x.AssignedAt, Version: x.Version,
	}), nil
}

func (r *repository) Save(ctx context.Context, l *domain.Lead, at time.Time) error {
	s := l.Snapshot()
	n, err := r.q.UpdateLead(ctx, crmsqlc.UpdateLeadParams{
		TenantID: r.tenant, ID: s.ID, AgentID: s.AgentID, PartyID: s.PartyID, Status: string(s.Status), LostReason: text(s.LostReason), FollowUpOn: s.FollowUpOn,
		ContactedAt: s.ContactedAt, AssignedAt: s.AssignedAt, At: at, Actor: actor(ctx), Version: s.Version,
	})
	if err != nil {
		return fmt.Errorf("crm: guardando prospecto: %w", err)
	}
	if n == 0 {
		return domain.ErrConcurrentUpdate
	}
	return nil
}

func (r *repository) List(ctx context.Context, status string, agent, before uuid.NullUUID, limit int32) ([]domain.Snapshot, error) {
	rows, err := r.q.ListLeads(ctx, crmsqlc.ListLeadsParams{TenantID: r.tenant, Status: text(status), Agent: agent, Before: before, PageSize: limit})
	if err != nil {
		return nil, fmt.Errorf("crm: listando prospectos: %w", err)
	}
	out := make([]domain.Snapshot, 0, len(rows))
	for _, x := range rows {
		out = append(out, domain.Snapshot{
			ID: x.ID, Name: x.Name, Email: x.Email.String, Phone: x.Phone.String, Source: x.Source, LineOfBusinessID: x.LineOfBusinessID, AgentID: x.AgentID, PartyID: x.PartyID,
			Status: domain.Status(x.Status), LostReason: x.LostReason.String, FollowUpOn: x.FollowUpOn, ContactedAt: x.ContactedAt, AssignedAt: x.AssignedAt, Version: x.Version,
		})
	}
	return out, nil
}

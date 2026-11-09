// Package postgres implementa la persistencia de siniestros con pgx + sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	claimssqlc "github.com/marvinrepremarca/seguros/internal/modules/claims/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/modules/claims/app"
	"github.com/marvinrepremarca/seguros/internal/modules/claims/domain"
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
		return fn(ctx, &unitOfWork{tx: tx, store: s, repo: &repository{q: claimssqlc.New(tx), tenant: uuid.UUID(tenant)}})
	})
}

type unitOfWork struct {
	tx    pgx.Tx
	store *Store
	repo  *repository
}

func (u *unitOfWork) Claims() app.Repository { return u.repo }

func (u *unitOfWork) Audit(ctx context.Context, e audit.Entry) error {
	return u.store.recorder.Record(ctx, u.tx, e)
}

func (u *unitOfWork) Publish(ctx context.Context, topic string, payload any) error {
	_, err := u.store.outbox.Add(ctx, u.tx, topic, payload)
	return err
}

type repository struct {
	q      *claimssqlc.Queries
	tenant uuid.UUID
}

func actor(ctx context.Context) uuid.NullUUID {
	if u, ok := reqctx.UserID(ctx); ok {
		return uuid.NullUUID{UUID: uuid.UUID(u), Valid: true}
	}
	return uuid.NullUUID{}
}

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

func wrap(err error, op string) error {
	if err != nil {
		return fmt.Errorf("claims: %s: %w", op, err)
	}
	return nil
}

func nullMoney(m *kernel.Money) decimal.NullDecimal {
	if m == nil {
		return decimal.NullDecimal{}
	}
	return decimal.NullDecimal{Decimal: m.Amount(), Valid: true}
}

func moneyOf(d decimal.NullDecimal, c kernel.Currency) (*kernel.Money, error) {
	if !d.Valid {
		return nil, nil //nolint:nilnil // sin valor registrado
	}
	m, err := kernel.NewMoney(d.Decimal, c)
	if err != nil {
		return nil, wrap(err, "valor guardado")
	}
	return &m, nil
}

func (r *repository) Insert(ctx context.Context, s domain.Snapshot, at time.Time) error {
	return wrap(r.q.InsertClaim(ctx, claimssqlc.InsertClaimParams{
		ID: s.ID, TenantID: r.tenant, PolicyID: s.PolicyID, PolicyNumber: text(s.PolicyNumber), InsurerID: s.InsurerID, HolderPartyID: s.HolderPartyID, OccurredOn: s.OccurredOn,
		Location: s.Location, ClaimType: s.ClaimType, Description: s.Description, Currency: string(s.Currency), ClaimedAmount: nullMoney(s.ClaimedAmount),
		AlertOutOfCoverage: s.AlertOutOfCoverage, AlertInArrears: s.AlertInArrears, AlertNotInForce: s.AlertNotInForce, At: at, Actor: actor(ctx),
	}), "creando siniestro")
}

func snapshot(x claimssqlc.GetClaimRow) (domain.Snapshot, error) {
	c := kernel.Currency(x.Currency)
	claimed, err := moneyOf(x.ClaimedAmount, c)
	if err != nil {
		return domain.Snapshot{}, err
	}
	paid, err := moneyOf(x.PaidAmount, c)
	if err != nil {
		return domain.Snapshot{}, err
	}
	return domain.Snapshot{
		ID: x.ID, PolicyID: x.PolicyID, PolicyNumber: x.PolicyNumber.String, InsurerID: x.InsurerID, HolderPartyID: x.HolderPartyID, OccurredOn: x.OccurredOn, Location: x.Location,
		ClaimType: x.ClaimType, Description: x.Description, Currency: c, ClaimedAmount: claimed, PaidAmount: paid, Status: domain.Status(x.Status), InsurerClaimNumber: x.InsurerClaimNumber.String,
		DecisionSupport: x.DecisionSupport.String, AlertOutOfCoverage: x.AlertOutOfCoverage, AlertInArrears: x.AlertInArrears, AlertNotInForce: x.AlertNotInForce, HasHealthDetails: x.HasHealth,
		ReportedAt: x.ReportedAt, NotifiedAt: x.NotifiedAt, Version: x.Version,
	}, nil
}

func (r *repository) Get(ctx context.Context, id uuid.UUID) (*domain.Claim, error) {
	x, err := r.q.GetClaim(ctx, claimssqlc.GetClaimParams{TenantID: r.tenant, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrap(err, "leyendo siniestro")
	}
	s, err := snapshot(x)
	if err != nil {
		return nil, err
	}
	return domain.Rehydrate(s), nil
}

func (r *repository) Save(ctx context.Context, c *domain.Claim, at time.Time) error {
	s := c.Snapshot()
	n, err := r.q.UpdateClaim(ctx, claimssqlc.UpdateClaimParams{
		TenantID: r.tenant, ID: s.ID, Status: string(s.Status), ClaimedAmount: nullMoney(s.ClaimedAmount), PaidAmount: nullMoney(s.PaidAmount), InsurerClaimNumber: text(s.InsurerClaimNumber),
		DecisionSupport: text(s.DecisionSupport), NotifiedAt: s.NotifiedAt, At: at, Actor: actor(ctx), Version: s.Version,
	})
	if err != nil {
		return wrap(err, "guardando siniestro")
	}
	if n == 0 {
		return domain.ErrConcurrentUpdate
	}
	return nil
}

func (r *repository) List(ctx context.Context, f app.ListFilter) ([]domain.Snapshot, error) {
	rows, err := r.q.ListClaims(ctx, claimssqlc.ListClaimsParams{TenantID: r.tenant, Status: text(f.Status), Scoped: f.Scoped, Policies: f.Policies, Before: f.Before, PageSize: f.Limit})
	if err != nil {
		return nil, wrap(err, "listando siniestros")
	}
	out := make([]domain.Snapshot, 0, len(rows))
	for _, x := range rows {
		s, err := snapshot(claimssqlc.GetClaimRow(x))
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (r *repository) SetHealth(ctx context.Context, id uuid.UUID, ciphertext []byte, at time.Time) error {
	n, err := r.q.SetHealth(ctx, claimssqlc.SetHealthParams{TenantID: r.tenant, ID: id, Ciphertext: ciphertext, At: at, Actor: actor(ctx)})
	if err != nil {
		return wrap(err, "guardando detalle")
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *repository) Health(ctx context.Context, id uuid.UUID) ([]byte, error) {
	out, err := r.q.GetHealth(ctx, claimssqlc.GetHealthParams{TenantID: r.tenant, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrap(err, "leyendo detalle")
	}
	if len(out) == 0 {
		return nil, domain.ErrNoHealthDetails
	}
	return out, nil
}

func (r *repository) AddChecklistItem(ctx context.Context, claim uuid.UUID, label string, at time.Time) error {
	id, err := uuid.NewV7()
	if err != nil {
		return wrap(err, "creando elemento")
	}
	return wrap(r.q.InsertChecklistItem(ctx, claimssqlc.InsertChecklistItemParams{ID: id, TenantID: r.tenant, ClaimID: claim, Label: label, At: at, Actor: actor(ctx)}), "creando elemento")
}

func (r *repository) SetChecklistItem(ctx context.Context, claim, item uuid.UUID, received bool, at time.Time) error {
	n, err := r.q.SetChecklistReceived(ctx, claimssqlc.SetChecklistReceivedParams{TenantID: r.tenant, ClaimID: claim, ID: item, Received: received, At: at, Actor: actor(ctx)})
	if err != nil {
		return wrap(err, "guardando elemento")
	}
	if n == 0 {
		return domain.ErrItemNotFound
	}
	return nil
}

func (r *repository) Checklist(ctx context.Context, claim uuid.UUID) ([]app.ChecklistItem, error) {
	rows, err := r.q.ListChecklist(ctx, claimssqlc.ListChecklistParams{TenantID: r.tenant, ClaimID: claim})
	if err != nil {
		return nil, wrap(err, "leyendo lista")
	}
	out := make([]app.ChecklistItem, 0, len(rows))
	for _, x := range rows {
		out = append(out, app.ChecklistItem{ID: x.ID, Label: x.Label, Received: x.Received})
	}
	return out, nil
}

func (r *repository) AddNote(ctx context.Context, claim uuid.UUID, body string, at time.Time) error {
	id, err := uuid.NewV7()
	if err != nil {
		return wrap(err, "creando gestión")
	}
	return wrap(r.q.InsertNote(ctx, claimssqlc.InsertNoteParams{ID: id, TenantID: r.tenant, ClaimID: claim, Body: body, At: at, Actor: actor(ctx)}), "creando gestión")
}

func (r *repository) Notes(ctx context.Context, claim uuid.UUID) ([]app.Note, error) {
	rows, err := r.q.ListNotes(ctx, claimssqlc.ListNotesParams{TenantID: r.tenant, ClaimID: claim})
	if err != nil {
		return nil, wrap(err, "leyendo gestiones")
	}
	out := make([]app.Note, 0, len(rows))
	for _, x := range rows {
		out = append(out, app.Note{ID: x.ID, Body: x.Body, CreatedAt: x.CreatedAt})
	}
	return out, nil
}

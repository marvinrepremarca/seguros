// Package postgres implementa la persistencia de parties con pgx + sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	partiessqlc "github.com/marvinrepremarca/seguros/internal/modules/parties/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/modules/parties/app"
	"github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/parties/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

const uniqueViolation = "23505"

// Store implementa app.Store.
type Store struct {
	db       *db.DB
	recorder *audit.Recorder
}

var _ app.Store = (*Store)(nil)

// NewStore construye el Store.
func NewStore(database *db.DB, recorder *audit.Recorder) *Store {
	return &Store{db: database, recorder: recorder}
}

// WithinTenant implementa app.Store.
func (s *Store) WithinTenant(ctx context.Context, fn func(ctx context.Context, uow app.UnitOfWork) error) error {
	tenant, ok := reqctx.TenantID(ctx)
	if !ok {
		return db.ErrNoTenant
	}
	return s.db.WithTenantTx(ctx, func(tx pgx.Tx) error {
		return fn(ctx, &unitOfWork{tx: tx, recorder: s.recorder, repo: &repository{q: partiessqlc.New(tx), tenant: uuid.UUID(tenant)}})
	})
}

type unitOfWork struct {
	tx       pgx.Tx
	recorder *audit.Recorder
	repo     *repository
}

func (u *unitOfWork) Parties() app.PartyRepository { return u.repo }

func (u *unitOfWork) Audit(ctx context.Context, e audit.Entry) error {
	return u.recorder.Record(ctx, u.tx, e)
}

type repository struct {
	q      *partiessqlc.Queries
	tenant uuid.UUID
}

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

func actor(ctx context.Context) uuid.NullUUID {
	if u, ok := reqctx.UserID(ctx); ok {
		return uuid.NullUUID{UUID: uuid.UUID(u), Valid: true}
	}
	return uuid.NullUUID{}
}

func mapWriteError(err error, op string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "parties_document_key" {
		return domain.ErrDocumentTaken
	}
	return fmt.Errorf("parties: %s: %w", op, err)
}

func (r *repository) Insert(ctx context.Context, p *domain.Party, doc app.SealedDocument) error {
	s := p.Snapshot()
	err := r.q.InsertParty(ctx, partiessqlc.InsertPartyParams{
		ID: uuid.UUID(s.ID), TenantID: r.tenant, Kind: string(s.Kind), Name: s.Name, DocumentType: string(s.DocumentType),
		DocumentCiphertext: doc.Ciphertext, DocumentBidx: doc.BlindIndex, DocumentLast4: s.DocumentLast4,
		Email: text(s.Contact.Email), Phone: text(s.Contact.Phone), Address: text(s.Contact.Address), City: text(s.Contact.City),
		ResponsibleAgentID: s.Responsible, CreatedAt: s.CreatedAt, Actor: actor(ctx),
	})
	if err != nil {
		return mapWriteError(err, "insertando tercero")
	}
	return nil
}

func rehydrate(id uuid.UUID, kind, name, docType, last4 string, email, phone, address, city pgtype.Text, row func(*domain.PartySnapshot)) *domain.Party {
	s := domain.PartySnapshot{
		ID: domain.PartyID(id), Kind: domain.Kind(kind), Name: name, DocumentType: domain.DocumentType(docType), DocumentLast4: last4,
		Contact: domain.Contact{Email: email.String, Phone: phone.String, Address: address.String, City: city.String},
	}
	row(&s)
	return domain.RehydrateParty(s)
}

func fromGet(r partiessqlc.GetPartyRow) *domain.Party {
	return rehydrate(r.ID, r.Kind, r.Name, r.DocumentType, r.DocumentLast4, r.Email, r.Phone, r.Address, r.City, func(s *domain.PartySnapshot) {
		s.Responsible, s.CreatedAt, s.UpdatedAt, s.Version = r.ResponsibleAgentID, r.CreatedAt, r.UpdatedAt, r.Version
	})
}

func (r *repository) Get(ctx context.Context, id domain.PartyID) (*domain.Party, error) {
	row, err := r.q.GetParty(ctx, partiessqlc.GetPartyParams{TenantID: r.tenant, ID: uuid.UUID(id)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("parties: leyendo tercero: %w", err)
	}
	return fromGet(row), nil
}

func (r *repository) GetByBlindIndex(ctx context.Context, docType domain.DocumentType, blindIndex []byte) (*domain.Party, error) {
	row, err := r.q.GetPartyByDocument(ctx, partiessqlc.GetPartyByDocumentParams{TenantID: r.tenant, DocumentType: string(docType), DocumentBidx: blindIndex})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("parties: buscando por documento: %w", err)
	}
	return fromGet(partiessqlc.GetPartyRow(row)), nil
}

func (r *repository) GetDocument(ctx context.Context, id domain.PartyID) (domain.DocumentType, []byte, error) {
	row, err := r.q.GetPartyDocument(ctx, partiessqlc.GetPartyDocumentParams{TenantID: r.tenant, ID: uuid.UUID(id)})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, domain.ErrNotFound
	}
	if err != nil {
		return "", nil, fmt.Errorf("parties: leyendo documento: %w", err)
	}
	return domain.DocumentType(row.DocumentType), row.DocumentCiphertext, nil
}

func (r *repository) IsVisibleTo(ctx context.Context, id domain.PartyID, agent uuid.UUID) (bool, error) {
	visible, err := r.q.IsPartyVisibleTo(ctx, partiessqlc.IsPartyVisibleToParams{TenantID: r.tenant, ID: uuid.UUID(id), Agent: agent})
	if err != nil {
		return false, fmt.Errorf("parties: verificando alcance: %w", err)
	}
	return visible, nil
}

func (r *repository) List(ctx context.Context, f app.ListFilter) ([]*domain.Party, error) {
	rows, err := r.q.ListParties(ctx, partiessqlc.ListPartiesParams{
		TenantID: r.tenant, Before: f.Before, NamePattern: text(f.NamePattern), VisibleTo: f.VisibleTo, PageSize: f.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("parties: listando terceros: %w", err)
	}
	out := make([]*domain.Party, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromGet(partiessqlc.GetPartyRow(row)))
	}
	return out, nil
}

func (r *repository) Update(ctx context.Context, p *domain.Party, doc *app.SealedDocument) error {
	s := p.Snapshot()
	params := partiessqlc.UpdatePartyParams{
		Name: s.Name, Email: text(s.Contact.Email), Phone: text(s.Contact.Phone), Address: text(s.Contact.Address), City: text(s.Contact.City),
		DocumentType: string(s.DocumentType), DocumentLast4: s.DocumentLast4, ResponsibleAgentID: s.Responsible, UpdatedAt: s.UpdatedAt, Actor: actor(ctx),
		TenantID: r.tenant, ID: uuid.UUID(s.ID), Version: s.Version,
	}
	if doc != nil {
		params.DocumentCiphertext, params.DocumentBidx = doc.Ciphertext, doc.BlindIndex
	}
	n, err := r.q.UpdateParty(ctx, params)
	if err != nil {
		return mapWriteError(err, "actualizando tercero")
	}
	if n == 0 {
		return domain.ErrConcurrentUpdate
	}
	return nil
}

func (r *repository) RecordResponsibleChange(ctx context.Context, id domain.PartyID, version int32, previous uuid.NullUUID, agent uuid.UUID, reason kernel.TransferReason, at time.Time) error {
	if err := r.q.InsertResponsibleHistory(ctx, partiessqlc.InsertResponsibleHistoryParams{
		TenantID: r.tenant, PartyID: uuid.UUID(id), PartyVersion: version, PreviousAgentID: previous,
		NewAgentID: agent, Reason: reason.Code, Note: text(reason.Note), Actor: actor(ctx), ChangedAt: at,
	}); err != nil {
		return fmt.Errorf("parties: guardando historial de responsable: %w", err)
	}
	return nil
}

func (r *repository) InsertConsent(ctx context.Context, c domain.Consent) error {
	if err := r.q.InsertConsent(ctx, partiessqlc.InsertConsentParams{
		ID: c.ID, TenantID: r.tenant, PartyID: uuid.UUID(c.PartyID), Status: string(c.Status), Purpose: c.Purpose,
		TextVersion: c.TextVersion, ConsentDate: c.ConsentDate, RecordedAt: c.RecordedAt, RecordedBy: actor(ctx),
	}); err != nil {
		return fmt.Errorf("parties: guardando autorización: %w", err)
	}
	return nil
}

func (r *repository) ListConsents(ctx context.Context, id domain.PartyID) ([]domain.Consent, error) {
	rows, err := r.q.ListConsents(ctx, partiessqlc.ListConsentsParams{TenantID: r.tenant, PartyID: uuid.UUID(id)})
	if err != nil {
		return nil, fmt.Errorf("parties: listando autorizaciones: %w", err)
	}
	out := make([]domain.Consent, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Consent{
			ID: row.ID, PartyID: id, Status: domain.ConsentStatus(row.Status), Purpose: row.Purpose,
			TextVersion: row.TextVersion, ConsentDate: row.ConsentDate, RecordedAt: row.RecordedAt,
		})
	}
	return out, nil
}

func (r *repository) Summaries(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]contracts.PartySummary, error) {
	rows, err := r.q.ListPartySummaries(ctx, partiessqlc.ListPartySummariesParams{TenantID: r.tenant, Ids: ids})
	if err != nil {
		return nil, fmt.Errorf("parties: consultando terceros: %w", err)
	}
	out := make(map[uuid.UUID]contracts.PartySummary, len(rows))
	for _, row := range rows {
		out[row.ID] = contracts.PartySummary{ID: row.ID, Kind: row.Kind, Name: row.Name}
	}
	return out, nil
}

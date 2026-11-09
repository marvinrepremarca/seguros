// Package postgres implementa la persistencia de documentos con pgx + sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	documentssqlc "github.com/marvinrepremarca/seguros/internal/modules/documents/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/modules/documents/app"
	"github.com/marvinrepremarca/seguros/internal/modules/documents/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
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
		tenant, ok := reqctx.TenantID(ctx)
		if !ok {
			return db.ErrNoTenant
		}
		return fn(ctx, &unitOfWork{tx: tx, store: s, repo: &repository{q: documentssqlc.New(tx), tenant: uuid.UUID(tenant)}})
	})
}

// TenantsWithPendingScans lista los tenants con archivos por revisar.
func (s *Store) TenantsWithPendingScans(ctx context.Context) ([]kernel.TenantID, error) {
	var out []kernel.TenantID
	err := s.db.WithoutTenantTx(ctx, func(tx pgx.Tx) error {
		ids, err := documentssqlc.New(tx).TenantsWithPendingScans(ctx)
		if err != nil {
			return fmt.Errorf("documents: tenants con archivos: %w", err)
		}
		for _, id := range ids {
			out = append(out, kernel.TenantID(id))
		}
		return nil
	})
	return out, err
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

type repository struct {
	q      *documentssqlc.Queries
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
		return fmt.Errorf("documents: %s: %w", op, err)
	}
	return nil
}

func toDocument(x documentssqlc.DocumentsDocument) domain.Document {
	return domain.Document{ID: x.ID, OwnerType: domain.OwnerType(x.OwnerType), OwnerID: x.OwnerID, Title: x.Title, Sensitive: x.Sensitive, CurrentNumber: x.CurrentNumber, CreatedOn: x.CreatedOn, Version: x.Version}
}

func toDocuments(rows []documentssqlc.DocumentsDocument) []domain.Document {
	out := make([]domain.Document, 0, len(rows))
	for _, x := range rows {
		out = append(out, toDocument(x))
	}
	return out
}

func toVersions(rows []documentssqlc.DocumentsVersion) []domain.Version {
	out := make([]domain.Version, 0, len(rows))
	for _, x := range rows {
		out = append(out, domain.Version{ID: x.ID, DocumentID: x.DocumentID, Number: x.Number, StorageKey: x.StorageKey, MimeType: x.MimeType, SizeBytes: x.SizeBytes, Sha256: x.Sha256, Encrypted: x.Encrypted, Scan: domain.ScanStatus(x.ScanStatus), CreatedAt: x.CreatedAt})
	}
	return out
}

func (r *repository) Insert(ctx context.Context, d domain.Document, key string, at time.Time) error {
	return wrap(r.q.InsertDocument(ctx, documentssqlc.InsertDocumentParams{
		ID: d.ID, TenantID: r.tenant, OwnerType: string(d.OwnerType), OwnerID: d.OwnerID, Title: d.Title, Sensitive: d.Sensitive,
		IdempotencyKey: pgtype.Text{String: key, Valid: key != ""}, CreatedOn: d.CreatedOn, At: at, Actor: actor(ctx),
	}), "registrando documento")
}

func (r *repository) InsertVersion(ctx context.Context, v domain.Version, at time.Time) error {
	return wrap(r.q.InsertVersion(ctx, documentssqlc.InsertVersionParams{
		ID: v.ID, TenantID: r.tenant, DocumentID: v.DocumentID, Number: v.Number, StorageKey: v.StorageKey, MimeType: v.MimeType, SizeBytes: v.SizeBytes, Sha256: v.Sha256, Encrypted: v.Encrypted, At: at, Actor: actor(ctx),
	}), "registrando versión")
}

func (r *repository) Bump(ctx context.Context, id uuid.UUID, number, version int32, at time.Time) error {
	n, err := r.q.BumpDocument(ctx, documentssqlc.BumpDocumentParams{Number: number, At: at, Actor: actor(ctx), TenantID: r.tenant, ID: id, Version: version})
	if err != nil {
		return wrap(err, "actualizando documento")
	}
	if n == 0 {
		return domain.ErrConcurrentUpdate
	}
	return nil
}

func (r *repository) Get(ctx context.Context, id uuid.UUID) (domain.Document, error) {
	x, err := r.q.GetDocument(ctx, documentssqlc.GetDocumentParams{TenantID: r.tenant, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Document{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Document{}, wrap(err, "leyendo documento")
	}
	return toDocument(x), nil
}

func (r *repository) GetByKey(ctx context.Context, key string) (domain.Document, bool, error) {
	x, err := r.q.GetDocumentByKey(ctx, documentssqlc.GetDocumentByKeyParams{TenantID: r.tenant, IdempotencyKey: pgtype.Text{String: key, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Document{}, false, nil
	}
	if err != nil {
		return domain.Document{}, false, wrap(err, "leyendo documento")
	}
	return toDocument(x), true, nil
}

func (r *repository) ListByOwner(ctx context.Context, t domain.OwnerType, owner uuid.UUID, sensitive bool, before uuid.NullUUID, limit int32) ([]domain.Document, error) {
	rows, err := r.q.ListByOwner(ctx, documentssqlc.ListByOwnerParams{TenantID: r.tenant, OwnerType: string(t), OwnerID: owner, IncludeSensitive: sensitive, Before: before, RowLimit: limit})
	return toDocuments(rows), wrap(err, "listando documentos")
}

func (r *repository) ListRetentionDue(ctx context.Context, until kernel.Date, sensitive bool, before uuid.NullUUID, limit int32) ([]domain.Document, error) {
	rows, err := r.q.ListRetentionDue(ctx, documentssqlc.ListRetentionDueParams{TenantID: r.tenant, CreatedUntil: until, IncludeSensitive: sensitive, Before: before, RowLimit: limit})
	return toDocuments(rows), wrap(err, "listando retención")
}

func (r *repository) Versions(ctx context.Context, id uuid.UUID) ([]domain.Version, error) {
	rows, err := r.q.ListVersions(ctx, documentssqlc.ListVersionsParams{TenantID: r.tenant, DocumentID: id})
	return toVersions(rows), wrap(err, "listando versiones")
}

func (r *repository) PendingScans(ctx context.Context, limit int32) ([]domain.Version, error) {
	rows, err := r.q.ListPendingScans(ctx, documentssqlc.ListPendingScansParams{TenantID: r.tenant, RowLimit: limit})
	return toVersions(rows), wrap(err, "listando pendientes")
}

func (r *repository) SetScan(ctx context.Context, id uuid.UUID, s domain.ScanStatus, at time.Time) error {
	_, err := r.q.SetScanResult(ctx, documentssqlc.SetScanResultParams{ScanStatus: string(s), At: &at, TenantID: r.tenant, ID: id})
	return wrap(err, "guardando antivirus")
}

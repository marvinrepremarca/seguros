// Package postgres implementa la persistencia de catalog con pgx + sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	catalogsqlc "github.com/marvinrepremarca/seguros/internal/modules/catalog/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/modules/catalog/app"
	"github.com/marvinrepremarca/seguros/internal/modules/catalog/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/catalog/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

const (
	uniqueViolation     = "23505"
	foreignKeyViolation = "23503"
	exclusionViolation  = "23P01"
)

// Store implementa app.Store.
type Store struct {
	db       *db.DB
	recorder *audit.Recorder
	clock    kernel.Clock
}

var _ app.Store = (*Store)(nil)

// NewStore construye el Store.
func NewStore(database *db.DB, recorder *audit.Recorder, clock kernel.Clock) *Store {
	return &Store{db: database, recorder: recorder, clock: clock}
}

// WithinTenant implementa app.Store.
func (s *Store) WithinTenant(ctx context.Context, fn func(ctx context.Context, uow app.UnitOfWork) error) error {
	tenant, ok := reqctx.TenantID(ctx)
	if !ok {
		return db.ErrNoTenant
	}
	return s.db.WithTenantTx(ctx, func(tx pgx.Tx) error {
		return fn(ctx, &unitOfWork{tx: tx, recorder: s.recorder, repo: &repository{q: catalogsqlc.New(tx), tenant: uuid.UUID(tenant), now: s.clock.Now().UTC()}})
	})
}

type unitOfWork struct {
	tx       pgx.Tx
	recorder *audit.Recorder
	repo     *repository
}

func (u *unitOfWork) Catalog() app.Repository { return u.repo }

func (u *unitOfWork) Audit(ctx context.Context, e audit.Entry) error {
	return u.recorder.Record(ctx, u.tx, e)
}

type repository struct {
	q      *catalogsqlc.Queries
	tenant uuid.UUID
	now    time.Time
}

func actor(ctx context.Context) uuid.NullUUID {
	if u, ok := reqctx.UserID(ctx); ok {
		return uuid.NullUUID{UUID: uuid.UUID(u), Valid: true}
	}
	return uuid.NullUUID{}
}

// writeErr traduce las restricciones de la BD a errores de dominio.
func writeErr(err error, op string) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case uniqueViolation:
			return domain.ErrNameTaken
		case exclusionViolation:
			return domain.ErrAgreementOverlap
		case foreignKeyViolation:
			return domain.ErrAgreementReference
		}
	}
	return fmt.Errorf("catalog: %s: %w", op, err)
}

func readErr(err error, op string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("catalog: %s: %w", op, err)
	}
	return nil
}

func updated(n int64, err error, op string) error {
	if err != nil {
		return writeErr(err, op)
	}
	if n == 0 {
		return domain.ErrConcurrentUpdate
	}
	return nil
}

func (r *repository) InsertInsurer(ctx context.Context, i domain.Item) error {
	return writeErr(r.q.InsertInsurer(ctx, catalogsqlc.InsertInsurerParams{
		ID: i.ID, TenantID: r.tenant, Name: i.Name, Active: i.Active, Now: r.now, Actor: actor(ctx),
	}), "insertando aseguradora")
}

func (r *repository) GetInsurer(ctx context.Context, id uuid.UUID) (domain.Item, error) {
	row, err := r.q.GetInsurer(ctx, catalogsqlc.GetInsurerParams{TenantID: r.tenant, ID: id})
	if err := readErr(err, "leyendo aseguradora"); err != nil {
		return domain.Item{}, err
	}
	return domain.Item{ID: row.ID, Name: row.Name, Active: row.Active, Version: row.Version}, nil
}

func (r *repository) ListInsurers(ctx context.Context, after uuid.NullUUID, limit int32) ([]domain.Item, error) {
	rows, err := r.q.ListInsurers(ctx, catalogsqlc.ListInsurersParams{TenantID: r.tenant, After: after, PageSize: limit})
	if err != nil {
		return nil, fmt.Errorf("catalog: listando aseguradoras: %w", err)
	}
	out := make([]domain.Item, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Item{ID: row.ID, Name: row.Name, Active: row.Active, Version: row.Version})
	}
	return out, nil
}

func (r *repository) UpdateInsurer(ctx context.Context, i domain.Item) error {
	n, err := r.q.UpdateInsurer(ctx, catalogsqlc.UpdateInsurerParams{
		Name: i.Name, Active: i.Active, Now: r.now, Actor: actor(ctx), TenantID: r.tenant, ID: i.ID, Version: i.Version,
	})
	return updated(n, err, "actualizando aseguradora")
}

func (r *repository) InsertLine(ctx context.Context, l domain.LineOfBusiness) error {
	return writeErr(r.q.InsertLine(ctx, catalogsqlc.InsertLineParams{
		ID: l.ID, TenantID: r.tenant, Name: l.Name, RiskSchemaKey: l.RiskSchemaKey, Active: l.Active, Now: r.now, Actor: actor(ctx),
	}), "insertando ramo")
}

func (r *repository) GetLine(ctx context.Context, id uuid.UUID) (domain.LineOfBusiness, error) {
	row, err := r.q.GetLine(ctx, catalogsqlc.GetLineParams{TenantID: r.tenant, ID: id})
	if err := readErr(err, "leyendo ramo"); err != nil {
		return domain.LineOfBusiness{}, err
	}
	return domain.LineOfBusiness{Item: domain.Item{ID: row.ID, Name: row.Name, Active: row.Active, Version: row.Version}, RiskSchemaKey: row.RiskSchemaKey}, nil
}

func (r *repository) ListLines(ctx context.Context, after uuid.NullUUID, limit int32) ([]domain.LineOfBusiness, error) {
	rows, err := r.q.ListLines(ctx, catalogsqlc.ListLinesParams{TenantID: r.tenant, After: after, PageSize: limit})
	if err != nil {
		return nil, fmt.Errorf("catalog: listando ramos: %w", err)
	}
	out := make([]domain.LineOfBusiness, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.LineOfBusiness{Item: domain.Item{ID: row.ID, Name: row.Name, Active: row.Active, Version: row.Version}, RiskSchemaKey: row.RiskSchemaKey})
	}
	return out, nil
}

func (r *repository) UpdateLine(ctx context.Context, l domain.LineOfBusiness) error {
	n, err := r.q.UpdateLine(ctx, catalogsqlc.UpdateLineParams{
		Name: l.Name, Active: l.Active, Now: r.now, Actor: actor(ctx), TenantID: r.tenant, ID: l.ID, Version: l.Version,
	})
	return updated(n, err, "actualizando ramo")
}

func (r *repository) InsertProduct(ctx context.Context, p domain.InsuranceProduct) error {
	return writeErr(r.q.InsertProduct(ctx, catalogsqlc.InsertProductParams{
		ID: p.ID, TenantID: r.tenant, InsurerID: p.InsurerID, LineOfBusinessID: p.LineOfBusinessID, Name: p.Name, Active: p.Active, Now: r.now, Actor: actor(ctx),
	}), "insertando producto")
}

func (r *repository) GetProduct(ctx context.Context, id uuid.UUID) (domain.InsuranceProduct, error) {
	row, err := r.q.GetProduct(ctx, catalogsqlc.GetProductParams{TenantID: r.tenant, ID: id})
	if err := readErr(err, "leyendo producto"); err != nil {
		return domain.InsuranceProduct{}, err
	}
	return domain.InsuranceProduct{
		Item:      domain.Item{ID: row.ID, Name: row.Name, Active: row.Active, Version: row.Version},
		InsurerID: row.InsurerID, LineOfBusinessID: row.LineOfBusinessID,
	}, nil
}

func (r *repository) ListProducts(ctx context.Context, after uuid.NullUUID, limit int32) ([]domain.InsuranceProduct, error) {
	rows, err := r.q.ListProducts(ctx, catalogsqlc.ListProductsParams{TenantID: r.tenant, After: after, PageSize: limit})
	if err != nil {
		return nil, fmt.Errorf("catalog: listando productos: %w", err)
	}
	out := make([]domain.InsuranceProduct, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.InsuranceProduct{
			Item:      domain.Item{ID: row.ID, Name: row.Name, Active: row.Active, Version: row.Version},
			InsurerID: row.InsurerID, LineOfBusinessID: row.LineOfBusinessID,
		})
	}
	return out, nil
}

func (r *repository) UpdateProduct(ctx context.Context, p domain.InsuranceProduct) error {
	n, err := r.q.UpdateProduct(ctx, catalogsqlc.UpdateProductParams{
		Name: p.Name, Active: p.Active, Now: r.now, Actor: actor(ctx), TenantID: r.tenant, ID: p.ID, Version: p.Version,
	})
	return updated(n, err, "actualizando producto")
}

func (r *repository) InsertAgreement(ctx context.Context, a domain.CommissionAgreement) error {
	s := a.Snapshot()
	return writeErr(r.q.InsertAgreement(ctx, catalogsqlc.InsertAgreementParams{
		ID: s.ID, TenantID: r.tenant, InsurerID: s.InsurerID, LineOfBusinessID: s.LineID, ProductID: s.ProductID,
		Basis: string(s.Basis), Percentage: s.Percentage.Decimal(), ValidFrom: s.Validity.From(), ValidTo: s.Validity.To(), Now: r.now, Actor: actor(ctx),
	}), "insertando acuerdo")
}

func agreement(id, insurer, line uuid.UUID, product uuid.NullUUID, basis string, row catalogsqlc.GetAgreementRow) (domain.CommissionAgreement, error) {
	validity, err := kernel.NewDateRange(row.ValidFrom, row.ValidTo)
	if err != nil {
		return domain.CommissionAgreement{}, fmt.Errorf("catalog: vigencia guardada inválida: %w", err)
	}
	pct, err := kernel.NewPercentage(row.Percentage)
	if err != nil {
		return domain.CommissionAgreement{}, fmt.Errorf("catalog: porcentaje guardado inválido: %w", err)
	}
	return domain.RehydrateAgreement(domain.AgreementSnapshot{
		ID: id, InsurerID: insurer, LineID: line, ProductID: product, Basis: domain.Basis(basis),
		Percentage: pct, Validity: validity, Version: row.Version,
	}), nil
}

func (r *repository) GetAgreement(ctx context.Context, id uuid.UUID) (domain.CommissionAgreement, error) {
	row, err := r.q.GetAgreement(ctx, catalogsqlc.GetAgreementParams{TenantID: r.tenant, ID: id})
	if err := readErr(err, "leyendo acuerdo"); err != nil {
		return domain.CommissionAgreement{}, err
	}
	return agreement(row.ID, row.InsurerID, row.LineOfBusinessID, row.ProductID, row.Basis, row)
}

func (r *repository) ListAgreements(ctx context.Context, after uuid.NullUUID, limit int32) ([]domain.CommissionAgreement, error) {
	rows, err := r.q.ListAgreements(ctx, catalogsqlc.ListAgreementsParams{TenantID: r.tenant, After: after, PageSize: limit})
	if err != nil {
		return nil, fmt.Errorf("catalog: listando acuerdos: %w", err)
	}
	out := make([]domain.CommissionAgreement, 0, len(rows))
	for _, row := range rows {
		a, err := agreement(row.ID, row.InsurerID, row.LineOfBusinessID, row.ProductID, row.Basis, catalogsqlc.GetAgreementRow(row))
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func (r *repository) UpdateAgreement(ctx context.Context, a domain.CommissionAgreement) error {
	s := a.Snapshot()
	n, err := r.q.UpdateAgreement(ctx, catalogsqlc.UpdateAgreementParams{
		Basis: string(s.Basis), Percentage: s.Percentage.Decimal(), ValidFrom: s.Validity.From(), ValidTo: s.Validity.To(),
		Now: r.now, Actor: actor(ctx), TenantID: r.tenant, ID: s.ID, Version: s.Version,
	})
	return updated(n, err, "actualizando acuerdo")
}

func (r *repository) AgreementFor(ctx context.Context, ref contracts.PolicyRef, on kernel.Date) (contracts.Agreement, bool, error) {
	row, err := r.q.AgreementFor(ctx, catalogsqlc.AgreementForParams{
		TenantID: r.tenant, InsurerID: ref.InsurerID, LineOfBusinessID: ref.LineOfBusinessID, ProductID: ref.ProductID, OnDate: on,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Agreement{}, false, nil
	}
	if err != nil {
		return contracts.Agreement{}, false, fmt.Errorf("catalog: buscando acuerdo: %w", err)
	}
	pct, err := kernel.NewPercentage(row.Percentage)
	if err != nil {
		return contracts.Agreement{}, false, fmt.Errorf("catalog: porcentaje guardado inválido: %w", err)
	}
	return contracts.Agreement{Basis: row.Basis, Percentage: pct}, true, nil
}

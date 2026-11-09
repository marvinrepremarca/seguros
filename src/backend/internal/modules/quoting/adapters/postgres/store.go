// Package postgres implementa la persistencia de cotización con pgx + sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	quotingsqlc "github.com/marvinrepremarca/seguros/internal/modules/quoting/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/modules/quoting/app"
	"github.com/marvinrepremarca/seguros/internal/modules/quoting/domain"
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
		return fn(ctx, &unitOfWork{tx: tx, store: s, repo: &repository{q: quotingsqlc.New(tx), tenant: uuid.UUID(tenant)}})
	})
}

type unitOfWork struct {
	tx    pgx.Tx
	store *Store
	repo  *repository
}

func (u *unitOfWork) Requests() app.Repository { return u.repo }

func (u *unitOfWork) Audit(ctx context.Context, e audit.Entry) error {
	return u.store.recorder.Record(ctx, u.tx, e)
}

func (u *unitOfWork) Publish(ctx context.Context, topic string, payload any) error {
	_, err := u.store.outbox.Add(ctx, u.tx, topic, payload)
	return err
}

type repository struct {
	q      *quotingsqlc.Queries
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
		return fmt.Errorf("quoting: %s: %w", op, err)
	}
	return nil
}

func (r *repository) Insert(ctx context.Context, s domain.Snapshot, at time.Time) error {
	return wrap(r.q.InsertRequest(ctx, quotingsqlc.InsertRequestParams{
		ID: s.ID, TenantID: r.tenant, LeadID: s.LeadID, HolderPartyID: s.HolderPartyID, LineOfBusinessID: s.LineOfBusinessID, AgentID: s.AgentID,
		RiskDescription: s.RiskDescription, RiskData: s.RiskData, At: at, Actor: actor(ctx),
	}), "creando solicitud")
}

func (r *repository) Get(ctx context.Context, id uuid.UUID) (*domain.Request, error) {
	x, err := r.q.GetRequest(ctx, quotingsqlc.GetRequestParams{TenantID: r.tenant, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, wrap(err, "leyendo solicitud")
	}
	offers, err := r.offers(ctx, id)
	if err != nil {
		return nil, err
	}
	return domain.Rehydrate(domain.Snapshot{
		ID: x.ID, LeadID: x.LeadID, HolderPartyID: x.HolderPartyID, LineOfBusinessID: x.LineOfBusinessID, AgentID: x.AgentID, RiskDescription: x.RiskDescription, RiskData: x.RiskData,
		Status: domain.RequestStatus(x.Status), DeclineReason: x.DeclineReason.String, PolicyID: x.PolicyID, Offers: offers, Version: x.Version,
	}), nil
}

func (r *repository) offers(ctx context.Context, id uuid.UUID) ([]domain.Offer, error) {
	rows, err := r.q.ListOffers(ctx, quotingsqlc.ListOffersParams{TenantID: r.tenant, RequestID: id})
	if err != nil {
		return nil, wrap(err, "leyendo ofertas")
	}
	coverages, err := r.q.ListCoverages(ctx, quotingsqlc.ListCoveragesParams{TenantID: r.tenant, RequestID: id})
	if err != nil {
		return nil, wrap(err, "leyendo coberturas")
	}
	currency := make(map[uuid.UUID]kernel.Currency, len(rows))
	byOffer := make(map[uuid.UUID][]domain.Coverage, len(rows))
	for _, o := range rows {
		currency[o.ID] = kernel.Currency(o.Currency)
	}
	for _, c := range coverages {
		sum, err := kernel.NewMoney(c.SumInsured, currency[c.OfferID])
		if err != nil {
			return nil, wrap(err, "suma asegurada guardada")
		}
		byOffer[c.OfferID] = append(byOffer[c.OfferID], domain.Coverage{Name: c.Name, SumInsured: sum, Deductible: c.Deductible.String})
	}
	out := make([]domain.Offer, 0, len(rows))
	for _, o := range rows {
		net, err := kernel.NewMoney(o.NetPremium, kernel.Currency(o.Currency))
		if err != nil {
			return nil, wrap(err, "prima guardada")
		}
		taxes, err := kernel.NewMoney(o.Taxes, kernel.Currency(o.Currency))
		if err != nil {
			return nil, wrap(err, "impuestos guardados")
		}
		out = append(out, domain.Offer{
			ID: o.ID, InsurerID: o.InsurerID, ProductID: o.ProductID, Reference: o.Reference.String, NetPremium: net, Taxes: taxes, EffectiveFrom: o.EffectiveFrom, EffectiveTo: o.EffectiveTo,
			ValidUntil: o.ValidUntil, Status: domain.OfferStatus(o.Status), RiskSchemaKey: o.RiskSchemaKey, RiskSchemaVersion: o.RiskSchemaVersion, Coverages: byOffer[o.ID],
		})
	}
	return out, nil
}

// Save actualiza la solicitud y el estado de sus ofertas.
func (r *repository) Save(ctx context.Context, q *domain.Request, at time.Time) error {
	s := q.Snapshot()
	n, err := r.q.UpdateRequest(ctx, quotingsqlc.UpdateRequestParams{
		TenantID: r.tenant, ID: s.ID, Status: string(s.Status), DeclineReason: text(s.DeclineReason), PolicyID: s.PolicyID, At: at, Actor: actor(ctx), Version: s.Version,
	})
	if err != nil {
		return wrap(err, "guardando solicitud")
	}
	if n == 0 {
		return domain.ErrConcurrentUpdate
	}
	// Primero las descartadas: el índice único admite una sola aceptada.
	for _, want := range []domain.OfferStatus{domain.OfferDiscarded, domain.OfferAccepted} {
		for _, o := range s.Offers {
			if o.Status == want {
				if err := r.q.SetOfferStatus(ctx, quotingsqlc.SetOfferStatusParams{TenantID: r.tenant, ID: o.ID, Status: string(want)}); err != nil {
					return wrap(err, "guardando oferta")
				}
			}
		}
	}
	return nil
}

func (r *repository) InsertOffer(ctx context.Context, request uuid.UUID, o domain.Offer, at time.Time) error {
	err := r.q.InsertOffer(ctx, quotingsqlc.InsertOfferParams{
		ID: o.ID, TenantID: r.tenant, RequestID: request, InsurerID: o.InsurerID, ProductID: o.ProductID, Reference: text(o.Reference), Currency: string(o.NetPremium.Currency()),
		NetPremium: o.NetPremium.Amount(), Taxes: o.Taxes.Amount(), EffectiveFrom: o.EffectiveFrom, EffectiveTo: o.EffectiveTo, ValidUntil: o.ValidUntil,
		RiskSchemaKey: o.RiskSchemaKey, RiskSchemaVersion: o.RiskSchemaVersion, At: at, Actor: actor(ctx),
	})
	if err != nil {
		return wrap(err, "creando oferta")
	}
	for i, c := range o.Coverages {
		if err := r.q.InsertCoverage(ctx, quotingsqlc.InsertCoverageParams{
			TenantID: r.tenant, OfferID: o.ID, Position: int32(i), Name: c.Name, SumInsured: c.SumInsured.Amount(), Deductible: text(c.Deductible), //nolint:gosec // máximo 20
		}); err != nil {
			return wrap(err, "creando cobertura")
		}
	}
	return nil
}

func (r *repository) List(ctx context.Context, status string, agent, before uuid.NullUUID, limit int32) ([]domain.Snapshot, error) {
	rows, err := r.q.ListRequests(ctx, quotingsqlc.ListRequestsParams{TenantID: r.tenant, Status: text(status), Agent: agent, Before: before, PageSize: limit})
	if err != nil {
		return nil, wrap(err, "listando solicitudes")
	}
	out := make([]domain.Snapshot, 0, len(rows))
	for _, x := range rows {
		out = append(out, domain.Snapshot{
			ID: x.ID, LeadID: x.LeadID, HolderPartyID: x.HolderPartyID, LineOfBusinessID: x.LineOfBusinessID, AgentID: x.AgentID, RiskDescription: x.RiskDescription, RiskData: x.RiskData,
			Status: domain.RequestStatus(x.Status), DeclineReason: x.DeclineReason.String, PolicyID: x.PolicyID, Version: x.Version,
		})
	}
	return out, nil
}

func (r *repository) OfferValidDays(ctx context.Context) (int32, error) {
	d, err := r.q.GetOfferValidDays(ctx, r.tenant)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultOfferValidDays, nil
	}
	return d, wrap(err, "leyendo días de vigencia")
}

func (r *repository) SetOfferValidDays(ctx context.Context, days int32, at time.Time) error {
	return wrap(r.q.SetOfferValidDays(ctx, quotingsqlc.SetOfferValidDaysParams{TenantID: r.tenant, OfferValidDays: days, At: at, Actor: actor(ctx)}), "guardando días de vigencia")
}

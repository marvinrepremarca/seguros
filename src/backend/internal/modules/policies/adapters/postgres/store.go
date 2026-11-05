// Package postgres implementa la persistencia de policies con pgx + sqlc.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	policiessqlc "github.com/marvinrepremarca/seguros/internal/modules/policies/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/modules/policies/app"
	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

const uniqueViolation = "23505"

// Store implementa app.Store.
type Store struct {
	db       *db.DB
	recorder *audit.Recorder
	outbox   *outbox.Writer
}

var _ app.Store = (*Store)(nil)

// NewStore construye el Store.
func NewStore(database *db.DB, recorder *audit.Recorder, writer *outbox.Writer) *Store {
	return &Store{db: database, recorder: recorder, outbox: writer}
}

// WithinTenant implementa app.Store.
func (s *Store) WithinTenant(ctx context.Context, fn func(ctx context.Context, uow app.UnitOfWork) error) error {
	tenant, ok := reqctx.TenantID(ctx)
	if !ok {
		return db.ErrNoTenant
	}
	return s.db.WithTenantTx(ctx, func(tx pgx.Tx) error {
		return fn(ctx, &unitOfWork{tx: tx, recorder: s.recorder, outbox: s.outbox, repo: &repository{q: policiessqlc.New(tx), tenant: uuid.UUID(tenant)}})
	})
}

// Bind devuelve la unidad de trabajo sobre una transacción ya abierta (consumidores del outbox, cuya
// transacción incluye el inbox).
func (s *Store) Bind(ctx context.Context, tx pgx.Tx) (app.UnitOfWork, error) {
	tenant, ok := reqctx.TenantID(ctx)
	if !ok {
		return nil, db.ErrNoTenant
	}
	return &unitOfWork{tx: tx, recorder: s.recorder, outbox: s.outbox, repo: &repository{q: policiessqlc.New(tx), tenant: uuid.UUID(tenant)}}, nil
}

// DueForExpiration implementa app.Store mediante la función SECURITY DEFINER (cruza tenants, solo
// devuelve IDs y solo la puede ejecutar el rol del worker).
func (s *Store) DueForExpiration(ctx context.Context, today kernel.Date, limit int32) ([]app.DuePolicy, error) {
	var out []app.DuePolicy
	err := s.db.WithoutTenantTx(ctx, func(tx pgx.Tx) error {
		rows, err := policiessqlc.New(tx).DueForExpiration(ctx, policiessqlc.DueForExpirationParams{Today: today, PageSize: limit})
		if err != nil {
			return fmt.Errorf("policies: listando pólizas por vencer: %w", err)
		}
		out = make([]app.DuePolicy, 0, len(rows))
		for _, r := range rows {
			out = append(out, app.DuePolicy{Tenant: kernel.TenantID(r.TenantID), ID: domain.PolicyID(r.ID)})
		}
		return nil
	})
	return out, err
}

type unitOfWork struct {
	tx       pgx.Tx
	recorder *audit.Recorder
	outbox   *outbox.Writer
	repo     *repository
}

func (u *unitOfWork) Policies() app.PolicyRepository { return u.repo }

func (u *unitOfWork) Audit(ctx context.Context, e audit.Entry) error {
	return u.recorder.Record(ctx, u.tx, e)
}

func (u *unitOfWork) Publish(ctx context.Context, topic string, payload any) error {
	_, err := u.outbox.Add(ctx, u.tx, topic, payload)
	return err
}

type repository struct {
	q      *policiessqlc.Queries
	tenant uuid.UUID
}

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

func actor(ctx context.Context) uuid.NullUUID {
	if u, ok := reqctx.UserID(ctx); ok {
		return uuid.NullUUID{UUID: uuid.UUID(u), Valid: true}
	}
	return uuid.NullUUID{}
}

// cancellationColumns son las columnas de la cancelación (todas nulas si no hay).
type cancellationColumns struct {
	cause     pgtype.Text
	date      *kernel.Date
	unearned  decimal.NullDecimal
	reference pgtype.Text
}

func cancellationOf(c *domain.Cancellation) cancellationColumns {
	if c == nil {
		return cancellationColumns{}
	}
	d := c.EffectiveDate
	return cancellationColumns{
		cause: text(string(c.Cause)), date: &d,
		unearned:  decimal.NullDecimal{Decimal: c.UnearnedPremium.Amount(), Valid: true},
		reference: text(c.InsurerReference),
	}
}

func (r *repository) Insert(ctx context.Context, p *domain.Policy) error {
	s := p.Snapshot()
	t := s.Terms
	c := cancellationOf(s.Cancellation)
	if err := r.q.InsertPolicy(ctx, policiessqlc.InsertPolicyParams{
		ID: uuid.UUID(s.ID), TenantID: r.tenant, Status: string(s.Status), PolicyNumber: text(s.Number), InsurerID: t.InsurerID,
		LineOfBusinessID: t.LineOfBusinessID, ProductID: t.ProductID, AgentID: t.AgentID, Currency: string(t.NetPremium.Currency()),
		NetPremium: t.NetPremium.Amount(), Taxes: t.Taxes.Amount(), EffectiveFrom: t.Period.From(), EffectiveTo: t.Period.To(),
		PaymentForm: string(t.PaymentForm), RenewedFromPolicyID: s.RenewedFrom, IssuedAt: s.IssuedAt, RejectionReason: text(s.RejectionReason),
		CancellationCause: c.cause, CancellationDate: c.date, UnearnedPremium: c.unearned, CancellationReference: c.reference,
		CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, Actor: actor(ctx),
	}); err != nil {
		return mapWriteError(err, "insertando póliza")
	}
	return r.writeChildren(ctx, s)
}

func mapWriteError(err error, op string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "policies_number_key" {
		return domain.ErrNumberTaken
	}
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "policies_one_renewal_key" {
		return app.ErrRenewalExist
	}
	return fmt.Errorf("policies: %s: %w", op, err)
}

func (r *repository) Save(ctx context.Context, p *domain.Policy) error {
	s := p.Snapshot()
	t := s.Terms
	c := cancellationOf(s.Cancellation)
	n, err := r.q.UpdatePolicy(ctx, policiessqlc.UpdatePolicyParams{
		Status: string(s.Status), PolicyNumber: text(s.Number), InsurerID: t.InsurerID, LineOfBusinessID: t.LineOfBusinessID,
		ProductID: t.ProductID, AgentID: t.AgentID, Currency: string(t.NetPremium.Currency()), NetPremium: t.NetPremium.Amount(), Taxes: t.Taxes.Amount(),
		EffectiveFrom: t.Period.From(), EffectiveTo: t.Period.To(), PaymentForm: string(t.PaymentForm), RenewedFromPolicyID: s.RenewedFrom,
		IssuedAt: s.IssuedAt, RejectionReason: text(s.RejectionReason), CancellationCause: c.cause, CancellationDate: c.date,
		UnearnedPremium: c.unearned, CancellationReference: c.reference, UpdatedAt: s.UpdatedAt, Actor: actor(ctx),
		TenantID: r.tenant, ID: uuid.UUID(s.ID), Version: s.Version,
	})
	if err != nil {
		return mapWriteError(err, "actualizando póliza")
	}
	if n == 0 {
		return domain.ErrConcurrentUpdate
	}
	id := uuid.UUID(s.ID)
	if err := r.q.DeletePolicyParties(ctx, policiessqlc.DeletePolicyPartiesParams{TenantID: r.tenant, PolicyID: id}); err != nil {
		return fmt.Errorf("policies: reemplazando partes: %w", err)
	}
	if err := r.q.DeleteCoverages(ctx, policiessqlc.DeleteCoveragesParams{TenantID: r.tenant, PolicyID: id}); err != nil {
		return fmt.Errorf("policies: reemplazando coberturas: %w", err)
	}
	if err := r.q.DeleteInsuredItems(ctx, policiessqlc.DeleteInsuredItemsParams{TenantID: r.tenant, PolicyID: id}); err != nil {
		return fmt.Errorf("policies: reemplazando bienes: %w", err)
	}
	return r.writeChildren(ctx, s)
}

func (r *repository) writeChildren(ctx context.Context, s domain.Snapshot) error {
	id := uuid.UUID(s.ID)
	for _, p := range s.Terms.Parties {
		if err := r.q.InsertPolicyParty(ctx, policiessqlc.InsertPolicyPartyParams{TenantID: r.tenant, PolicyID: id, PartyID: p.PartyID, Role: string(p.Role)}); err != nil {
			return fmt.Errorf("policies: guardando parte: %w", err)
		}
	}
	for _, c := range s.Terms.Coverages {
		if err := r.q.InsertCoverage(ctx, policiessqlc.InsertCoverageParams{
			ID: c.ID, TenantID: r.tenant, PolicyID: id, Name: c.Name, SumInsured: c.SumInsured.Amount(), Deductible: text(c.Deductible),
		}); err != nil {
			return fmt.Errorf("policies: guardando cobertura: %w", err)
		}
	}
	for _, i := range s.Terms.Items {
		if err := r.q.InsertInsuredItem(ctx, policiessqlc.InsertInsuredItemParams{
			ID: i.ID, TenantID: r.tenant, PolicyID: id, Description: i.Description, RiskSchemaKey: i.RiskSchemaKey,
			RiskSchemaVersion: i.RiskSchemaVersion, RiskData: i.RiskData,
		}); err != nil {
			return fmt.Errorf("policies: guardando bien: %w", err)
		}
	}
	for _, e := range s.Endorsements {
		if err := r.upsertEndorsement(ctx, id, e); err != nil {
			return err
		}
	}
	return nil
}

// effectsJSON registra lo que aplicó un endoso emitido, como historial.
type effectsJSON struct {
	CoverageChanges []coverageChangeJSON `json:"coverageChanges,omitempty"`
	AddParties      []partyJSON          `json:"addParties,omitempty"`
	RemoveParties   []partyJSON          `json:"removeParties,omitempty"`
	ItemChanges     []itemChangeJSON     `json:"itemChanges,omitempty"`
}

type coverageChangeJSON struct {
	CoverageID uuid.UUID `json:"coverageId"`
	SumInsured string    `json:"sumInsured"`
}

type partyJSON struct {
	PartyID uuid.UUID `json:"partyId"`
	Role    string    `json:"role"`
}

type itemChangeJSON struct {
	ItemID   uuid.UUID       `json:"itemId"`
	RiskData json.RawMessage `json:"riskData"`
}

// effectsColumn devuelve nil si no hay efectos: al guardar, la columna conserva el valor anterior
// (los efectos no se rehidratan porque nada los vuelve a aplicar).
func effectsColumn(e domain.EndorsementEffects) ([]byte, error) {
	if len(e.CoverageChanges)+len(e.AddParties)+len(e.RemoveParties)+len(e.ItemChanges) == 0 {
		return nil, nil
	}
	out := effectsJSON{}
	for _, c := range e.CoverageChanges {
		out.CoverageChanges = append(out.CoverageChanges, coverageChangeJSON{CoverageID: c.CoverageID, SumInsured: c.SumInsured.Amount().String()})
	}
	for _, p := range e.AddParties {
		out.AddParties = append(out.AddParties, partyJSON{PartyID: p.PartyID, Role: string(p.Role)})
	}
	for _, p := range e.RemoveParties {
		out.RemoveParties = append(out.RemoveParties, partyJSON{PartyID: p.PartyID, Role: string(p.Role)})
	}
	for _, c := range e.ItemChanges {
		out.ItemChanges = append(out.ItemChanges, itemChangeJSON{ItemID: c.ItemID, RiskData: c.RiskData})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("policies: serializando efectos: %w", err)
	}
	return b, nil
}

func nullMoney(m *kernel.Money) decimal.NullDecimal {
	if m == nil {
		return decimal.NullDecimal{}
	}
	return decimal.NullDecimal{Decimal: m.Amount(), Valid: true}
}

func (r *repository) upsertEndorsement(ctx context.Context, policy uuid.UUID, e domain.Endorsement) error {
	effects, err := effectsColumn(e.Effects)
	if err != nil {
		return err
	}
	if err := r.q.UpsertEndorsement(ctx, policiessqlc.UpsertEndorsementParams{
		ID: e.ID, TenantID: r.tenant, PolicyID: policy, Type: string(e.Type), Status: string(e.Status), Description: e.Description,
		EffectiveDate: e.EffectiveDate, RequestedAt: e.RequestedAt, RequestedBy: actor(ctx), ResolvedAt: e.ResolvedAt,
		EndorsementNumber: text(e.Number), NetPremiumDelta: nullMoney(e.NetPremiumDelta), TaxesDelta: nullMoney(e.TaxesDelta),
		Effects: effects, RejectionReason: text(e.RejectionReason),
	}); err != nil {
		return fmt.Errorf("policies: guardando endoso: %w", err)
	}
	return nil
}

func (r *repository) Get(ctx context.Context, id domain.PolicyID) (*domain.Policy, error) {
	pid := uuid.UUID(id)
	row, err := r.q.GetPolicy(ctx, policiessqlc.GetPolicyParams{TenantID: r.tenant, ID: pid})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("policies: leyendo póliza: %w", err)
	}
	currency := kernel.Currency(row.Currency)
	net, err := kernel.NewMoney(row.NetPremium, currency)
	if err != nil {
		return nil, fmt.Errorf("policies: prima guardada: %w", err)
	}
	taxes, err := kernel.NewMoney(row.Taxes, currency)
	if err != nil {
		return nil, fmt.Errorf("policies: impuestos guardados: %w", err)
	}
	period, err := kernel.NewDateRange(row.EffectiveFrom, row.EffectiveTo)
	if err != nil {
		return nil, fmt.Errorf("policies: vigencia guardada: %w", err)
	}
	s := domain.Snapshot{
		ID: id, Status: domain.Status(row.Status), Number: row.PolicyNumber.String, RenewedFrom: row.RenewedFromPolicyID, IssuedAt: row.IssuedAt,
		RejectionReason: row.RejectionReason.String, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Version: row.Version,
		Terms: domain.Terms{
			InsurerID: row.InsurerID, LineOfBusinessID: row.LineOfBusinessID, ProductID: row.ProductID, AgentID: row.AgentID,
			NetPremium: net, Taxes: taxes, Period: period, PaymentForm: domain.PaymentForm(row.PaymentForm),
		},
	}
	if row.CancellationCause.Valid && row.CancellationDate != nil && row.UnearnedPremium.Valid {
		unearned, err := kernel.NewMoney(row.UnearnedPremium.Decimal, currency)
		if err != nil {
			return nil, fmt.Errorf("policies: prima no devengada guardada: %w", err)
		}
		s.Cancellation = &domain.Cancellation{
			Cause: domain.CancellationCause(row.CancellationCause.String), EffectiveDate: *row.CancellationDate,
			UnearnedPremium: unearned, InsurerReference: row.CancellationReference.String,
		}
	}
	if err := r.loadChildren(ctx, pid, currency, &s); err != nil {
		return nil, err
	}
	return domain.Rehydrate(s), nil
}

func (r *repository) loadChildren(ctx context.Context, id uuid.UUID, currency kernel.Currency, s *domain.Snapshot) error {
	parties, err := r.q.ListPolicyParties(ctx, policiessqlc.ListPolicyPartiesParams{TenantID: r.tenant, PolicyID: id})
	if err != nil {
		return fmt.Errorf("policies: leyendo partes: %w", err)
	}
	for _, p := range parties {
		s.Terms.Parties = append(s.Terms.Parties, domain.PolicyParty{PartyID: p.PartyID, Role: domain.PartyRole(p.Role)})
	}
	coverages, err := r.q.ListCoverages(ctx, policiessqlc.ListCoveragesParams{TenantID: r.tenant, PolicyID: id})
	if err != nil {
		return fmt.Errorf("policies: leyendo coberturas: %w", err)
	}
	for _, c := range coverages {
		sum, err := kernel.NewMoney(c.SumInsured, currency)
		if err != nil {
			return fmt.Errorf("policies: suma guardada: %w", err)
		}
		s.Terms.Coverages = append(s.Terms.Coverages, domain.Coverage{ID: c.ID, Name: c.Name, SumInsured: sum, Deductible: c.Deductible.String})
	}
	items, err := r.q.ListInsuredItems(ctx, policiessqlc.ListInsuredItemsParams{TenantID: r.tenant, PolicyID: id})
	if err != nil {
		return fmt.Errorf("policies: leyendo bienes: %w", err)
	}
	for _, i := range items {
		s.Terms.Items = append(s.Terms.Items, domain.InsuredItem{
			ID: i.ID, Description: i.Description, RiskSchemaKey: i.RiskSchemaKey, RiskSchemaVersion: i.RiskSchemaVersion, RiskData: i.RiskData,
		})
	}
	endorsements, err := r.q.ListEndorsements(ctx, policiessqlc.ListEndorsementsParams{TenantID: r.tenant, PolicyID: id})
	if err != nil {
		return fmt.Errorf("policies: leyendo endosos: %w", err)
	}
	for _, e := range endorsements {
		out := domain.Endorsement{
			ID: e.ID, Type: domain.EndorsementType(e.Type), Status: domain.EndorsementStatus(e.Status), Description: e.Description,
			EffectiveDate: e.EffectiveDate, RequestedAt: e.RequestedAt, ResolvedAt: e.ResolvedAt, Number: e.EndorsementNumber.String,
			RejectionReason: e.RejectionReason.String,
		}
		if out.NetPremiumDelta, err = optionalMoney(e.NetPremiumDelta, currency); err != nil {
			return err
		}
		if out.TaxesDelta, err = optionalMoney(e.TaxesDelta, currency); err != nil {
			return err
		}
		s.Endorsements = append(s.Endorsements, out)
	}
	return nil
}

func (r *repository) IDsAfter(ctx context.Context, after uuid.NullUUID, limit int32) ([]domain.PolicyID, error) {
	rows, err := r.q.ListPolicyIDsAfter(ctx, policiessqlc.ListPolicyIDsAfterParams{TenantID: r.tenant, After: after, PageSize: limit})
	if err != nil {
		return nil, fmt.Errorf("policies: recorriendo pólizas: %w", err)
	}
	out := make([]domain.PolicyID, len(rows))
	for i, id := range rows {
		out[i] = domain.PolicyID(id)
	}
	return out, nil
}

func (r *repository) InsertTransferRequest(ctx context.Context, q app.TransferRequest) error {
	if err := r.q.InsertTransferRequest(ctx, policiessqlc.InsertTransferRequestParams{
		ID: q.ID, TenantID: r.tenant, FromAgentID: q.From, ToAgentID: q.To, Reason: q.Reason.Code, Note: text(q.Reason.Note),
		RequestedBy: q.RequestedBy, RequestedAt: q.RequestedAt,
	}); err != nil {
		return fmt.Errorf("policies: guardando solicitud de traspaso: %w", err)
	}
	return nil
}

func (r *repository) AppendAgentHistory(ctx context.Context, e domain.PolicyAgentChanged) error {
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("policies: id de historial: %w", err)
	}
	if err := r.q.InsertAgentHistory(ctx, policiessqlc.InsertAgentHistoryParams{
		ID: id, TenantID: r.tenant, PolicyID: uuid.UUID(e.PolicyID), PreviousAgentID: e.Previous, AgentID: e.New,
		EffectiveAt: e.At, Reason: e.Reason, Note: text(e.Note), ChangedBy: actor(ctx),
	}); err != nil {
		return fmt.Errorf("policies: guardando historial de asesor: %w", err)
	}
	return nil
}

func (r *repository) ReassignableByAgent(ctx context.Context, agent uuid.UUID, cutoff time.Time, limit int32) ([]domain.PolicyID, error) {
	rows, err := r.q.ListReassignablePolicyIDs(ctx, policiessqlc.ListReassignablePolicyIDsParams{
		TenantID: r.tenant, AgentID: uuid.NullUUID{UUID: agent, Valid: true}, RequestedAt: cutoff, PageSize: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("policies: listando cartera: %w", err)
	}
	out := make([]domain.PolicyID, 0, len(rows))
	for _, id := range rows {
		out = append(out, domain.PolicyID(id))
	}
	return out, nil
}

func optionalMoney(d decimal.NullDecimal, currency kernel.Currency) (*kernel.Money, error) {
	if !d.Valid {
		return nil, nil //nolint:nilnil // nulo en la BD: el endoso aún no tiene variación
	}
	m, err := kernel.NewMoney(d.Decimal, currency)
	if err != nil {
		return nil, fmt.Errorf("policies: variación guardada: %w", err)
	}
	return &m, nil
}

func (r *repository) List(ctx context.Context, f app.ListFilter) ([]app.Summary, error) {
	rows, err := r.q.ListPolicies(ctx, policiessqlc.ListPoliciesParams{
		TenantID: r.tenant, Before: f.Before, AgentID: f.Agent, Status: text(string(f.Status)), PageSize: f.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("policies: listando pólizas: %w", err)
	}
	out := make([]app.Summary, 0, len(rows))
	for _, row := range rows {
		currency := kernel.Currency(row.Currency)
		net, err := kernel.NewMoney(row.NetPremium, currency)
		if err != nil {
			return nil, fmt.Errorf("policies: prima guardada: %w", err)
		}
		taxes, err := kernel.NewMoney(row.Taxes, currency)
		if err != nil {
			return nil, fmt.Errorf("policies: impuestos guardados: %w", err)
		}
		out = append(out, app.Summary{
			ID: domain.PolicyID(row.ID), Status: domain.Status(row.Status), Number: row.PolicyNumber.String, InsurerID: row.InsurerID,
			LineOfBusinessID: row.LineOfBusinessID, HolderPartyID: row.HolderPartyID, NetPremium: net, Taxes: taxes,
			EffectiveFrom: row.EffectiveFrom, EffectiveTo: row.EffectiveTo,
		})
	}
	return out, nil
}

func (r *repository) RenewalsOf(ctx context.Context, previous domain.PolicyID) ([]app.Renewal, error) {
	rows, err := r.q.RenewalsOf(ctx, policiessqlc.RenewalsOfParams{TenantID: r.tenant, Previous: uuid.NullUUID{UUID: uuid.UUID(previous), Valid: true}})
	if err != nil {
		return nil, fmt.Errorf("policies: buscando renovaciones: %w", err)
	}
	out := make([]app.Renewal, len(rows))
	for i, x := range rows {
		out[i] = app.Renewal{ID: domain.PolicyID(x.ID), Status: domain.Status(x.Status)}
	}
	return out, nil
}

func (r *repository) RenewalSettings(ctx context.Context) (app.RenewalSettings, error) {
	row, err := r.q.GetRenewalSettings(ctx, r.tenant)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.RenewalSettings{KeepHolder: true, KeepLine: true}, nil
	}
	if err != nil {
		return app.RenewalSettings{}, fmt.Errorf("policies: leyendo reglas de renovación: %w", err)
	}
	return app.RenewalSettings{KeepHolder: row.KeepHolder, KeepLine: row.KeepLine}, nil
}

func (r *repository) SetRenewalSettings(ctx context.Context, v app.RenewalSettings, at time.Time) error {
	err := r.q.SetRenewalSettings(ctx, policiessqlc.SetRenewalSettingsParams{TenantID: r.tenant, KeepHolder: v.KeepHolder, KeepLine: v.KeepLine, At: at, Actor: actor(ctx)})
	if err != nil {
		return fmt.Errorf("policies: guardando reglas de renovación: %w", err)
	}
	return nil
}

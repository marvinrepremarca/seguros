// Package app contiene los casos de uso de comisiones.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/marvinrepremarca/seguros/internal/modules/commissions/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Errores de la aplicación.
var (
	ErrStatementExists = kernel.NewError(kernel.KindConflict, "commissions.statement.already_registered", "esa liquidación ya está registrada")
	ErrInvalidRate     = kernel.NewError(kernel.KindValidation, "commissions.rate.invalid", "tasa entre 0 y 100, con vigencia válida")
)

// Terms es la proyección local de la póliza.
type Terms struct {
	PolicyID, InsurerID, LineID uuid.UUID
	ProductID                   uuid.NullUUID
	PolicyNumber                string
	Net, Taxes                  kernel.Money
}

// Accrual es una fila de lectura de la comisión causada.
type Accrual struct {
	ID, PolicyID     uuid.UUID
	PolicyNumber     string
	Kind             domain.Kind
	Base             decimal.Decimal
	Percentage       decimal.Decimal
	Amount           decimal.Decimal
	Currency         string
	AgentID          uuid.NullUUID
	AccruedOn        kernel.Date
	MissingAgreement bool
	Reconciled       bool
}

// PendingAccrual es una causación sin conciliar con el ramo de su póliza.
type PendingAccrual struct {
	Accrual *domain.Accrual
	LineID  uuid.UUID
}

// Discrepancy es una diferencia abierta.
type Discrepancy struct {
	ID, InsurerID      uuid.UUID
	StatementReference string
	PolicyNumber       string
	PolicyID           uuid.NullUUID
	Reported, Expected decimal.Decimal
}

// Line es una línea de liquidación por registrar.
type Line struct {
	StatementID        uuid.UUID
	PolicyNumber       string
	PolicyID           uuid.NullUUID
	Reported, Expected decimal.Decimal
	Matched            bool
}

// AgentCommission es la comisión interna.
type AgentCommission struct {
	ID, AccrualID, AgentID uuid.UUID
	Percentage, Amount     decimal.Decimal
	Currency               string
	MissingRate            bool
}

// AgentRate es la tasa de un asesor.
type AgentRate struct {
	ID, AgentID uuid.UUID
	LineID      uuid.NullUUID
	Percentage  decimal.Decimal
	From        kernel.Date
	To          *kernel.Date
}

// Repository persiste comisiones en el tenant del contexto.
type Repository interface {
	UpsertTerms(ctx context.Context, t Terms) error
	GetTerms(ctx context.Context, policyID uuid.UUID) (Terms, bool, error)
	FindPolicy(ctx context.Context, insurerID uuid.UUID, number string) (uuid.NullUUID, error)
	InsertAgent(ctx context.Context, policyID uuid.UUID, agent uuid.NullUUID, on kernel.Date) error
	AgentOn(ctx context.Context, policyID uuid.UUID, on kernel.Date) (uuid.NullUUID, error)
	InsertAccrual(ctx context.Context, a *domain.Accrual, at time.Time) error
	AccrualBySource(ctx context.Context, kind domain.Kind, source uuid.UUID) (*domain.Accrual, bool, error)
	ListAccruals(ctx context.Context, onlyPending bool, after uuid.NullUUID, limit int32) ([]Accrual, error)
	PendingAccruals(ctx context.Context, policyID uuid.UUID) ([]PendingAccrual, error)
	// MarkReconciled concilia solo las causaciones comparadas: una que llegue después queda pendiente.
	MarkReconciled(ctx context.Context, ids []uuid.UUID, statementID uuid.UUID) error
	InsertStatement(ctx context.Context, id, insurer uuid.UUID, reference string, on kernel.Date, at time.Time) error
	CloseDiscrepancies(ctx context.Context, insurer uuid.UUID, number string, at time.Time) error
	InsertLine(ctx context.Context, l Line) error
	ListDiscrepancies(ctx context.Context, after uuid.NullUUID, limit int32) ([]Discrepancy, error)
	RateFor(ctx context.Context, agent, line uuid.UUID, on kernel.Date) (decimal.Decimal, bool, error)
	InsertAgentCommission(ctx context.Context, c AgentCommission, at time.Time) error
	ListAgentCommissions(ctx context.Context, agent uuid.NullUUID, after uuid.NullUUID, limit int32) ([]AgentCommission, error)
	InsertRate(ctx context.Context, r AgentRate, at time.Time) error
	ListRates(ctx context.Context) ([]AgentRate, error)
	Tolerance(ctx context.Context, currency kernel.Currency) (kernel.Money, error)
	ListTolerances(ctx context.Context) ([]kernel.Money, error)
	SetTolerance(ctx context.Context, v kernel.Money, at time.Time) error
}

// UnitOfWork es una transacción del tenant.
type UnitOfWork interface {
	Commissions() Repository
	Audit(ctx context.Context, e audit.Entry) error
}

// Store abre unidades de trabajo.
type Store interface {
	WithinTenant(ctx context.Context, fn func(ctx context.Context, uow UnitOfWork) error) error
}

// Service agrupa los casos de uso.
type Service struct {
	Store Store
	Clock kernel.Clock
}

func newID() (uuid.UUID, error) { return uuid.NewV7() }

func page[T any](rows []T, size int32, id func(T) uuid.UUID) ([]T, string) {
	if int32(len(rows)) <= size { //nolint:gosec // acotado por el límite
		return rows, ""
	}
	rows = rows[:size]
	return rows, kernel.EncodeCursor(id(rows[size-1]))
}

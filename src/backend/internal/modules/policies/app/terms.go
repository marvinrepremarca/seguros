package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	catalog "github.com/marvinrepremarca/seguros/internal/modules/catalog/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// CoverageInput es una cobertura recibida.
type CoverageInput struct {
	Name       string
	SumInsured decimal.Decimal
	Deductible string
}

// ItemInput es un bien asegurado recibido; RiskData es JSON.
type ItemInput struct {
	Description string
	RiskData    []byte
}

// TermsInput son los datos recibidos de una póliza.
type TermsInput struct {
	InsurerID        uuid.UUID
	LineOfBusinessID uuid.UUID
	ProductID        uuid.NullUUID
	AgentID          uuid.NullUUID
	Currency         kernel.Currency
	NetPremium       decimal.Decimal
	Taxes            decimal.Decimal
	EffectiveFrom    kernel.Date
	EffectiveTo      kernel.Date
	PaymentForm      domain.PaymentForm
	Parties          []domain.PolicyParty
	Coverages        []CoverageInput
	Items            []ItemInput
}

// buildTerms valida contra los otros módulos (catálogo, esquema de riesgo, terceros, asesor). Corre
// fuera de la transacción de la póliza: cada módulo abre la suya.
func (d Deps) buildTerms(ctx context.Context, in TermsInput) (domain.Terms, error) {
	net, err := kernel.NewMoney(in.NetPremium, in.Currency)
	if err != nil {
		return domain.Terms{}, err
	}
	taxes, err := kernel.NewMoney(in.Taxes, in.Currency)
	if err != nil {
		return domain.Terms{}, err
	}
	period, err := kernel.NewDateRange(in.EffectiveFrom, in.EffectiveTo)
	if err != nil {
		return domain.Terms{}, err
	}
	schemaKey, err := d.Catalog.ResolvePolicyRef(ctx, catalog.PolicyRef{InsurerID: in.InsurerID, LineOfBusinessID: in.LineOfBusinessID, ProductID: in.ProductID})
	if err != nil {
		return domain.Terms{}, err
	}
	if err := d.checkParties(ctx, in.Parties); err != nil {
		return domain.Terms{}, err
	}
	if err := d.checkAgent(ctx, in.AgentID); err != nil {
		return domain.Terms{}, err
	}
	coverages := make([]domain.Coverage, 0, len(in.Coverages))
	for _, c := range in.Coverages {
		sum, err := kernel.NewMoney(c.SumInsured, in.Currency)
		if err != nil {
			return domain.Terms{}, err
		}
		id, err := uuid.NewV7()
		if err != nil {
			return domain.Terms{}, fmt.Errorf("policies: %w", err)
		}
		coverages = append(coverages, domain.Coverage{ID: id, Name: c.Name, SumInsured: sum, Deductible: c.Deductible})
	}
	version, ok := d.Schemas.Current(schemaKey)
	if !ok {
		return domain.Terms{}, catalog.ErrUnknownRiskSchema
	}
	items := make([]domain.InsuredItem, 0, len(in.Items))
	for _, i := range in.Items {
		if err := d.Schemas.Validate(schemaKey, i.RiskData); err != nil {
			return domain.Terms{}, err
		}
		id, err := uuid.NewV7()
		if err != nil {
			return domain.Terms{}, fmt.Errorf("policies: %w", err)
		}
		items = append(items, domain.InsuredItem{ID: id, Description: i.Description, RiskSchemaKey: schemaKey, RiskSchemaVersion: version, RiskData: i.RiskData})
	}
	return domain.Terms{
		InsurerID: in.InsurerID, LineOfBusinessID: in.LineOfBusinessID, ProductID: in.ProductID, AgentID: in.AgentID,
		NetPremium: net, Taxes: taxes, Period: period, PaymentForm: in.PaymentForm,
		Parties: in.Parties, Coverages: coverages, Items: items,
	}, nil
}

func (d Deps) checkParties(ctx context.Context, parties []domain.PolicyParty) error {
	ids := make([]uuid.UUID, 0, len(parties))
	for _, p := range parties {
		ids = append(ids, p.PartyID)
	}
	found, err := d.Parties.Summaries(ctx, ids)
	if err != nil {
		return fmt.Errorf("policies: consultando partes: %w", err)
	}
	for _, id := range ids {
		if _, ok := found[id]; !ok {
			return domain.ErrInvalidParty
		}
	}
	return nil
}

func (d Deps) checkAgent(ctx context.Context, agent uuid.NullUUID) error {
	if !agent.Valid {
		return nil
	}
	return d.checkActiveAgent(ctx, agent.UUID)
}

// checkActiveAgent exige que el usuario sea del tenant y siga activo como asesor.
func (d Deps) checkActiveAgent(ctx context.Context, agent uuid.UUID) error {
	users, err := d.Users.Agents(ctx)
	if err != nil {
		return fmt.Errorf("policies: consultando asesores: %w", err)
	}
	for _, u := range users {
		if u.ID == agent && u.Active {
			return nil
		}
	}
	return domain.ErrInvalidAgent
}

package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	catalog "github.com/marvinrepremarca/seguros/internal/modules/catalog/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/quoting/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// CoverageInput es una cobertura recibida.
type CoverageInput struct {
	Name       string
	SumInsured decimal.Decimal
	Deductible string
}

// OfferInput son los datos de una oferta.
type OfferInput struct {
	InsurerID     uuid.UUID
	ProductID     uuid.NullUUID
	Reference     string
	Currency      kernel.Currency
	NetPremium    decimal.Decimal
	Taxes         decimal.Decimal
	EffectiveFrom kernel.Date
	EffectiveTo   kernel.Date
	ValidUntil    kernel.Date
	Coverages     []CoverageInput
}

func buildOffer(in OfferInput) (domain.Offer, error) {
	net, err := kernel.NewMoney(in.NetPremium, in.Currency)
	if err != nil {
		return domain.Offer{}, domain.ErrInvalidOffer
	}
	taxes, err := kernel.NewMoney(in.Taxes, in.Currency)
	if err != nil {
		return domain.Offer{}, domain.ErrInvalidOffer
	}
	id, err := uuid.NewV7()
	if err != nil {
		return domain.Offer{}, fmt.Errorf("quoting: %w", err)
	}
	o := domain.Offer{
		ID: id, InsurerID: in.InsurerID, ProductID: in.ProductID, Reference: in.Reference, NetPremium: net, Taxes: taxes,
		EffectiveFrom: in.EffectiveFrom, EffectiveTo: in.EffectiveTo, ValidUntil: in.ValidUntil,
	}
	for _, c := range in.Coverages {
		sum, err := kernel.NewMoney(c.SumInsured, in.Currency)
		if err != nil {
			return domain.Offer{}, domain.ErrInvalidOffer
		}
		o.Coverages = append(o.Coverages, domain.Coverage{Name: c.Name, SumInsured: sum, Deductible: c.Deductible})
	}
	return o, nil
}

// AddOffer registra la propuesta de una aseguradora y valida el riesgo contra el esquema del ramo.
func (s *Service) AddOffer(ctx context.Context, id uuid.UUID, in OfferInput) (View, error) {
	if err := authz.Require(ctx, authz.QuoteManageOwn); err != nil {
		return View{}, err
	}
	user, err := currentUser(ctx)
	if err != nil {
		return View{}, err
	}
	offer, err := buildOffer(in)
	if err != nil {
		return View{}, err
	}
	// Lee fuera de la transacción: catálogo abre la suya.
	var current domain.Snapshot
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		r, err := loadOwn(ctx, uow, id, user)
		if err != nil {
			return err
		}
		current = r.Snapshot()
		return nil
	})
	if err != nil {
		return View{}, err
	}
	key, err := s.Catalog.ResolvePolicyRef(ctx, catalog.PolicyRef{InsurerID: in.InsurerID, LineOfBusinessID: current.LineOfBusinessID, ProductID: in.ProductID})
	if err != nil {
		return View{}, err
	}
	if err := s.Schemas.Validate(key, current.RiskData); err != nil {
		return View{}, err
	}
	version, ok := s.Schemas.Current(key)
	if !ok {
		return View{}, catalog.ErrUnknownRiskSchema
	}
	offer.RiskSchemaKey, offer.RiskSchemaVersion = key, version
	var snap domain.Snapshot
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		r, err := loadOwn(ctx, uow, id, user)
		if err != nil {
			return err
		}
		days, err := uow.Requests().OfferValidDays(ctx)
		if err != nil {
			return err
		}
		added, err := r.AddOffer(offer, kernel.Today(s.Clock), days)
		if err != nil {
			return err
		}
		now := s.Clock.Now().UTC()
		if err := uow.Requests().InsertOffer(ctx, id, added, now); err != nil {
			return err
		}
		// Sube la versión: dos cambios simultáneos sobre la solicitud no se pisan.
		if err := uow.Requests().Save(ctx, r, now); err != nil {
			return err
		}
		snap = r.Snapshot()
		e := entry("quoting.offer.added", id)
		e.Details = map[string]string{"offer": added.ID.String()}
		return uow.Audit(ctx, e)
	})
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, snap)
}

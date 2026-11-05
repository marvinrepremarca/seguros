package app

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/marvinrepremarca/seguros/internal/modules/catalog/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/catalog/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// checkReference verifica que aseguradora y ramo existan y estén activos.
func checkReference(ctx context.Context, repo Repository, insurer, line uuid.UUID) error {
	i, err := repo.GetInsurer(ctx, insurer)
	if err := asInvalidReference(err); err != nil {
		return err
	}
	l, err := repo.GetLine(ctx, line)
	if err := asInvalidReference(err); err != nil {
		return err
	}
	if !i.Active || !l.Active {
		return contracts.ErrInvalidReference
	}
	return nil
}

func asInvalidReference(err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return contracts.ErrInvalidReference
	}
	return err
}

// ListAgreements lista acuerdos de comisión.
func (s *Service) ListAgreements(ctx context.Context, q PageQuery) (Page[domain.CommissionAgreement], error) {
	return list(ctx, s, authz.AgreementRead, q, func(a domain.CommissionAgreement) uuid.UUID { return a.ID() }, Repository.ListAgreements)
}

// CreateAgreementCommand registra un acuerdo de comisión.
type CreateAgreementCommand struct {
	InsurerID        uuid.UUID
	LineOfBusinessID uuid.UUID
	ProductID        uuid.NullUUID
	Basis            domain.Basis
	Percentage       decimal.Decimal
	ValidFrom        kernel.Date
	ValidTo          kernel.Date
}

// CreateAgreement registra el acuerdo; la BD impide que se solape con otro del mismo ámbito.
func (s *Service) CreateAgreement(ctx context.Context, cmd CreateAgreementCommand) (domain.CommissionAgreement, error) {
	if err := authz.Require(ctx, authz.AgreementManage); err != nil {
		return domain.CommissionAgreement{}, err
	}
	id, err := newUUID()
	if err != nil {
		return domain.CommissionAgreement{}, err
	}
	pct, err := kernel.NewPercentage(cmd.Percentage)
	if err != nil {
		return domain.CommissionAgreement{}, err
	}
	agreement, err := domain.NewCommissionAgreement(id, cmd.InsurerID, cmd.LineOfBusinessID, cmd.ProductID, cmd.Basis, pct, cmd.ValidFrom, cmd.ValidTo)
	if err != nil {
		return domain.CommissionAgreement{}, err
	}
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := checkReference(ctx, uow.Catalog(), cmd.InsurerID, cmd.LineOfBusinessID); err != nil {
			return err
		}
		if cmd.ProductID.Valid {
			p, err := uow.Catalog().GetProduct(ctx, cmd.ProductID.UUID)
			if err := asInvalidReference(err); err != nil {
				return err
			}
			if p.InsurerID != cmd.InsurerID || p.LineOfBusinessID != cmd.LineOfBusinessID {
				return contracts.ErrInvalidReference
			}
		}
		if err := uow.Catalog().InsertAgreement(ctx, agreement); err != nil {
			return err
		}
		return uow.Audit(ctx, entry("catalog.commission_agreement.created", "catalog.commission_agreement", id, map[string]string{
			"basis": string(cmd.Basis), "percentage": pct.String(),
		}))
	})
	return agreement, err
}

// UpdateAgreementCommand cambia los términos de un acuerdo.
type UpdateAgreementCommand struct {
	ID         uuid.UUID
	Basis      domain.Basis
	Percentage decimal.Decimal
	ValidFrom  kernel.Date
	ValidTo    kernel.Date
	Version    int32
}

// UpdateAgreement cambia los términos y audita los valores anteriores y nuevos.
func (s *Service) UpdateAgreement(ctx context.Context, cmd UpdateAgreementCommand) (domain.CommissionAgreement, error) {
	if err := authz.Require(ctx, authz.AgreementManage); err != nil {
		return domain.CommissionAgreement{}, err
	}
	pct, err := kernel.NewPercentage(cmd.Percentage)
	if err != nil {
		return domain.CommissionAgreement{}, err
	}
	var out domain.CommissionAgreement
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		current, err := uow.Catalog().GetAgreement(ctx, cmd.ID)
		if err != nil {
			return err
		}
		out, err = current.Changed(cmd.Basis, pct, cmd.ValidFrom, cmd.ValidTo, cmd.Version)
		if err != nil {
			return err
		}
		if err := uow.Catalog().UpdateAgreement(ctx, out); err != nil {
			return err
		}
		// La versión siguiente es la que quedó en la BD tras la concurrencia optimista.
		stored, err := uow.Catalog().GetAgreement(ctx, cmd.ID)
		if err != nil {
			return err
		}
		before, after := current.Snapshot(), stored.Snapshot()
		out = stored
		return uow.Audit(ctx, entry("catalog.commission_agreement.updated", "catalog.commission_agreement", cmd.ID, map[string]string{
			"previousPercentage": before.Percentage.String(), "percentage": after.Percentage.String(),
			"previousBasis": string(before.Basis), "basis": string(after.Basis),
			"previousValidFrom": before.Validity.From().String(), "validFrom": after.Validity.From().String(),
			"previousValidTo": before.Validity.To().String(), "validTo": after.Validity.To().String(),
		}))
	})
	return out, err
}

// Directory implementa contracts.Directory.
type Directory struct{ Deps }

var _ contracts.Directory = (*Directory)(nil)

// ResolvePolicyRef implementa contracts.Directory.
func (d *Directory) ResolvePolicyRef(ctx context.Context, ref contracts.PolicyRef) (string, error) {
	var key string
	err := d.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		repo := uow.Catalog()
		if err := checkReference(ctx, repo, ref.InsurerID, ref.LineOfBusinessID); err != nil {
			return err
		}
		line, err := repo.GetLine(ctx, ref.LineOfBusinessID)
		if err != nil {
			return err
		}
		key = line.RiskSchemaKey
		if !ref.ProductID.Valid {
			return nil
		}
		p, err := repo.GetProduct(ctx, ref.ProductID.UUID)
		if err := asInvalidReference(err); err != nil {
			return err
		}
		if !p.Active || p.InsurerID != ref.InsurerID || p.LineOfBusinessID != ref.LineOfBusinessID {
			return contracts.ErrInvalidReference
		}
		return nil
	})
	return key, err
}

// AgreementFor implementa contracts.Directory.
func (d *Directory) AgreementFor(ctx context.Context, ref contracts.PolicyRef, on kernel.Date) (contracts.Agreement, bool, error) {
	var out contracts.Agreement
	var found bool
	err := d.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		out, found, err = uow.Catalog().AgreementFor(ctx, ref, on)
		return err
	})
	return out, found, err
}

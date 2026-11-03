package app

import (
	"context"
	"strconv"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/catalog/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// UpdateItemCommand cambia nombre y estado de un registro.
type UpdateItemCommand struct {
	ID      uuid.UUID
	Name    string
	Active  bool
	Version int32
}

// ListInsurers lista aseguradoras.
func (s *Service) ListInsurers(ctx context.Context, q PageQuery) (Page[domain.Item], error) {
	return list(ctx, s, authz.CatalogRead, q, func(i domain.Item) uuid.UUID { return i.ID }, Repository.ListInsurers)
}

// CreateInsurer registra una aseguradora.
func (s *Service) CreateInsurer(ctx context.Context, name string) (domain.Item, error) {
	if err := authz.Require(ctx, authz.CatalogManage); err != nil {
		return domain.Item{}, err
	}
	id, err := newUUID()
	if err != nil {
		return domain.Item{}, err
	}
	item, err := domain.NewItem(id, name)
	if err != nil {
		return domain.Item{}, err
	}
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Catalog().InsertInsurer(ctx, item); err != nil {
			return err
		}
		return uow.Audit(ctx, entry("catalog.insurer.created", "catalog.insurer", id, nil))
	})
	return item, err
}

// UpdateInsurer cambia una aseguradora.
func (s *Service) UpdateInsurer(ctx context.Context, cmd UpdateItemCommand) (domain.Item, error) {
	if err := authz.Require(ctx, authz.CatalogManage); err != nil {
		return domain.Item{}, err
	}
	var out domain.Item
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		current, err := uow.Catalog().GetInsurer(ctx, cmd.ID)
		if err != nil {
			return err
		}
		out, err = current.Changed(cmd.Name, cmd.Active, cmd.Version)
		if err != nil {
			return err
		}
		if err := uow.Catalog().UpdateInsurer(ctx, out); err != nil {
			return err
		}
		out.Version++
		return uow.Audit(ctx, entry("catalog.insurer.updated", "catalog.insurer", cmd.ID, activeDetail(cmd.Active)))
	})
	return out, err
}

// ListLines lista ramos.
func (s *Service) ListLines(ctx context.Context, q PageQuery) (Page[domain.LineOfBusiness], error) {
	return list(ctx, s, authz.CatalogRead, q, func(l domain.LineOfBusiness) uuid.UUID { return l.ID }, Repository.ListLines)
}

// CreateLine registra un ramo con su esquema de riesgo.
func (s *Service) CreateLine(ctx context.Context, name, riskSchemaKey string) (domain.LineOfBusiness, error) {
	if err := authz.Require(ctx, authz.CatalogManage); err != nil {
		return domain.LineOfBusiness{}, err
	}
	if !s.Schemas.Known(riskSchemaKey) {
		return domain.LineOfBusiness{}, domain.ErrInvalidRiskSchema
	}
	id, err := newUUID()
	if err != nil {
		return domain.LineOfBusiness{}, err
	}
	item, err := domain.NewItem(id, name)
	if err != nil {
		return domain.LineOfBusiness{}, err
	}
	line := domain.LineOfBusiness{Item: item, RiskSchemaKey: riskSchemaKey}
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Catalog().InsertLine(ctx, line); err != nil {
			return err
		}
		return uow.Audit(ctx, entry("catalog.line_of_business.created", "catalog.line_of_business", id, map[string]string{"riskSchema": riskSchemaKey}))
	})
	return line, err
}

// UpdateLine cambia un ramo (el esquema de riesgo no cambia).
func (s *Service) UpdateLine(ctx context.Context, cmd UpdateItemCommand) (domain.LineOfBusiness, error) {
	if err := authz.Require(ctx, authz.CatalogManage); err != nil {
		return domain.LineOfBusiness{}, err
	}
	var out domain.LineOfBusiness
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		current, err := uow.Catalog().GetLine(ctx, cmd.ID)
		if err != nil {
			return err
		}
		item, err := current.Changed(cmd.Name, cmd.Active, cmd.Version)
		if err != nil {
			return err
		}
		out = domain.LineOfBusiness{Item: item, RiskSchemaKey: current.RiskSchemaKey}
		if err := uow.Catalog().UpdateLine(ctx, out); err != nil {
			return err
		}
		out.Version++
		return uow.Audit(ctx, entry("catalog.line_of_business.updated", "catalog.line_of_business", cmd.ID, activeDetail(cmd.Active)))
	})
	return out, err
}

// ListProducts lista productos.
func (s *Service) ListProducts(ctx context.Context, q PageQuery) (Page[domain.InsuranceProduct], error) {
	return list(ctx, s, authz.CatalogRead, q, func(p domain.InsuranceProduct) uuid.UUID { return p.ID }, Repository.ListProducts)
}

// CreateProductCommand registra un producto.
type CreateProductCommand struct {
	Name             string
	InsurerID        uuid.UUID
	LineOfBusinessID uuid.UUID
}

// CreateProduct registra un producto de una aseguradora y un ramo activos.
func (s *Service) CreateProduct(ctx context.Context, cmd CreateProductCommand) (domain.InsuranceProduct, error) {
	if err := authz.Require(ctx, authz.CatalogManage); err != nil {
		return domain.InsuranceProduct{}, err
	}
	id, err := newUUID()
	if err != nil {
		return domain.InsuranceProduct{}, err
	}
	item, err := domain.NewItem(id, cmd.Name)
	if err != nil {
		return domain.InsuranceProduct{}, err
	}
	product := domain.InsuranceProduct{Item: item, InsurerID: cmd.InsurerID, LineOfBusinessID: cmd.LineOfBusinessID}
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := checkReference(ctx, uow.Catalog(), cmd.InsurerID, cmd.LineOfBusinessID); err != nil {
			return err
		}
		if err := uow.Catalog().InsertProduct(ctx, product); err != nil {
			return err
		}
		return uow.Audit(ctx, entry("catalog.product.created", "catalog.product", id, nil))
	})
	return product, err
}

// UpdateProduct cambia un producto (aseguradora y ramo no cambian).
func (s *Service) UpdateProduct(ctx context.Context, cmd UpdateItemCommand) (domain.InsuranceProduct, error) {
	if err := authz.Require(ctx, authz.CatalogManage); err != nil {
		return domain.InsuranceProduct{}, err
	}
	var out domain.InsuranceProduct
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		current, err := uow.Catalog().GetProduct(ctx, cmd.ID)
		if err != nil {
			return err
		}
		item, err := current.Changed(cmd.Name, cmd.Active, cmd.Version)
		if err != nil {
			return err
		}
		out = domain.InsuranceProduct{Item: item, InsurerID: current.InsurerID, LineOfBusinessID: current.LineOfBusinessID}
		if err := uow.Catalog().UpdateProduct(ctx, out); err != nil {
			return err
		}
		out.Version++
		return uow.Audit(ctx, entry("catalog.product.updated", "catalog.product", cmd.ID, activeDetail(cmd.Active)))
	})
	return out, err
}

func activeDetail(active bool) map[string]string {
	return map[string]string{"active": strconv.FormatBool(active)}
}

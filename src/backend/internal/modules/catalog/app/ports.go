// Package app contiene los casos de uso de catalog.
package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/catalog/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Repository persiste el catálogo del tenant del contexto. Los Get devuelven domain.ErrNotFound;
// los Update aplican concurrencia optimista sobre la versión del valor recibido.
type Repository interface {
	InsertInsurer(ctx context.Context, i domain.Item) error
	GetInsurer(ctx context.Context, id uuid.UUID) (domain.Item, error)
	ListInsurers(ctx context.Context, after uuid.NullUUID, limit int32) ([]domain.Item, error)
	UpdateInsurer(ctx context.Context, i domain.Item) error

	InsertLine(ctx context.Context, l domain.LineOfBusiness) error
	GetLine(ctx context.Context, id uuid.UUID) (domain.LineOfBusiness, error)
	ListLines(ctx context.Context, after uuid.NullUUID, limit int32) ([]domain.LineOfBusiness, error)
	UpdateLine(ctx context.Context, l domain.LineOfBusiness) error

	InsertProduct(ctx context.Context, p domain.InsuranceProduct) error
	GetProduct(ctx context.Context, id uuid.UUID) (domain.InsuranceProduct, error)
	ListProducts(ctx context.Context, after uuid.NullUUID, limit int32) ([]domain.InsuranceProduct, error)
	UpdateProduct(ctx context.Context, p domain.InsuranceProduct) error

	InsertAgreement(ctx context.Context, a domain.CommissionAgreement) error
	GetAgreement(ctx context.Context, id uuid.UUID) (domain.CommissionAgreement, error)
	ListAgreements(ctx context.Context, after uuid.NullUUID, limit int32) ([]domain.CommissionAgreement, error)
	UpdateAgreement(ctx context.Context, a domain.CommissionAgreement) error
}

// UnitOfWork agrupa repositorio y auditoría en una transacción con RLS.
type UnitOfWork interface {
	Catalog() Repository
	Audit(ctx context.Context, e audit.Entry) error
}

// Store abre unidades de trabajo para el tenant del contexto.
type Store interface {
	WithinTenant(ctx context.Context, fn func(ctx context.Context, uow UnitOfWork) error) error
}

// RiskSchemaKeys indica qué esquemas de riesgo existen.
type RiskSchemaKeys interface {
	Known(key string) bool
}

// Deps son las dependencias de los casos de uso.
type Deps struct {
	Store   Store
	Schemas RiskSchemaKeys
}

// Service agrupa los casos de uso del catálogo: todos siguen el mismo patrón de autorizar, validar y
// guardar con auditoría sobre registros de referencia, sin reglas propias que justifiquen un tipo por
// operación.
type Service struct{ Deps }

// PageQuery pide una página.
type PageQuery struct {
	Cursor string
	Limit  int32
}

func (q PageQuery) resolve() (uuid.NullUUID, int32, error) {
	after, err := kernel.DecodeCursor(q.Cursor)
	if err != nil {
		return uuid.NullUUID{}, 0, err
	}
	return after, kernel.PageSize(q.Limit), nil
}

// Page es una página de resultados.
type Page[T any] struct {
	Items      []T
	NextCursor string
}

// list autoriza y pagina por ID: pide un registro de más para saber si hay otra página.
func list[T any](ctx context.Context, s *Service, perm authz.Permission, q PageQuery, id func(T) uuid.UUID,
	fetch func(Repository, context.Context, uuid.NullUUID, int32) ([]T, error),
) (Page[T], error) {
	if err := authz.Require(ctx, perm); err != nil {
		return Page[T]{}, err
	}
	after, size, err := q.resolve()
	if err != nil {
		return Page[T]{}, err
	}
	var page Page[T]
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		rows, err := fetch(uow.Catalog(), ctx, after, size+1)
		if err != nil {
			return err
		}
		if int32(len(rows)) > size { //nolint:gosec // acotado por MaxPageSize + 1
			rows = rows[:size]
			page.NextCursor = kernel.EncodeCursor(id(rows[size-1]))
		}
		page.Items = rows
		return nil
	})
	return page, err
}

func entry(action, resourceType string, id uuid.UUID, details map[string]string) audit.Entry {
	return audit.Entry{Action: action, ResourceType: resourceType, ResourceID: id.String(), Outcome: audit.OutcomeSuccess, Details: details}
}

func newUUID() (uuid.UUID, error) {
	id, err := kernel.NewID[uuid.UUID]()
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("catalog: %w", err)
	}
	return id, nil
}

package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/catalog/app"
	"github.com/marvinrepremarca/seguros/internal/modules/catalog/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

var errStoreReached = errors.New("no debió llegar a la base de datos")

type unreachableStore struct{}

func (unreachableStore) WithinTenant(context.Context, func(context.Context, app.UnitOfWork) error) error {
	return errStoreReached
}

type schemaKeys struct{}

func (schemaKeys) Known(key string) bool { return key == "auto" }

func as(t *testing.T, roles ...authz.Role) context.Context {
	t.Helper()
	policy, err := authz.DefaultPolicy()
	require.NoError(t, err)
	return authz.WithPrincipal(context.Background(), authz.NewPrincipal(kernel.UserID(uuid.New()), kernel.TenantID(uuid.New()), roles, policy))
}

func TestCatalogRejectsBeforeTouchingTheStore(t *testing.T) {
	t.Parallel()
	s := &app.Service{Deps: app.Deps{Store: unreachableStore{}, Schemas: schemaKeys{}}}
	admin, sales := as(t, authz.RoleTenantAdmin), as(t, authz.RoleSalesAgent)

	_, err := s.CreateInsurer(context.Background(), "Sura")
	require.ErrorIs(t, err, authz.ErrUnauthenticated)
	_, err = s.CreateInsurer(sales, "Sura")
	require.ErrorIs(t, err, authz.ErrForbidden)
	_, err = s.CreateLine(sales, "Autos", "auto")
	require.ErrorIs(t, err, authz.ErrForbidden)
	_, err = s.CreateLine(admin, "Autos", "desconocido")
	require.ErrorIs(t, err, domain.ErrInvalidRiskSchema)
	_, err = s.CreateAgreement(sales, app.CreateAgreementCommand{})
	require.ErrorIs(t, err, authz.ErrForbidden)
	_, err = s.CreateInsurer(admin, "Sura")
	assert.ErrorIs(t, err, errStoreReached, "con permiso y datos válidos sí guarda")
}

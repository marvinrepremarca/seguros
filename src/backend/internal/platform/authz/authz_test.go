package authz_test

import (
	"context"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

func policy(t *testing.T) *authz.Policy {
	t.Helper()
	p, err := authz.DefaultPolicy()
	require.NoError(t, err)
	return p
}

func principal(t *testing.T, roles ...authz.Role) authz.Principal {
	t.Helper()
	return authz.NewPrincipal(kernel.UserID(uuid.Must(uuid.NewV7())), kernel.TenantID(uuid.Must(uuid.NewV7())), roles, policy(t))
}

func TestNewPolicyValidates(t *testing.T) {
	t.Parallel()
	_, err := authz.NewPolicy(map[authz.Role][]authz.Permission{"Root": {authz.FeatureFlagRead}})
	require.Error(t, err, "rol desconocido")
	_, err = authz.NewPolicy(map[authz.Role][]authz.Permission{authz.RoleManager: {"flags.read"}})
	require.Error(t, err, "permiso sin 3 segmentos")
	_, err = authz.NewPolicy(map[authz.Role][]authz.Permission{authz.RoleManager: {"Billing.Payment.Read"}})
	require.Error(t, err, "permiso con mayúsculas")
}

func TestDefaultGrantsAreValid(t *testing.T) {
	t.Parallel()
	p := policy(t)
	for role, perms := range authz.DefaultGrants() {
		for _, perm := range perms {
			assert.True(t, p.Known(perm), "%s → %s", role, perm)
		}
	}
	assert.False(t, p.Known("billing.payment.reverse"))
}

func TestPermissionsForUnionsRoles(t *testing.T) {
	t.Parallel()
	p := policy(t)
	got := p.PermissionsFor([]authz.Role{authz.RoleManager, authz.RoleTenantAdmin})
	assert.Equal(t, []authz.Permission{authz.FeatureFlagRead, authz.FeatureFlagUpdate}, got, "ordenados y sin duplicados")
	assert.Empty(t, p.PermissionsFor([]authz.Role{authz.RoleSalesAgent}))
}

func TestParseRoles(t *testing.T) {
	t.Parallel()
	got := authz.ParseRoles([]string{"Manager", "offline_access", "TenantAdmin", "Manager", "tenantadmin"})
	assert.Equal(t, []authz.Role{authz.RoleManager, authz.RoleTenantAdmin}, got)
	assert.Empty(t, authz.ParseRoles(nil))
}

func TestRequire(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, authz.Require(context.Background(), authz.FeatureFlagRead), authz.ErrUnauthenticated)
	require.ErrorIs(t, authz.RequireAuthenticated(context.Background()), authz.ErrUnauthenticated)

	ctx := authz.WithPrincipal(context.Background(), principal(t, authz.RoleManager))
	require.NoError(t, authz.RequireAuthenticated(ctx))
	require.NoError(t, authz.Require(ctx, authz.FeatureFlagRead))
	require.ErrorIs(t, authz.Require(ctx, authz.FeatureFlagUpdate), authz.ErrForbidden)

	de, ok := kernel.AsDomainError(authz.Require(ctx, authz.FeatureFlagUpdate))
	require.True(t, ok)
	assert.Equal(t, kernel.KindForbidden, de.Kind())
}

func TestWithPrincipalSetsTenantAndUser(t *testing.T) {
	t.Parallel()
	p := principal(t, authz.RoleOperations)
	ctx := authz.WithPrincipal(context.Background(), p)
	tid, ok := reqctx.TenantID(ctx)
	require.True(t, ok)
	assert.Equal(t, p.TenantID, tid)
	uid, ok := reqctx.UserID(ctx)
	require.True(t, ok)
	assert.Equal(t, p.UserID, uid)
	assert.True(t, p.HasRole(authz.RoleOperations))
	assert.False(t, p.HasRole(authz.RoleManager))
}

func TestPrincipalWithZeroIDsIsRejected(t *testing.T) {
	t.Parallel()
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{})
	_, ok := authz.PrincipalFrom(ctx)
	assert.False(t, ok)
}

func TestPermissionsAreCopied(t *testing.T) {
	t.Parallel()
	p := principal(t, authz.RoleTenantAdmin)
	perms := p.Permissions()
	perms[0] = "x.y.z"
	assert.True(t, p.Has(authz.FeatureFlagRead), "modificar la copia no altera el principal")
}

func spec(ops map[string]any) *openapi3.T {
	paths := openapi3.NewPaths()
	for id, perm := range ops {
		op := &openapi3.Operation{OperationID: id, Extensions: map[string]any{}}
		if perm != nil {
			op.Extensions[authz.XPermission] = perm
		}
		item := &openapi3.PathItem{}
		item.SetOperation("GET", op)
		paths.Set("/"+id, item)
	}
	return &openapi3.T{Paths: paths}
}

func TestOperationGuard(t *testing.T) {
	t.Parallel()
	g, err := authz.NewOperationGuard(spec(map[string]any{
		"login": authz.PermPublic, "me": authz.PermAuthenticated, "listFlags": string(authz.FeatureFlagRead),
	}), policy(t))
	require.NoError(t, err)
	assert.Equal(t, []string{"login"}, g.PublicOperations())
	req, ok := g.Requirement("listFlags")
	require.True(t, ok)
	assert.Equal(t, string(authz.FeatureFlagRead), req)

	anon := context.Background()
	sales := authz.WithPrincipal(context.Background(), principal(t, authz.RoleSalesAgent))
	manager := authz.WithPrincipal(context.Background(), principal(t, authz.RoleManager))

	require.NoError(t, g.Check(anon, "login"))
	require.ErrorIs(t, g.Check(anon, "me"), authz.ErrUnauthenticated)
	require.NoError(t, g.Check(sales, "me"))
	require.ErrorIs(t, g.Check(anon, "listFlags"), authz.ErrUnauthenticated)
	require.ErrorIs(t, g.Check(sales, "listFlags"), authz.ErrForbidden)
	require.NoError(t, g.Check(manager, "listFlags"))
	require.ErrorIs(t, g.Check(manager, "noRegistrada"), authz.ErrForbidden, "deny-by-default")
}

func TestOperationGuardRejectsUndeclaredOrUnknownPermissions(t *testing.T) {
	t.Parallel()
	p := policy(t)
	_, err := authz.NewOperationGuard(spec(map[string]any{"a": nil}), p)
	require.ErrorContains(t, err, "no declara x-permission")
	_, err = authz.NewOperationGuard(spec(map[string]any{"a": ""}), p)
	require.Error(t, err)
	_, err = authz.NewOperationGuard(spec(map[string]any{"a": 3}), p)
	require.Error(t, err)
	_, err = authz.NewOperationGuard(spec(map[string]any{"a": "billing.payment.reverse"}), p)
	require.ErrorContains(t, err, "fuera del catálogo")
	_, err = authz.NewOperationGuard(nil, p)
	require.Error(t, err)

	noID := openapi3.NewPaths()
	item := &openapi3.PathItem{}
	item.SetOperation("GET", &openapi3.Operation{Extensions: map[string]any{authz.XPermission: authz.PermPublic}})
	noID.Set("/x", item)
	_, err = authz.NewOperationGuard(&openapi3.T{Paths: noID}, p)
	require.ErrorContains(t, err, "operationId")
}

func TestMFARolesAreBaseRoles(t *testing.T) {
	t.Parallel()
	for _, r := range authz.MFARoles() {
		assert.Contains(t, authz.AllRoles(), r)
	}
}

package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/identity/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/parties/app"
	"github.com/marvinrepremarca/seguros/internal/modules/parties/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

var errStoreReached = errors.New("no debió llegar a la base de datos")

type unreachableStore struct{ app.Store }

func (unreachableStore) WithinTenant(context.Context, func(context.Context, app.UnitOfWork) error) error {
	return errStoreReached
}

type agents map[uuid.UUID]bool

func (a agents) Agents(context.Context) ([]contracts.User, error) { return nil, nil }

func (a agents) Agent(_ context.Context, id uuid.UUID) (contracts.User, bool, error) {
	active, ok := a[id]
	return contracts.User{ID: id, Active: active}, ok, nil
}

func as(t *testing.T, roles ...authz.Role) context.Context {
	t.Helper()
	policy, err := authz.DefaultPolicy()
	require.NoError(t, err)
	return authz.WithPrincipal(context.Background(), authz.NewPrincipal(kernel.UserID(uuid.New()), kernel.TenantID(uuid.New()), roles, policy))
}

func TestAssignResponsibleValidatesBeforeTheStore(t *testing.T) {
	t.Parallel()
	active, gone := uuid.New(), uuid.New()
	h := &app.AssignResponsibleHandler{Deps: app.Deps{Store: unreachableStore{}, Agents: agents{active: true, gone: false}}}
	cmd := func(agent uuid.UUID, reason string) app.AssignResponsibleCommand {
		return app.AssignResponsibleCommand{ID: domain.PartyID(uuid.New()), Version: 1, AgentID: agent, Reason: reason}
	}
	manager := as(t, authz.RoleManager)

	_, err := h.Handle(context.Background(), cmd(active, kernel.TransferRoleChange))
	require.ErrorIs(t, err, authz.ErrUnauthenticated)
	_, err = h.Handle(as(t, authz.RoleSalesAgent), cmd(active, kernel.TransferRoleChange))
	require.ErrorIs(t, err, authz.ErrForbidden)
	_, err = h.Handle(manager, cmd(active, "retiro"))
	require.ErrorIs(t, err, domain.ErrInvalidReason)
	_, err = h.Handle(manager, cmd(gone, kernel.TransferRoleChange))
	require.ErrorIs(t, err, domain.ErrInvalidAgent, "inactivo")
	_, err = h.Handle(manager, cmd(uuid.New(), kernel.TransferRoleChange))
	require.ErrorIs(t, err, domain.ErrInvalidAgent, "de otra agencia")
	_, err = h.Handle(manager, cmd(active, kernel.TransferRoleChange))
	assert.ErrorIs(t, err, errStoreReached)
}

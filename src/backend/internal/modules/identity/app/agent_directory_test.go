package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/identity/app"
	"github.com/marvinrepremarca/seguros/internal/modules/identity/contracts"
)

type localUsers []app.TenantUser

func (l localUsers) TenantUsers(context.Context) ([]app.TenantUser, error) { return l, nil }

type activeAgents struct {
	subjects map[string]bool
	err      error
}

func (a activeAgents) ActiveAgentSubjects(context.Context) (map[string]bool, error) {
	return a.subjects, a.err
}

func (l localUsers) TenantUser(_ context.Context, id uuid.UUID) (app.TenantUser, bool, error) {
	for _, u := range l {
		if u.ID == id {
			return u, true, nil
		}
	}
	return app.TenantUser{}, false, nil
}

func TestAgentLookup(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	d := &app.AgentDirectory{Users: localUsers{{ID: id, Subject: "kc-a", DisplayName: "Ana"}}, Active: activeAgents{subjects: map[string]bool{"kc-a": true}}}
	u, ok, err := d.Agent(context.Background(), id)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, u.Active)
	_, ok, err = d.Agent(context.Background(), uuid.New())
	require.NoError(t, err)
	assert.False(t, ok, "un usuario de otro tenant no aparece")
}

func TestAgentDirectory(t *testing.T) {
	t.Parallel()
	active, gone := uuid.New(), uuid.New()
	users := localUsers{{ID: active, Subject: "kc-a", DisplayName: "Ana"}, {ID: gone, Subject: "kc-b", DisplayName: "Beto"}}

	d := &app.AgentDirectory{Users: users, Active: activeAgents{subjects: map[string]bool{"kc-a": true, "kc-de-otro-tenant": true}}}
	got, err := d.Agents(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []contracts.User{{ID: active, DisplayName: "Ana", Active: true}, {ID: gone, DisplayName: "Beto"}}, got,
		"quien se fue sigue en la lista para traspasar su cartera; un activo de otro tenant no aparece")

	left := localUsers{{ID: active, Subject: "kc-a", DisplayName: "Ana", LeftOrganization: true}}
	d = &app.AgentDirectory{Users: left, Active: activeAgents{subjects: map[string]bool{"kc-a": true}}}
	got, err = d.Agents(context.Background())
	require.NoError(t, err)
	assert.False(t, got[0].Active, "habilitada en el realm, pero ya no es de esta agencia")

	d.Active = activeAgents{err: errors.New("keycloak caído")}
	_, err = d.Agents(context.Background())
	require.ErrorIs(t, err, contracts.ErrDirectoryUnavailable, "no se ofrece como activo a quien no se puede confirmar")
}

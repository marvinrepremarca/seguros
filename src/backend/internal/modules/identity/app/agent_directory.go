package app

import (
	"context"
	"errors"

	"github.com/marvinrepremarca/seguros/internal/modules/identity/contracts"
)

// AgentDirectory implementa contracts.Directory. Cruza los usuarios del tenant con los activos del
// proveedor: el aislamiento lo da RLS, porque leer la organización de un usuario en Keycloak exigiría
// manage-realm, un permiso de escritura que la API no debe tener.
type AgentDirectory struct {
	Users  TenantUsers
	Active ActiveAgents
}

var _ contracts.Directory = (*AgentDirectory)(nil)

// Agents implementa contracts.Directory. Sin proveedor no se puede saber quién sigue activo: falla en
// vez de ofrecer como asesor a alguien que ya se fue.
func (d *AgentDirectory) Agents(ctx context.Context) ([]contracts.User, error) {
	users, err := d.Users.TenantUsers(ctx)
	if err != nil {
		return nil, err
	}
	active, err := d.Active.ActiveAgentSubjects(ctx)
	if err != nil {
		return nil, errors.Join(contracts.ErrDirectoryUnavailable, err)
	}
	out := make([]contracts.User, 0, len(users))
	for _, u := range users {
		out = append(out, contracts.User{ID: u.ID, DisplayName: u.DisplayName, Active: active[u.Subject]})
	}
	return out, nil
}

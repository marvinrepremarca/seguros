// Package authz implementa la autorización deny-by-default: catálogo de permisos, asignación por
// rol, el Principal autenticado en el contexto y Require(ctx, perm), que todo caso de uso invoca
// antes de tocar el dominio (rules/architecture.md §Aplicación).
package authz

import (
	"context"
	"fmt"
	"regexp"
	"slices"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Errores estables (KindUnauthorized → 401, KindForbidden → 403).
var (
	ErrUnauthenticated = kernel.NewError(kernel.KindUnauthorized, "authz.unauthenticated", "se requiere sesión")
	ErrForbidden       = kernel.NewError(kernel.KindForbidden, "authz.forbidden", "permiso insuficiente")
)

var permissionPattern = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9-]*\.[a-z][a-z0-9-]*$`) //nolint:gochecknoglobals // regexp inmutable

// Policy resuelve los permisos efectivos de un conjunto de roles.
type Policy struct {
	grants map[Role][]Permission
	known  map[Permission]bool
}

// NewPolicy valida la asignación (roles conocidos, permisos bien formados).
func NewPolicy(grants map[Role][]Permission) (*Policy, error) {
	p := &Policy{grants: map[Role][]Permission{}, known: map[Permission]bool{}}
	for role, perms := range grants {
		if !slices.Contains(AllRoles(), role) {
			return nil, fmt.Errorf("authz: rol desconocido %q", role)
		}
		for _, perm := range perms {
			if !permissionPattern.MatchString(string(perm)) {
				return nil, fmt.Errorf("authz: permiso mal formado %q", perm)
			}
			p.known[perm] = true
		}
		p.grants[role] = slices.Clone(perms)
	}
	return p, nil
}

// DefaultPolicy construye la política base (DefaultGrants).
func DefaultPolicy() (*Policy, error) { return NewPolicy(DefaultGrants()) }

// Known indica si el permiso está en el catálogo (asignado al menos a un rol).
func (p *Policy) Known(perm Permission) bool { return p.known[perm] }

// PermissionsFor devuelve los permisos efectivos (ordenados, sin duplicados) de los roles.
func (p *Policy) PermissionsFor(roles []Role) []Permission {
	var out []Permission
	for _, r := range roles {
		out = append(out, p.grants[r]...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// ParseRoles traduce los roles del proveedor de identidad descartando los desconocidos. Resultado
// ordenado y sin duplicados.
func ParseRoles(raw []string) []Role {
	var out []Role
	for _, s := range raw {
		if r := Role(s); slices.Contains(AllRoles(), r) {
			out = append(out, r)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Principal es el usuario autenticado de la petición.
type Principal struct {
	UserID      kernel.UserID
	TenantID    kernel.TenantID
	Roles       []Role
	permissions []Permission
}

// NewPrincipal calcula los permisos efectivos de los roles según la política.
func NewPrincipal(user kernel.UserID, tenant kernel.TenantID, roles []Role, policy *Policy) Principal {
	return Principal{UserID: user, TenantID: tenant, Roles: slices.Clone(roles), permissions: policy.PermissionsFor(roles)}
}

// Permissions devuelve una copia de los permisos efectivos.
func (p Principal) Permissions() []Permission { return slices.Clone(p.permissions) }

// Has indica si el principal tiene el permiso.
func (p Principal) Has(perm Permission) bool {
	_, ok := slices.BinarySearch(p.permissions, perm)
	return ok
}

// HasRole indica si el principal tiene el rol.
func (p Principal) HasRole(r Role) bool { return slices.Contains(p.Roles, r) }

type ctxKey struct{}

// WithPrincipal pone el principal en el contexto, junto con su tenant y usuario (reqctx), de modo
// que db.WithTenantTx y la auditoría usen siempre la identidad autenticada.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	ctx = reqctx.WithTenantID(ctx, p.TenantID)
	ctx = reqctx.WithUserID(ctx, p.UserID)
	return context.WithValue(ctx, ctxKey{}, p)
}

// PrincipalFrom devuelve el principal del contexto.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	if !ok || kernel.IsZeroID(p.UserID) || kernel.IsZeroID(p.TenantID) {
		return Principal{}, false
	}
	return p, true
}

// Require falla si no hay principal (ErrUnauthenticated) o si no tiene el permiso (ErrForbidden).
func Require(ctx context.Context, perm Permission) error {
	p, ok := PrincipalFrom(ctx)
	if !ok {
		return ErrUnauthenticated
	}
	if !p.Has(perm) {
		return ErrForbidden.WithMessage("falta %s", perm)
	}
	return nil
}

// RequireAuthenticated falla si no hay principal.
func RequireAuthenticated(ctx context.Context) error {
	if _, ok := PrincipalFrom(ctx); !ok {
		return ErrUnauthenticated
	}
	return nil
}

// Package app contiene los casos de uso de identity: completar el login OIDC (resolver el tenant,
// aplicar la política de MFA, aprovisionar el usuario y auditar).
package app

import (
	"context"

	"github.com/marvinrepremarca/seguros/internal/modules/identity/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// UserRepository persiste el agregado User dentro de una unidad de trabajo.
type UserRepository interface {
	// GetBySubject devuelve domain.ErrNotFound si no existe.
	GetBySubject(ctx context.Context, tenant kernel.TenantID, subject string) (*domain.User, error)
	// Insert devuelve domain.ErrConcurrentUpdate si otro login lo creó primero.
	Insert(ctx context.Context, u *domain.User) error
	// Update aplica concurrencia optimista (domain.ErrConcurrentUpdate).
	Update(ctx context.Context, u *domain.User) error
}

// UnitOfWork agrupa los repositorios y la auditoría de una transacción con RLS.
type UnitOfWork interface {
	Users() UserRepository
	Audit(ctx context.Context, e audit.Entry) error
}

// Store abre unidades de trabajo para el tenant del contexto.
type Store interface {
	WithinTenant(ctx context.Context, fn func(ctx context.Context, uow UnitOfWork) error) error
}

// VerifiedIdentity son los claims de un ID token ya validado por el proveedor OIDC (firma, iss,
// aud, exp y nonce). La produce adapters/oidc y la consume el BFF.
type VerifiedIdentity struct {
	Subject       string
	Email         string
	Name          string
	Roles         []string
	Organizations []string
	AuthMethods   []string
}

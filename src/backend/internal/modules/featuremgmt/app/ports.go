// Package app contiene los casos de uso de featuremgmt: consultar el estado de los flags de un
// tenant y cambiar overrides (TenantAdmin para funcionalidades editables; la plataforma para
// módulos) o switches globales, validando dependencias, auditando e invalidando cachés.
package app

import (
	"context"
	"time"

	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Override es un override persistido.
type Override struct {
	Enabled   bool
	Reason    string
	UpdatedAt time.Time
}

// OverrideRepository persiste overrides del tenant del contexto.
type OverrideRepository interface {
	List(ctx context.Context, tenant kernel.TenantID) (map[flags.Key]Override, error)
	Upsert(ctx context.Context, tenant kernel.TenantID, key flags.Key, enabled bool, reason string, now time.Time) error
	Delete(ctx context.Context, tenant kernel.TenantID, key flags.Key) error
}

// UnitOfWork agrupa repositorio, auditoría y outbox en una transacción con RLS.
type UnitOfWork interface {
	Overrides() OverrideRepository
	Audit(ctx context.Context, e audit.Entry) error
	// Publish escribe un evento de integración en el outbox de la misma transacción.
	Publish(ctx context.Context, topic string, payload any) error
}

// Store abre unidades de trabajo y gestiona los switches globales.
type Store interface {
	WithinTenant(ctx context.Context, fn func(ctx context.Context, uow UnitOfWork) error) error
	GlobalSwitches(ctx context.Context) (map[flags.Key]bool, error)
	// SetGlobalSwitch fija (enabled != nil) o elimina (nil) un switch global.
	SetGlobalSwitch(ctx context.Context, key flags.Key, enabled *bool, reason string, now time.Time) error
}

// Invalidator descarta las cachés de flags de un tenant (tenant nulo = globales) en todas las
// instancias.
type Invalidator interface {
	Invalidate(ctx context.Context, tenant kernel.TenantID) error
}

// FlagView es el estado de un flag para un tenant.
type FlagView struct {
	Definition flags.Definition
	Resolution flags.Resolution
	// Override es nil si el tenant no tiene override.
	Override *Override
}

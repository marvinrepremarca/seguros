package app

import (
	"context"
	"log/slog"

	tenancy "github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Actor indica quién cambia un flag.
type Actor int

// Actores.
const (
	// ActorTenantAdmin: vía API; exige featuremgmt.flag.update y solo funcionalidades editables.
	ActorTenantAdmin Actor = iota + 1
	// ActorPlatform: super-administrador vía cmd/platformctl (DECISIONS P2); gestiona módulos y add-ons.
	ActorPlatform
)

// Deps son las dependencias compartidas por los casos de uso de flags.
type Deps struct {
	reg         *flags.Registry
	store       Store
	tenants     tenancy.Directory
	invalidator Invalidator
	clock       kernel.Clock
	logger      *slog.Logger
}

// NewDeps construye las dependencias.
func NewDeps(reg *flags.Registry, store Store, tenants tenancy.Directory, inv Invalidator, clock kernel.Clock, logger *slog.Logger) Deps {
	return Deps{reg: reg, store: store, tenants: tenants, invalidator: inv, clock: clock, logger: logger}
}

// invalidate descarta cachés tras el commit. Si falla, el cambio ya está guardado: las cachés
// vencen solas (L1 30 s, L2 1 h) y el evento FeatureToggled del outbox reintenta la invalidación.
func (s Deps) invalidate(ctx context.Context, tenant kernel.TenantID) {
	if err := s.invalidator.Invalidate(ctx, tenant); err != nil {
		s.logger.ErrorContext(ctx, "invalidando caché de flags", "error", err)
	}
}

func enabledMap(o map[flags.Key]Override) map[flags.Key]bool {
	out := make(map[flags.Key]bool, len(o))
	for k, v := range o {
		out[k] = v.Enabled
	}
	return out
}

func containsPlan(plans []string, plan string) bool {
	for _, p := range plans {
		if p == plan {
			return true
		}
	}
	return false
}

func actorName(a Actor) string {
	if a == ActorPlatform {
		return "platform"
	}
	return "tenant_admin"
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

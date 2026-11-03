// Package tenancy es el módulo núcleo de tenants (agencias): plan, estado y vínculo con la
// Organization de Keycloak. No es apagable.
package tenancy

import (
	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/app"
	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
)

// Module expone el directorio (contracts) y las operaciones de plataforma.
type Module struct {
	Directory    contracts.Directory
	Provision    *app.ProvisionTenantHandler
	ChangeStatus *app.ChangeTenantStatusHandler
	ChangePlan   *app.ChangeTenantPlanHandler
}

// New compone el módulo.
func New(database *db.DB, clock kernel.Clock) *Module {
	store := postgres.NewStore(database, audit.NewRecorder(clock), outbox.NewWriter(clock))
	return &Module{
		Directory:    app.NewDirectory(store),
		Provision:    app.NewProvisionTenantHandler(store, clock),
		ChangeStatus: app.NewChangeTenantStatusHandler(store, clock),
		ChangePlan:   app.NewChangeTenantPlanHandler(store, clock),
	}
}

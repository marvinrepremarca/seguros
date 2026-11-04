// Package parties es el módulo núcleo de terceros (clientes): personas y organizaciones, documento
// cifrado, contactos y autorizaciones de tratamiento de datos. No es apagable.
package parties

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	identity "github.com/marvinrepremarca/seguros/internal/modules/identity/contracts"
	partiesevents "github.com/marvinrepremarca/seguros/internal/modules/parties/adapters/events"
	partieshttp "github.com/marvinrepremarca/seguros/internal/modules/parties/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/parties/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/parties/app"
	"github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
)

// Consumers devuelve los consumidores de eventos del módulo (los registra cmd/worker).
func Consumers(recorder *audit.Recorder) []outbox.Consumer {
	return []outbox.Consumer{partiesevents.PolicyScope(), partiesevents.PortfolioTransfer(recorder)}
}

// Deps son las dependencias del módulo.
type Deps struct {
	DB     *db.DB
	Audit  *audit.Recorder
	Cipher app.Cipher
	Agents identity.Directory
	Policy *authz.Policy
	Clock  kernel.Clock
	Logger *slog.Logger
}

// Module expone las rutas y el directorio para otros módulos.
type Module struct {
	Directory contracts.Directory
	server    *partieshttp.Server
}

// New compone el módulo.
func New(d Deps) *Module {
	deps := app.Deps{Store: postgres.NewStore(d.DB, d.Audit), Cipher: d.Cipher, Agents: d.Agents, Clock: d.Clock}
	return &Module{
		Directory: &app.Directory{Deps: deps},
		server: partieshttp.NewServer(partieshttp.Handlers{
			Create: &app.CreatePartyHandler{Deps: deps}, Update: &app.UpdatePartyHandler{Deps: deps},
			Get: &app.GetPartyHandler{Deps: deps}, List: &app.ListPartiesHandler{Deps: deps},
			Search: &app.SearchByDocumentHandler{Deps: deps}, Reveal: &app.RevealDocumentHandler{Deps: deps},
			Assign: &app.AssignResponsibleHandler{Deps: deps}, Register: &app.RegisterConsentHandler{Deps: deps}, Consents: &app.ListConsentsHandler{Deps: deps},
		}, d.Policy, d.Logger),
	}
}

// Mount registra las rutas del módulo.
func (m *Module) Mount(r chi.Router) error { return m.server.Mount(r) }

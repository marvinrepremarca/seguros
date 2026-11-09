// Package catalog es el módulo núcleo del catálogo de la agencia: aseguradoras, ramos, productos,
// esquemas de riesgo y acuerdos de comisión. No es apagable.
package catalog

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	cataloghttp "github.com/marvinrepremarca/seguros/internal/modules/catalog/adapters/http"
	"github.com/marvinrepremarca/seguros/internal/modules/catalog/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/catalog/adapters/riskschema"
	"github.com/marvinrepremarca/seguros/internal/modules/catalog/app"
	"github.com/marvinrepremarca/seguros/internal/modules/catalog/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Deps son las dependencias del módulo.
type Deps struct {
	DB     *db.DB
	Audit  *audit.Recorder
	Policy *authz.Policy
	Clock  kernel.Clock
	Logger *slog.Logger
}

// Module expone las rutas y las consultas para otros módulos.
type Module struct {
	Directory contracts.Directory
	// LineSchemas resuelve el esquema de riesgo de un ramo.
	LineSchemas contracts.LineSchemas
	Schemas     contracts.RiskSchemas
	server      *cataloghttp.Server
}

// New compone el módulo; falla si un esquema de riesgo embebido es inválido.
func New(d Deps) (*Module, error) {
	schemas, err := riskschema.Load()
	if err != nil {
		return nil, err
	}
	deps := app.Deps{Store: postgres.NewStore(d.DB, d.Audit, d.Clock), Schemas: schemas}
	return &Module{
		Directory:   &app.Directory{Deps: deps},
		LineSchemas: &app.Directory{Deps: deps},
		Schemas:     schemas,
		server:      cataloghttp.NewServer(&app.Service{Deps: deps}, schemas, d.Policy, d.Logger),
	}, nil
}

// Mount registra las rutas del módulo.
func (m *Module) Mount(r chi.Router) error { return m.server.Mount(r) }

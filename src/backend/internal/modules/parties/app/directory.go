package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
)

// Directory implementa contracts.Directory para otros módulos.
type Directory struct{ Deps }

var _ contracts.Directory = (*Directory)(nil)

// Summaries implementa contracts.Directory. Sin permiso propio: lo invoca un caso de uso que ya
// autorizó su operación, y solo expone el nombre (nunca el documento).
func (d *Directory) Summaries(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]contracts.PartySummary, error) {
	var out map[uuid.UUID]contracts.PartySummary
	err := d.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		out, err = uow.Parties().Summaries(ctx, ids)
		return err
	})
	return out, err
}

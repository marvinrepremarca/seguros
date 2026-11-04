// Package contracts es la única superficie pública consultable del módulo identity.
package contracts

import (
	"context"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// ErrDirectoryUnavailable indica que no se pudo consultar quién sigue activo; se puede reintentar.
var ErrDirectoryUnavailable = kernel.NewError(kernel.KindUnavailable, "identity.directory.unavailable", "no se pudo consultar el directorio de usuarios")

// User es un usuario de la agencia. El nombre es dato Confidencial.
type User struct {
	ID          uuid.UUID
	DisplayName string
	// Active indica que sigue habilitado y con rol de asesor. Quien ya no lo está sigue en la lista
	// porque puede tener cartera por traspasar.
	Active bool
}

// Directory consulta los usuarios del tenant del contexto. Solo aparecen quienes ya iniciaron sesión
// al menos una vez.
type Directory interface {
	Agents(ctx context.Context) ([]User, error)
}

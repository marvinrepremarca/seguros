// Package contracts es la única superficie pública consultable del módulo identity.
package contracts

import (
	"context"

	"github.com/google/uuid"
)

// User es la vista mínima de un usuario de la agencia. El nombre es dato Confidencial.
type User struct {
	ID          uuid.UUID
	DisplayName string
}

// Directory consulta los usuarios del tenant del contexto. Los roles viven en Keycloak: aquí solo
// están quienes ya iniciaron sesión al menos una vez.
type Directory interface {
	Users(ctx context.Context) ([]User, error)
}

package kernel

import (
	"fmt"

	"github.com/google/uuid"
)

// UUID es la restricción de los IDs tipados: cualquier tipo definido sobre uuid.UUID
// (ej. `type PolicyID uuid.UUID`).
type UUID interface {
	~[16]byte
}

// ErrInvalidID indica un identificador mal formado o que no es UUIDv7.
var ErrInvalidID = NewError(KindValidation, "kernel.id.invalid", "identificador inválido")

// NewID genera un ID tipado UUIDv7 (ordenable por tiempo, no secuencial).
func NewID[T UUID]() (T, error) {
	u, err := uuid.NewV7()
	if err != nil {
		return T{}, fmt.Errorf("generando UUIDv7: %w", err)
	}
	return T(u), nil
}

// ParseID interpreta un ID tipado. Solo acepta UUIDv7 en formato canónico, para no aceptar IDs
// que el sistema no haya podido generar.
func ParseID[T UUID](s string) (T, error) {
	u, err := uuid.Parse(s)
	if err != nil || len(s) != 36 || u.Version() != 7 {
		return T{}, ErrInvalidID
	}
	return T(u), nil
}

// IDString devuelve la forma canónica de un ID tipado.
func IDString[T UUID](id T) string { return uuid.UUID(id).String() }

// IsZeroID indica si el ID es el UUID nulo.
func IsZeroID[T UUID](id T) bool { return id == T{} }

// TenantID identifica una agencia (tenant). Nunca se recibe del cliente: sale de la sesión.
type TenantID uuid.UUID

// String devuelve la forma canónica.
func (id TenantID) String() string { return IDString(id) }

// UserID identifica un usuario autenticado.
type UserID uuid.UUID

// String devuelve la forma canónica.
func (id UserID) String() string { return IDString(id) }

package kernel

import (
	"encoding/base64"

	"github.com/google/uuid"
)

// ErrInvalidCursor indica un cursor de paginación que el sistema no generó.
var ErrInvalidCursor = NewError(KindValidation, "kernel.cursor.invalid", "cursor de paginación inválido")

// Límites de paginación (rules/architecture.md §API).
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// EncodeCursor crea el cursor opaco de una paginación keyset por ID.
func EncodeCursor(id uuid.UUID) string { return base64.RawURLEncoding.EncodeToString(id[:]) }

// DecodeCursor interpreta un cursor. El cursor vacío devuelve uuid.NullUUID sin valor (primera página).
func DecodeCursor(s string) (uuid.NullUUID, error) {
	if s == "" {
		return uuid.NullUUID{}, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return uuid.NullUUID{}, ErrInvalidCursor
	}
	id, err := uuid.FromBytes(b)
	if err != nil {
		return uuid.NullUUID{}, ErrInvalidCursor
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

// PageSize aplica el tamaño por defecto y el máximo.
func PageSize(requested int32) int32 {
	switch {
	case requested <= 0:
		return DefaultPageSize
	case requested > MaxPageSize:
		return MaxPageSize
	default:
		return requested
	}
}

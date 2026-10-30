// Package reqctx transporta en el context.Context los datos de la petición autenticada: tenant,
// usuario y request-id. Solo los middlewares de plataforma los fijan; nunca provienen del cliente
// sin validar (CLAUDE.md regla 2).
package reqctx

import (
	"context"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

type ctxKey int

const (
	tenantKey ctxKey = iota + 1
	userKey
	requestIDKey
)

// WithTenantID devuelve un contexto con el tenant autenticado.
func WithTenantID(ctx context.Context, id kernel.TenantID) context.Context {
	return context.WithValue(ctx, tenantKey, id)
}

// TenantID devuelve el tenant del contexto. ok=false si no hay tenant o es el UUID nulo: quien lo
// necesite debe fallar, nunca usar un valor por defecto.
func TenantID(ctx context.Context) (kernel.TenantID, bool) {
	id, ok := ctx.Value(tenantKey).(kernel.TenantID)
	if !ok || kernel.IsZeroID(id) {
		return kernel.TenantID{}, false
	}
	return id, true
}

// WithUserID devuelve un contexto con el usuario autenticado.
func WithUserID(ctx context.Context, id kernel.UserID) context.Context {
	return context.WithValue(ctx, userKey, id)
}

// UserID devuelve el usuario del contexto.
func UserID(ctx context.Context) (kernel.UserID, bool) {
	id, ok := ctx.Value(userKey).(kernel.UserID)
	if !ok || kernel.IsZeroID(id) {
		return kernel.UserID{}, false
	}
	return id, true
}

// WithRequestID devuelve un contexto con el identificador de la petición.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID devuelve el identificador de la petición ("" si no hay).
func RequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

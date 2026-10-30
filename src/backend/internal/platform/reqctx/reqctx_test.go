package reqctx_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

func TestTenantID(t *testing.T) {
	t.Parallel()
	_, ok := reqctx.TenantID(context.Background())
	assert.False(t, ok, "sin tenant")

	_, ok = reqctx.TenantID(reqctx.WithTenantID(context.Background(), kernel.TenantID{}))
	assert.False(t, ok, "UUID nulo no es un tenant")

	id := kernel.TenantID(uuid.Must(uuid.NewV7()))
	got, ok := reqctx.TenantID(reqctx.WithTenantID(context.Background(), id))
	assert.True(t, ok)
	assert.Equal(t, id, got)
}

func TestUserID(t *testing.T) {
	t.Parallel()
	_, ok := reqctx.UserID(context.Background())
	assert.False(t, ok)
	_, ok = reqctx.UserID(reqctx.WithUserID(context.Background(), kernel.UserID{}))
	assert.False(t, ok)

	id := kernel.UserID(uuid.Must(uuid.NewV7()))
	got, ok := reqctx.UserID(reqctx.WithUserID(context.Background(), id))
	assert.True(t, ok)
	assert.Equal(t, id, got)
}

func TestRequestID(t *testing.T) {
	t.Parallel()
	assert.Empty(t, reqctx.RequestID(context.Background()))
	assert.Equal(t, "abc", reqctx.RequestID(reqctx.WithRequestID(context.Background(), "abc")))
}

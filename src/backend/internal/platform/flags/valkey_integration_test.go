//go:build integration

package flags_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/session"
	"github.com/marvinrepremarca/seguros/test/valkeytest"
)

func TestValkeyCacheAndCrossInstanceInvalidation(t *testing.T) {
	ctx := context.Background()
	vk, err := valkeytest.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, vk.Terminate()) })
	client, err := session.NewValkeyClient(vk.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	cache := flags.NewValkeyCache(client)
	_, ok, err := cache.Get(ctx, "t:x")
	require.NoError(t, err)
	assert.False(t, ok)
	require.NoError(t, cache.Set(ctx, "t:x", map[flags.Key]bool{flags.ClaimsModule: true}))
	v, ok, err := cache.Get(ctx, "t:x")
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, v[flags.ClaimsModule])

	// Dos "instancias" comparten L2 y se invalidan por pub/sub.
	store := &fakeStore{overrides: map[kernel.TenantID]map[flags.Key]bool{}}
	logger := slog.New(slog.DiscardHandler)
	reg := testReg(t)
	clock := kernel.NewFakeClock(time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC))
	inst1 := flags.NewEvaluator(reg, store, cache, clock, logger)
	inst2 := flags.NewEvaluator(reg, store, cache, clock, logger)

	subCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- flags.Subscribe(subCtx, client, inst2, logger) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })

	// Espera a que la suscripción esté activa para no perder el mensaje.
	require.Eventually(t, func() bool {
		m, err := client.Do(ctx, client.B().PubsubNumsub().Channel("flags:invalidate").Build()).AsIntMap()
		return err == nil && m["flags:invalidate"] >= 1
	}, 5*time.Second, 20*time.Millisecond)

	tenant := newTenant()
	tctx := tenantCtx(tenant, "empresarial")
	assert.False(t, inst1.IsEnabled(tctx, "module.billing"))
	assert.False(t, inst2.IsEnabled(tctx, "module.billing"), "inst2 lee la L2 y la cachea en su L1")

	store.set(tenant, "module.billing", true)
	require.NoError(t, flags.NewInvalidator(inst1, client).Invalidate(ctx, tenant))
	assert.True(t, inst1.IsEnabled(tctx, "module.billing"))
	require.Eventually(t, func() bool { return inst2.IsEnabled(tctx, "module.billing") }, 5*time.Second, 20*time.Millisecond,
		"inst2 recibe la invalidación sin esperar al TTL de 30 s")
}

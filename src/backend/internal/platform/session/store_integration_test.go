//go:build integration

package session_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/session"
	"github.com/marvinrepremarca/seguros/test/valkeytest"
)

func TestValkeyStore(t *testing.T) {
	ctx := context.Background()
	vk, err := valkeytest.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, vk.Terminate()) })
	client, err := session.NewValkeyClient(vk.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	now := time.Now() //nolint:forbidigo // TTL real de Valkey
	store := session.NewValkeyStore(client, "bff:sess:", kernel.NewFakeClock(now))

	_, found, err := store.FindCtx(ctx, "tok")
	require.NoError(t, err)
	assert.False(t, found)

	require.NoError(t, store.CommitCtx(ctx, "tok", []byte("datos"), now.Add(time.Minute)))
	b, found, err := store.FindCtx(ctx, "tok")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []byte("datos"), b)

	// La clave en Valkey es el hash del token, nunca el token.
	sum := sha256.Sum256([]byte("tok"))
	exists, err := client.Do(ctx, client.B().Exists().Key("bff:sess:"+hex.EncodeToString(sum[:])).Build()).AsInt64()
	require.NoError(t, err)
	assert.Equal(t, int64(1), exists)
	exists, err = client.Do(ctx, client.B().Exists().Key("bff:sess:tok").Build()).AsInt64()
	require.NoError(t, err)
	assert.Zero(t, exists)

	ttl, err := client.Do(ctx, client.B().Pttl().Key("bff:sess:"+hex.EncodeToString(sum[:])).Build()).AsInt64()
	require.NoError(t, err)
	assert.InDelta(t, time.Minute.Milliseconds(), ttl, 5000)

	require.NoError(t, store.DeleteCtx(ctx, "tok"))
	_, found, err = store.FindCtx(ctx, "tok")
	require.NoError(t, err)
	assert.False(t, found)

	require.NoError(t, store.CommitCtx(ctx, "old", []byte("x"), now.Add(-time.Second)), "expiración pasada = borrar")
	_, found, err = store.Find("old")
	require.NoError(t, err)
	assert.False(t, found)

	_, err = session.NewValkeyClient("")
	require.ErrorIs(t, err, session.ErrNoValkeyURL)
	_, err = session.NewValkeyClient("::no-es-url")
	require.Error(t, err)
}

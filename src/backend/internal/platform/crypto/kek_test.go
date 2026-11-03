package crypto_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/config"
	"github.com/marvinrepremarca/seguros/internal/platform/crypto"
	"github.com/marvinrepremarca/seguros/test/oidctest"
)

func material() string { return oidctest.RandomString() + oidctest.RandomString() } // ≥ 32 caracteres aleatorios

func TestLocalKEK(t *testing.T) {
	t.Parallel()
	m1, m2 := material(), material()
	k1, err := crypto.NewLocalKEK(map[string]string{"v1": m1}, "v1")
	require.NoError(t, err)
	dek := bytes.Repeat([]byte{7}, 32)

	wrapped, ref, err := k1.Wrap(context.Background(), dek)
	require.NoError(t, err)
	assert.Equal(t, "local:v1", ref)
	assert.NotContains(t, string(wrapped), string(dek))
	got, err := k1.Unwrap(context.Background(), wrapped, ref)
	require.NoError(t, err)
	assert.Equal(t, dek, got)

	// Rotación de KEK: v2 activa, v1 sigue desenvolviendo.
	k2, err := crypto.NewLocalKEK(map[string]string{"v1": m1, "v2": m2}, "v2")
	require.NoError(t, err)
	got, err = k2.Unwrap(context.Background(), wrapped, "local:v1")
	require.NoError(t, err)
	assert.Equal(t, dek, got)
	_, ref2, err := k2.Wrap(context.Background(), dek)
	require.NoError(t, err)
	assert.Equal(t, "local:v2", ref2)

	_, err = k1.Unwrap(context.Background(), wrapped, "local:v9")
	require.ErrorIs(t, err, crypto.ErrUnknownKEK)
	tampered := bytes.Clone(wrapped)
	tampered[len(tampered)-1] ^= 1
	_, err = k1.Unwrap(context.Background(), tampered, ref)
	require.ErrorIs(t, err, crypto.ErrDecrypt)
	_, err = k1.Unwrap(context.Background(), []byte("corto"), ref)
	require.ErrorIs(t, err, crypto.ErrDecrypt)

	_, err = crypto.NewLocalKEK(map[string]string{"v1": "corta"}, "v1")
	require.Error(t, err)
	_, err = crypto.NewLocalKEK(map[string]string{"v1": m1}, "v2")
	require.Error(t, err)
}

// fakeBao imita el motor Transit de OpenBao (cifrado reversible para la prueba).
func fakeBao(t *testing.T, token string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != token {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		switch r.URL.Path {
		case "/v1/transit/encrypt/seguros":
			writeData(t, w, map[string]string{"ciphertext": "vault:v1:" + body["plaintext"]})
		case "/v1/transit/decrypt/seguros":
			writeData(t, w, map[string]string{"plaintext": strings.TrimPrefix(body["ciphertext"], "vault:v1:")})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func writeData(t *testing.T, w http.ResponseWriter, data map[string]string) {
	t.Helper()
	require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": data}))
}

func TestOpenBaoTransit(t *testing.T) {
	t.Parallel()
	token := oidctest.RandomString()
	srv := fakeBao(t, token)
	t.Cleanup(srv.Close)
	o := crypto.NewOpenBaoTransit(srv.URL, token, "seguros", nil)
	dek := bytes.Repeat([]byte{9}, 32)

	wrapped, ref, err := o.Wrap(context.Background(), dek)
	require.NoError(t, err)
	assert.Equal(t, "openbao:seguros", ref)
	assert.True(t, strings.HasPrefix(string(wrapped), "vault:v1:"))
	got, err := o.Unwrap(context.Background(), wrapped, ref)
	require.NoError(t, err)
	assert.Equal(t, dek, got)
	assert.Equal(t, base64.StdEncoding.EncodeToString(dek), strings.TrimPrefix(string(wrapped), "vault:v1:"))

	_, err = o.Unwrap(context.Background(), wrapped, "local:v1")
	require.ErrorIs(t, err, crypto.ErrUnknownKEK)
	bad := crypto.NewOpenBaoTransit(srv.URL, "token-invalido", "seguros", nil)
	_, _, err = bad.Wrap(context.Background(), dek)
	require.ErrorContains(t, err, "403")
	assert.NotContains(t, err.Error(), token)
}

func TestNewWrapperAndConfig(t *testing.T) {
	t.Parallel()
	m := material()
	lookup := func(kv map[string]string) config.LookupFunc {
		return func(k string) (string, bool) { v, ok := kv[k]; return v, ok }
	}
	cfg, err := config.LoadCrypto(lookup(map[string]string{
		"SEGUROS_CRYPTO_LOCAL_KEKS": "v1:" + m + ",v2:" + m + "x", "SEGUROS_CRYPTO_LOCAL_ACTIVE": "v2",
		"SEGUROS_CRYPTO_INDEX_KEY": material(),
	}), config.EnvDevelopment)
	require.NoError(t, err)
	assert.Len(t, cfg.LocalKEKs, 2)
	w, err := crypto.NewWrapper(cfg)
	require.NoError(t, err)
	_, ref, err := w.Wrap(context.Background(), bytes.Repeat([]byte{1}, 32))
	require.NoError(t, err)
	assert.Equal(t, "local:v2", ref)
	assert.NotContains(t, strings.Join([]string{cfg.IndexKey.String(), cfg.LocalKEKs["v1"].String()}, ""), m, "secretos redactados")

	for name, tc := range map[string]struct {
		kv  map[string]string
		env config.Env
		key string
	}{
		"local en producción":  {map[string]string{"SEGUROS_CRYPTO_LOCAL_KEKS": "v1:" + m, "SEGUROS_CRYPTO_INDEX_KEY": m}, config.EnvProduction, "SEGUROS_CRYPTO_PROVIDER"},
		"sin índice":           {map[string]string{"SEGUROS_CRYPTO_LOCAL_KEKS": "v1:" + m}, config.EnvDevelopment, "SEGUROS_CRYPTO_INDEX_KEY"},
		"índice corto":         {map[string]string{"SEGUROS_CRYPTO_LOCAL_KEKS": "v1:" + m, "SEGUROS_CRYPTO_INDEX_KEY": "corta"}, config.EnvDevelopment, "SEGUROS_CRYPTO_INDEX_KEY"},
		"kek corta":            {map[string]string{"SEGUROS_CRYPTO_LOCAL_KEKS": "v1:corta", "SEGUROS_CRYPTO_INDEX_KEY": m}, config.EnvDevelopment, "SEGUROS_CRYPTO_LOCAL_KEKS"},
		"activa inexistente":   {map[string]string{"SEGUROS_CRYPTO_LOCAL_KEKS": "v1:" + m, "SEGUROS_CRYPTO_LOCAL_ACTIVE": "v3", "SEGUROS_CRYPTO_INDEX_KEY": m}, config.EnvDevelopment, "SEGUROS_CRYPTO_LOCAL_ACTIVE"},
		"openbao sin token":    {map[string]string{"SEGUROS_CRYPTO_PROVIDER": "openbao", "SEGUROS_OPENBAO_ADDR": "https://bao:8200", "SEGUROS_CRYPTO_INDEX_KEY": m}, config.EnvProduction, "SEGUROS_OPENBAO_TOKEN"},
		"openbao http en prod": {map[string]string{"SEGUROS_CRYPTO_PROVIDER": "openbao", "SEGUROS_OPENBAO_ADDR": "http://bao:8200", "SEGUROS_OPENBAO_TOKEN": "t", "SEGUROS_CRYPTO_INDEX_KEY": m}, config.EnvProduction, "https"},
		"proveedor raro":       {map[string]string{"SEGUROS_CRYPTO_PROVIDER": "kms", "SEGUROS_CRYPTO_INDEX_KEY": m}, config.EnvDevelopment, "SEGUROS_CRYPTO_PROVIDER"},
	} {
		_, err := config.LoadCrypto(lookup(tc.kv), tc.env)
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), tc.key, name)
		assert.NotContains(t, err.Error(), m, name)
	}
}

//go:build integration

package crypto_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/crypto"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
	"github.com/marvinrepremarca/seguros/test/pgtest"
)

var database *db.DB //nolint:gochecknoglobals // compartido por la suite (TestMain)

func TestMain(m *testing.M) { os.Exit(runMain(m)) }

func runMain(m *testing.M) int {
	ctx := context.Background()
	env, err := pgtest.Start(ctx)
	if err != nil {
		slog.Error("iniciando PostgreSQL", "error", err)
		return 1
	}
	defer env.Terminate() //nolint:errcheck // limpieza al final de la suite
	if err := env.MigrateAll(ctx); err != nil {
		slog.Error("migrando", "error", err)
		return 1
	}
	database, err = env.OpenApp(ctx)
	if err != nil {
		slog.Error("abriendo BD", "error", err)
		return 1
	}
	defer database.Close()
	return m.Run()
}

const field = "parties.person.document_number"

func newService(t *testing.T, kekMaterial, indexMaterial string) *crypto.Service {
	t.Helper()
	kek, err := crypto.NewLocalKEK(map[string]string{"v1": kekMaterial}, "v1")
	require.NoError(t, err)
	s, err := crypto.NewService(database, kek, indexMaterial, kernel.NewFakeClock(time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	return s
}

func tenant() context.Context {
	return reqctx.WithTenantID(context.Background(), kernel.TenantID(uuid.Must(uuid.NewV7())))
}

func TestEncryptDecryptAndBindings(t *testing.T) {
	t.Parallel()
	s := newService(t, material(), material())
	a, b := tenant(), tenant()
	secret := []byte("1032456789")

	c1, err := s.Encrypt(a, field, secret)
	require.NoError(t, err)
	c2, err := s.Encrypt(a, field, secret)
	require.NoError(t, err)
	assert.NotEqual(t, c1, c2, "nonce aleatorio: mismo valor, ciphertext distinto")
	assert.False(t, bytes.Contains(c1, secret))

	got, err := s.Decrypt(a, field, c1)
	require.NoError(t, err)
	assert.Equal(t, secret, got)

	_, err = s.Decrypt(b, field, c1)
	require.ErrorIs(t, err, crypto.ErrDecrypt, "no se descifra en otro tenant")
	_, err = s.Decrypt(a, "parties.person.health_notes", c1)
	require.ErrorIs(t, err, crypto.ErrDecrypt, "no se descifra como otro campo (AAD)")
	tampered := bytes.Clone(c1)
	tampered[len(tampered)-1] ^= 1
	_, err = s.Decrypt(a, field, tampered)
	require.ErrorIs(t, err, crypto.ErrDecrypt)
	_, err = s.Decrypt(a, field, []byte{0x02, 0, 0, 0, 1})
	require.ErrorIs(t, err, crypto.ErrDecrypt, "formato desconocido")
	_, err = s.Encrypt(context.Background(), field, secret)
	require.ErrorIs(t, err, crypto.ErrNoTenant)
	_, err = s.Encrypt(a, "documento", secret)
	require.ErrorIs(t, err, crypto.ErrBadField)
}

func TestDecryptWithFreshInstanceUsesStoredWrappedKey(t *testing.T) {
	t.Parallel()
	kek, idx := material(), material()
	a := tenant()
	c, err := newService(t, kek, idx).Encrypt(a, field, []byte("dato"))
	require.NoError(t, err)

	got, err := newService(t, kek, idx).Decrypt(a, field, c)
	require.NoError(t, err, "otra instancia con la misma KEK desenvuelve la DEK guardada")
	assert.Equal(t, []byte("dato"), got)

	_, err = newService(t, material(), idx).Decrypt(a, field, c)
	require.Error(t, err, "sin la KEK la DEK guardada no sirve")
}

func TestRotateDataKey(t *testing.T) {
	t.Parallel()
	s := newService(t, material(), material())
	a := tenant()
	old, err := s.Encrypt(a, field, []byte("antes"))
	require.NoError(t, err)
	assert.Equal(t, []byte{0x01, 0, 0, 0, 1}, old[:5], "versión 1 en la cabecera")

	v, err := s.RotateDataKey(a)
	require.NoError(t, err)
	assert.Equal(t, uint32(2), v)
	fresh, err := s.Encrypt(a, field, []byte("después"))
	require.NoError(t, err)
	assert.Equal(t, []byte{0x01, 0, 0, 0, 2}, fresh[:5])

	got, err := s.Decrypt(a, field, old)
	require.NoError(t, err, "la DEK retirada sigue descifrando")
	assert.Equal(t, []byte("antes"), got)

	var active, total int
	require.NoError(t, database.WithTenantTx(a, func(tx pgx.Tx) error {
		return tx.QueryRow(a, `SELECT count(*) FILTER (WHERE status = 'active'), count(*) FROM crypto.data_keys`).Scan(&active, &total)
	}))
	assert.Equal(t, 1, active)
	assert.Equal(t, 2, total)
}

func TestBlindIndex(t *testing.T) {
	t.Parallel()
	idx := material()
	s := newService(t, material(), idx)
	a, b := tenant(), tenant()
	i1, err := s.BlindIndex(a, field, "1032456789")
	require.NoError(t, err)
	i2, err := s.BlindIndex(a, field, "1032456789")
	require.NoError(t, err)
	assert.Equal(t, i1, i2, "determinista: permite buscar por igualdad")
	assert.Len(t, i1, 32)

	other, err := s.BlindIndex(b, field, "1032456789")
	require.NoError(t, err)
	assert.NotEqual(t, i1, other, "otro tenant, otro índice")
	otherField, err := s.BlindIndex(a, "parties.organization.nit", "1032456789")
	require.NoError(t, err)
	assert.NotEqual(t, i1, otherField, "otro campo, otro índice")
	otherKey, err := newService(t, material(), material()).BlindIndex(a, field, "1032456789")
	require.NoError(t, err)
	assert.NotEqual(t, i1, otherKey, "depende de la llave de índice")
}

func TestDataKeysAreTenantIsolated(t *testing.T) {
	t.Parallel()
	s := newService(t, material(), material())
	a, b := tenant(), tenant()
	_, err := s.Encrypt(a, field, []byte("x"))
	require.NoError(t, err)
	var n int
	require.NoError(t, database.WithTenantTx(b, func(tx pgx.Tx) error {
		return tx.QueryRow(b, `SELECT count(*) FROM crypto.data_keys`).Scan(&n)
	}))
	assert.Zero(t, n)
	err = database.WithTenantTx(a, func(tx pgx.Tx) error {
		_, err := tx.Exec(a, `DELETE FROM crypto.data_keys`)
		return err
	})
	require.Error(t, err, "el rol de la app no borra DEKs")
}

func TestConcurrentFirstUseCreatesASingleKey(t *testing.T) {
	t.Parallel()
	kek, idx := material(), material()
	a := tenant()
	var wg sync.WaitGroup
	errs := make([]error, 6)
	cts := make([][]byte, 6)
	for i := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cts[i], errs[i] = newService(t, kek, idx).Encrypt(a, field, []byte("v"))
		}()
	}
	wg.Wait()
	for i := range 6 {
		require.NoError(t, errs[i])
	}
	var n int
	require.NoError(t, database.WithTenantTx(a, func(tx pgx.Tx) error {
		return tx.QueryRow(a, `SELECT count(*) FROM crypto.data_keys`).Scan(&n)
	}))
	assert.Equal(t, 1, n, "una sola DEK aunque varias instancias la pidan a la vez")
	s := newService(t, kek, idx)
	for _, c := range cts {
		got, err := s.Decrypt(a, field, c)
		require.NoError(t, err)
		assert.Equal(t, []byte("v"), got)
	}
}

// Package session guarda las sesiones del BFF (alexedwards/scs) en Valkey. Las claves son el
// SHA-256 del token de sesión, de modo que un volcado de Valkey no contiene tokens utilizables.
package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/valkey-io/valkey-go"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// ValkeyStore implementa scs.CtxStore sobre valkey-go.
type ValkeyStore struct {
	client valkey.Client
	prefix string
	clock  kernel.Clock
}

var _ scs.CtxStore = (*ValkeyStore)(nil)

// NewValkeyStore crea un store cuyas claves empiezan por prefix (ej. "bff:sess:").
func NewValkeyStore(client valkey.Client, prefix string, clock kernel.Clock) *ValkeyStore {
	return &ValkeyStore{client: client, prefix: prefix, clock: clock}
}

func (s *ValkeyStore) key(token string) string {
	sum := sha256.Sum256([]byte(token))
	return s.prefix + hex.EncodeToString(sum[:])
}

// FindCtx implementa scs.CtxStore.
func (s *ValkeyStore) FindCtx(ctx context.Context, token string) ([]byte, bool, error) {
	b, err := s.client.Do(ctx, s.client.B().Get().Key(s.key(token)).Build()).AsBytes()
	if valkey.IsValkeyNil(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("session: leyendo: %w", err)
	}
	return b, true, nil
}

// CommitCtx implementa scs.CtxStore.
func (s *ValkeyStore) CommitCtx(ctx context.Context, token string, b []byte, expiry time.Time) error {
	ttl := expiry.Sub(s.clock.Now())
	if ttl <= 0 {
		return s.DeleteCtx(ctx, token)
	}
	cmd := s.client.B().Set().Key(s.key(token)).Value(valkey.BinaryString(b)).Px(ttl).Build()
	if err := s.client.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("session: guardando: %w", err)
	}
	return nil
}

// DeleteCtx implementa scs.CtxStore.
func (s *ValkeyStore) DeleteCtx(ctx context.Context, token string) error {
	if err := s.client.Do(ctx, s.client.B().Del().Key(s.key(token)).Build()).Error(); err != nil {
		return fmt.Errorf("session: borrando: %w", err)
	}
	return nil
}

// Find implementa scs.Store (scs usa las variantes Ctx cuando existen).
func (s *ValkeyStore) Find(token string) ([]byte, bool, error) {
	return s.FindCtx(context.Background(), token)
}

// Commit implementa scs.Store.
func (s *ValkeyStore) Commit(token string, b []byte, expiry time.Time) error {
	return s.CommitCtx(context.Background(), token, b, expiry)
}

// Delete implementa scs.Store.
func (s *ValkeyStore) Delete(token string) error { return s.DeleteCtx(context.Background(), token) }

// ErrNoValkeyURL indica que falta la URL de Valkey.
var ErrNoValkeyURL = errors.New("session: falta la URL de Valkey")

// NewValkeyClient abre un cliente Valkey desde una URL redis(s)://[:clave@]host:puerto/db.
func NewValkeyClient(url string) (valkey.Client, error) {
	if url == "" {
		return nil, ErrNoValkeyURL
	}
	opt, err := valkey.ParseURL(url)
	if err != nil {
		return nil, errors.New("session: URL de Valkey inválida") // sin detalles: contiene la contraseña
	}
	opt.DisableCache = true // sin caché del lado cliente (requiere RESP3 + tracking; no la necesitamos)
	c, err := valkey.NewClient(opt)
	if err != nil {
		return nil, fmt.Errorf("session: conectando a Valkey: %w", err)
	}
	return c, nil
}

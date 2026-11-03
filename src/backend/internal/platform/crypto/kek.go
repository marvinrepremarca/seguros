package crypto

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/marvinrepremarca/seguros/internal/platform/config"
)

// KeyWrapper envuelve y desenvuelve DEKs con la llave maestra (KEK). La KEK nunca sale de su
// custodio (OpenBao) ni se guarda en la BD.
type KeyWrapper interface {
	// Wrap devuelve la DEK envuelta y la referencia de la KEK usada (se guarda junto a la DEK).
	Wrap(ctx context.Context, dek []byte) (wrapped []byte, kekRef string, err error)
	// Unwrap recupera la DEK con la KEK referenciada.
	Unwrap(ctx context.Context, wrapped []byte, kekRef string) ([]byte, error)
}

// ErrUnknownKEK indica una referencia de KEK que este proceso no conoce (rotación incompleta).
var ErrUnknownKEK = errors.New("crypto: KEK desconocida")

// wrapAAD liga el envoltorio a su propósito (una DEK de este sistema).
var wrapAAD = []byte("seguros:dek:v1") //nolint:gochecknoglobals // constante binaria

// LocalKEK es la KEK local de desarrollo y pruebas: llaves versionadas derivadas (HKDF-SHA256) de
// material secreto de entorno. Prohibida en staging/producción (config lo impide).
type LocalKEK struct {
	keys   map[string][]byte
	active string
}

// NewLocalKEK construye la KEK local. material: versión → secreto (≥ 32 caracteres).
func NewLocalKEK(material map[string]string, active string) (*LocalKEK, error) {
	k := &LocalKEK{keys: map[string][]byte{}, active: active}
	for v, m := range material {
		if len(m) < 32 {
			return nil, fmt.Errorf("crypto: la KEK local %s necesita ≥ 32 caracteres", v)
		}
		key, err := hkdf.Key(sha256.New, []byte(m), nil, "seguros:kek:"+v, 32)
		if err != nil {
			return nil, fmt.Errorf("crypto: derivando KEK: %w", err)
		}
		k.keys["local:"+v] = key
	}
	if _, ok := k.keys["local:"+active]; !ok {
		return nil, fmt.Errorf("crypto: la KEK activa %q no está definida", active)
	}
	return k, nil
}

// Wrap implementa KeyWrapper.
func (k *LocalKEK) Wrap(_ context.Context, dek []byte) ([]byte, string, error) {
	ref := "local:" + k.active
	out, err := seal(k.keys[ref], dek, wrapAAD)
	return out, ref, err
}

// Unwrap implementa KeyWrapper.
func (k *LocalKEK) Unwrap(_ context.Context, wrapped []byte, ref string) ([]byte, error) {
	key, ok := k.keys[ref]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKEK, ref)
	}
	return open(key, wrapped, wrapAAD)
}

// seal cifra con AES-256-GCM: nonce aleatorio (12 bytes) ‖ ciphertext+tag.
func seal(key, plaintext, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("crypto: nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, aad), nil
}

func open(key, sealed, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize()+gcm.Overhead() {
		return nil, ErrDecrypt
	}
	pt, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: cifrador: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: GCM: %w", err)
	}
	return gcm, nil
}

// OpenBaoTransit envuelve DEKs con el motor Transit de OpenBao (la KEK nunca sale de OpenBao; su
// versión viaja dentro del ciphertext "vault:vN:..." y OpenBao la rota sin cambios aquí).
type OpenBaoTransit struct {
	addr   string
	token  string
	key    string
	client *http.Client
}

// NewOpenBaoTransit construye el cliente. addr: https://openbao:8200; key: nombre de la llave Transit.
func NewOpenBaoTransit(addr, token, key string, client *http.Client) *OpenBaoTransit {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &OpenBaoTransit{addr: strings.TrimRight(addr, "/"), token: token, key: key, client: client}
}

// Wrap implementa KeyWrapper.
func (o *OpenBaoTransit) Wrap(ctx context.Context, dek []byte) ([]byte, string, error) {
	var out struct {
		Data struct {
			Ciphertext string `json:"ciphertext"`
		} `json:"data"`
	}
	if err := o.call(ctx, "encrypt", map[string]string{"plaintext": base64.StdEncoding.EncodeToString(dek)}, &out); err != nil {
		return nil, "", err
	}
	if !strings.HasPrefix(out.Data.Ciphertext, "vault:v") {
		return nil, "", errors.New("crypto: respuesta de OpenBao inesperada")
	}
	return []byte(out.Data.Ciphertext), "openbao:" + o.key, nil
}

// Unwrap implementa KeyWrapper.
func (o *OpenBaoTransit) Unwrap(ctx context.Context, wrapped []byte, ref string) ([]byte, error) {
	if ref != "openbao:"+o.key {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKEK, ref)
	}
	var out struct {
		Data struct {
			Plaintext string `json:"plaintext"`
		} `json:"data"`
	}
	if err := o.call(ctx, "decrypt", map[string]string{"ciphertext": string(wrapped)}, &out); err != nil {
		return nil, err
	}
	dek, err := base64.StdEncoding.DecodeString(out.Data.Plaintext)
	if err != nil {
		return nil, errors.New("crypto: respuesta de OpenBao inesperada")
	}
	return dek, nil
}

func (o *OpenBaoTransit) call(ctx context.Context, op string, body any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("crypto: serializando: %w", err)
	}
	target := o.addr + "/v1/transit/" + op + "/" + url.PathEscape(o.key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("crypto: petición a OpenBao: %w", err)
	}
	req.Header.Set("X-Vault-Token", o.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.client.Do(req) // #nosec G704 -- destino fijo de configuración (OpenBao)
	if err != nil {
		return fmt.Errorf("crypto: OpenBao %s: %w", op, err)
	}
	defer resp.Body.Close() //nolint:errcheck // cierre de lectura
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("crypto: OpenBao %s respondió %d", op, resp.StatusCode) // sin cuerpo: puede contener detalles
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(out); err != nil {
		return fmt.Errorf("crypto: respuesta de OpenBao: %w", err)
	}
	return nil
}

// NewWrapper construye el KeyWrapper de la configuración.
func NewWrapper(cfg config.Crypto) (KeyWrapper, error) {
	switch cfg.Provider {
	case "openbao":
		return NewOpenBaoTransit(cfg.OpenBaoAddr, cfg.OpenBaoToken.Reveal(), cfg.OpenBaoKey, nil), nil
	case "local":
		material := make(map[string]string, len(cfg.LocalKEKs))
		for v, s := range cfg.LocalKEKs {
			material[v] = s.Reveal()
		}
		return NewLocalKEK(material, cfg.LocalActive)
	default:
		return nil, fmt.Errorf("crypto: proveedor desconocido %q", cfg.Provider)
	}
}

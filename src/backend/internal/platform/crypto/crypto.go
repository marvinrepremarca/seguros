// Package crypto implementa el cifrado de campos de datos Restringidos (ARCHITECTURE.md §4.4,
// rules/security.md §Criptografía), solo con la stdlib de Go:
//
//   - Envelope encryption: cada tenant tiene una DEK (AES-256) versionada en crypto.data_keys,
//     envuelta por la KEK (OpenBao Transit; en desarrollo, KEK local). El ciphertext lleva la
//     versión de la DEK, de modo que rotar no invalida los datos anteriores.
//   - AES-256-GCM con nonce aleatorio y datos asociados (AAD) = versión ‖ tenant ‖ campo: un
//     valor cifrado no se puede mover a otro tenant ni a otro campo sin que falle el descifrado.
//   - Blind index HMAC-SHA256 con una llave derivada (HKDF) por tenant y campo, para buscar por
//     igualdad sin descifrar (columna <campo>_bidx).
//
// Formato del ciphertext: 0x01 ‖ versión DEK (uint32 BE) ‖ nonce (12) ‖ ciphertext+tag (16).
package crypto

import (
	"context"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

const (
	formatV1   byte = 0x01
	headerLen       = 1 + 4
	dekSize         = 32
	dekCacheTT      = time.Hour
	// activeTTL acota cuánto tarda una réplica en enterarse de una rotación de DEK (deja de cifrar
	// con la retirada).
	activeTTL = time.Minute
	// minReload limita las recargas forzadas por versión desconocida (ciphertext forjado).
	minReload = 5 * time.Second
)

// Errores del cifrado. ErrDecrypt no distingue la causa (llave, manipulación, campo o tenant
// distinto): no se filtra información a quien manipula el ciphertext.
var (
	ErrDecrypt   = errors.New("crypto: no se pudo descifrar")
	ErrNoTenant  = errors.New("crypto: sin tenant en el contexto")
	ErrBadField  = errors.New("crypto: nombre de campo inválido")
	ErrNoDataKey = errors.New("crypto: el tenant no tiene llave de datos")
)

// fieldPattern: <modulo>.<entidad>.<campo> (ej. parties.person.document_number).
var fieldPattern = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`) //nolint:gochecknoglobals // regexp inmutable

type cachedDEK struct {
	key     []byte
	expires time.Time
}

type tenantKeys struct {
	active  uint32
	keys    map[uint32]cachedDEK
	loadedT time.Time
}

// Service cifra y descifra campos del tenant del contexto.
type Service struct {
	db       *db.DB
	wrapper  KeyWrapper
	indexKey []byte
	clock    kernel.Clock

	mu    sync.Mutex
	cache map[kernel.TenantID]*tenantKeys
}

// NewService construye el servicio. indexMaterial (≥ 32 caracteres) es el secreto del que se
// derivan las llaves de blind index; vive fuera de la BD, como la KEK.
func NewService(database *db.DB, wrapper KeyWrapper, indexMaterial string, clock kernel.Clock) (*Service, error) {
	if len(indexMaterial) < 32 {
		return nil, errors.New("crypto: la llave de blind index necesita ≥ 32 caracteres")
	}
	key, err := hkdf.Key(sha256.New, []byte(indexMaterial), nil, "seguros:bidx:v1", 32)
	if err != nil {
		return nil, fmt.Errorf("crypto: derivando llave de índice: %w", err)
	}
	return &Service{db: database, wrapper: wrapper, indexKey: key, clock: clock, cache: map[kernel.TenantID]*tenantKeys{}}, nil
}

// Encrypt cifra plaintext para el campo dado con la DEK activa del tenant del contexto (la crea si
// el tenant aún no tiene).
func (s *Service) Encrypt(ctx context.Context, field string, plaintext []byte) ([]byte, error) {
	return s.EncryptFor(ctx, field, "", plaintext)
}

// EncryptFor cifra como Encrypt y además liga el valor al registro dado (ej. el ID de la fila): un
// ciphertext copiado a otra fila del mismo tenant y campo no se puede descifrar.
func (s *Service) EncryptFor(ctx context.Context, field, record string, plaintext []byte) ([]byte, error) {
	tenant, err := tenantAndField(ctx, field)
	if err != nil {
		return nil, err
	}
	version, key, err := s.activeKey(ctx, tenant)
	if err != nil {
		return nil, err
	}
	header := make([]byte, headerLen)
	header[0] = formatV1
	binary.BigEndian.PutUint32(header[1:], version)
	sealed, err := seal(key, plaintext, aad(header, tenant, field, record))
	if err != nil {
		return nil, err
	}
	return append(header, sealed...), nil
}

// Decrypt descifra un valor producido por Encrypt para el mismo tenant y campo.
func (s *Service) Decrypt(ctx context.Context, field string, ciphertext []byte) ([]byte, error) {
	return s.DecryptFor(ctx, field, "", ciphertext)
}

// DecryptFor descifra un valor producido por EncryptFor para el mismo tenant, campo y registro.
func (s *Service) DecryptFor(ctx context.Context, field, record string, ciphertext []byte) ([]byte, error) {
	tenant, err := tenantAndField(ctx, field)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < headerLen || ciphertext[0] != formatV1 {
		return nil, ErrDecrypt
	}
	version := binary.BigEndian.Uint32(ciphertext[1:headerLen])
	key, err := s.keyVersion(ctx, tenant, version)
	if err != nil {
		if errors.Is(err, ErrNoDataKey) {
			return nil, ErrDecrypt
		}
		return nil, err
	}
	return open(key, ciphertext[headerLen:], aad(ciphertext[:headerLen], tenant, field, record))
}

// BlindIndex calcula el índice ciego de un valor YA NORMALIZADO por el llamador (ej. documento
// sin puntos ni espacios, en mayúsculas). Determinista por tenant y campo: el mismo valor en otro
// tenant o campo produce otro índice.
func (s *Service) BlindIndex(ctx context.Context, field, normalized string) ([]byte, error) {
	tenant, err := tenantAndField(ctx, field)
	if err != nil {
		return nil, err
	}
	k, err := hkdf.Key(sha256.New, s.indexKey, []byte(tenant.String()), "bidx:"+field, 32)
	if err != nil {
		return nil, fmt.Errorf("crypto: derivando llave de índice: %w", err)
	}
	m := hmac.New(sha256.New, k)
	if _, err := m.Write([]byte(normalized)); err != nil {
		return nil, fmt.Errorf("crypto: índice: %w", err)
	}
	return m.Sum(nil), nil
}

// RotateDataKey crea una DEK nueva y activa para el tenant del contexto; la anterior queda
// retirada (sigue descifrando). Devuelve la versión nueva. Recifrar los datos antiguos es un job
// aparte.
func (s *Service) RotateDataKey(ctx context.Context) (uint32, error) {
	tenant, ok := reqctx.TenantID(ctx)
	if !ok {
		return 0, ErrNoTenant
	}
	var version uint32
	err := s.db.WithTenantTx(ctx, func(tx pgx.Tx) error {
		var current int32
		if err := tx.QueryRow(ctx, `SELECT coalesce(max(version), 0) FROM crypto.data_keys WHERE tenant_id = $1`, uuid.UUID(tenant)).Scan(&current); err != nil {
			return fmt.Errorf("crypto: leyendo versiones: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE crypto.data_keys SET status = 'retired' WHERE tenant_id = $1 AND status = 'active'`, uuid.UUID(tenant)); err != nil {
			return fmt.Errorf("crypto: retirando DEK: %w", err)
		}
		v, err := s.insertDEK(ctx, tx, tenant, current+1)
		version = v
		return err
	})
	s.forget(tenant)
	return version, err
}

func tenantAndField(ctx context.Context, field string) (kernel.TenantID, error) {
	tenant, ok := reqctx.TenantID(ctx)
	if !ok {
		return kernel.TenantID{}, ErrNoTenant
	}
	if !fieldPattern.MatchString(field) || len(field) > 120 {
		return kernel.TenantID{}, fmt.Errorf("%w: %q", ErrBadField, field)
	}
	return tenant, nil
}

func aad(header []byte, tenant kernel.TenantID, field, record string) []byte {
	out := make([]byte, 0, len(header)+16+len(field)+len(record)+2)
	out = append(out, header...)
	t := uuid.UUID(tenant)
	out = append(out, t[:]...)
	out = append(out, '|')
	out = append(out, field...)
	if record != "" {
		out = append(out, '|')
		out = append(out, record...)
	}
	return out
}

func (s *Service) activeKey(ctx context.Context, tenant kernel.TenantID) (uint32, []byte, error) {
	tk, err := s.load(ctx, tenant, true, false)
	if err != nil {
		return 0, nil, err
	}
	return tk.active, tk.keys[tk.active].key, nil
}

func (s *Service) keyVersion(ctx context.Context, tenant kernel.TenantID, version uint32) ([]byte, error) {
	s.mu.Lock()
	tk, ok := s.cache[tenant]
	s.mu.Unlock()
	if ok {
		if k, ok := tk.keys[version]; ok && s.clock.Now().Before(k.expires) {
			return k.key, nil
		}
	}
	// Versión desconocida en caché (p. ej. otra réplica rotó la DEK): recarga ignorando el TTL.
	force := ok && s.clock.Now().Sub(tk.loadedT) >= minReload
	tk, err := s.load(ctx, tenant, false, force)
	if err != nil {
		return nil, err
	}
	k, ok := tk.keys[version]
	if !ok {
		return nil, ErrNoDataKey
	}
	return k.key, nil
}

// load lee y desenvuelve las DEKs del tenant (caché: la DEK activa se revalida cada minuto). Con create, si el tenant no
// tiene DEK activa la crea (carrera entre instancias resuelta por el índice único).
func (s *Service) load(ctx context.Context, tenant kernel.TenantID, create, force bool) (*tenantKeys, error) {
	now := s.clock.Now()
	s.mu.Lock()
	tk, ok := s.cache[tenant]
	s.mu.Unlock()
	if ok && !force && tk.active != 0 && now.Sub(tk.loadedT) < activeTTL {
		return tk, nil
	}

	ctx = reqctx.WithTenantID(ctx, tenant)
	type row struct {
		version int32
		wrapped []byte
		ref     string
		active  bool
	}
	var rows []row
	read := func(tx pgx.Tx) error {
		rs, err := tx.Query(ctx, `SELECT version, wrapped, kek_ref, status = 'active' FROM crypto.data_keys WHERE tenant_id = $1`, uuid.UUID(tenant))
		if err != nil {
			return fmt.Errorf("crypto: leyendo DEKs: %w", err)
		}
		defer rs.Close()
		rows = rows[:0]
		for rs.Next() {
			var r row
			if err := rs.Scan(&r.version, &r.wrapped, &r.ref, &r.active); err != nil {
				return fmt.Errorf("crypto: leyendo DEKs: %w", err)
			}
			rows = append(rows, r)
		}
		return rs.Err()
	}
	load := func(tx pgx.Tx) error {
		if err := read(tx); err != nil {
			return err
		}
		hasActive, maxVersion := false, int32(0)
		for _, r := range rows {
			hasActive = hasActive || r.active
			maxVersion = max(maxVersion, r.version)
		}
		if hasActive || !create {
			return nil
		}
		if _, err := s.insertDEK(ctx, tx, tenant, maxVersion+1); err != nil {
			return err
		}
		return read(tx)
	}
	err := s.db.WithTenantTx(ctx, load)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		// Otra instancia creó la DEK al mismo tiempo: se lee la suya.
		err = s.db.WithTenantTx(ctx, load)
	}
	if err != nil {
		return nil, err
	}

	tk = &tenantKeys{keys: map[uint32]cachedDEK{}, loadedT: now}
	for _, r := range rows {
		dek, err := s.wrapper.Unwrap(ctx, r.wrapped, r.ref)
		if err != nil {
			return nil, fmt.Errorf("crypto: desenvolviendo DEK v%d: %w", r.version, err)
		}
		v := uint32(r.version) // #nosec G115 -- CHECK version > 0
		tk.keys[v] = cachedDEK{key: dek, expires: now.Add(dekCacheTT)}
		if r.active {
			tk.active = v
		}
	}
	if create && tk.active == 0 {
		return nil, ErrNoDataKey
	}
	s.mu.Lock()
	s.cache[tenant] = tk
	s.mu.Unlock()
	return tk, nil
}

// insertDEK genera, envuelve y guarda una DEK activa. Si otra instancia creó la DEK activa en
// paralelo, el índice único lo rechaza y la transacción falla (el llamador reintenta al leer).
func (s *Service) insertDEK(ctx context.Context, tx pgx.Tx, tenant kernel.TenantID, version int32) (uint32, error) {
	dek := make([]byte, dekSize)
	if _, err := rand.Read(dek); err != nil {
		return 0, fmt.Errorf("crypto: generando DEK: %w", err)
	}
	wrapped, ref, err := s.wrapper.Wrap(ctx, dek)
	if err != nil {
		return 0, fmt.Errorf("crypto: envolviendo DEK: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO crypto.data_keys (tenant_id, version, wrapped, kek_ref, status, created_at)
		VALUES ($1, $2, $3, $4, 'active', $5)`, uuid.UUID(tenant), version, wrapped, ref, s.clock.Now().UTC()); err != nil {
		return 0, fmt.Errorf("crypto: guardando DEK: %w", err)
	}
	return uint32(version), nil // #nosec G115 -- version > 0
}

func (s *Service) forget(tenant kernel.TenantID) {
	s.mu.Lock()
	delete(s.cache, tenant)
	s.mu.Unlock()
}

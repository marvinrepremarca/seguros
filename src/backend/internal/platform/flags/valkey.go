package flags

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/valkey-io/valkey-go"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Canal de invalidación entre instancias (ARCHITECTURE.md §5).
const invalidationChannel = "flags:invalidate"

// l2TTL acota la vida de la caché compartida aunque se pierda una invalidación.
const l2TTL = 5 * time.Minute

// ValkeyCache implementa SharedCache en Valkey: valor en flags:<scope> (con su generación) y
// generación en flags:gen:<scope>.
type ValkeyCache struct {
	client valkey.Client
}

var _ SharedCache = (*ValkeyCache)(nil)

// NewValkeyCache construye la caché.
func NewValkeyCache(client valkey.Client) *ValkeyCache { return &ValkeyCache{client: client} }

func cacheKey(scope string) string { return "flags:" + scope }
func genKey(scope string) string   { return "flags:gen:" + scope }

type cachedValue struct {
	Gen   int64        `json:"g"`
	Flags map[Key]bool `json:"v"`
}

// Generation implementa SharedCache.
func (c *ValkeyCache) Generation(ctx context.Context, scope string) (int64, error) {
	g, err := c.client.Do(ctx, c.client.B().Get().Key(genKey(scope)).Build()).AsInt64()
	if valkey.IsValkeyNil(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("flags: leyendo generación: %w", err)
	}
	return g, nil
}

// Get implementa SharedCache.
func (c *ValkeyCache) Get(ctx context.Context, scope string) (map[Key]bool, bool, error) {
	vals, err := c.client.Do(ctx, c.client.B().Mget().Key(cacheKey(scope), genKey(scope)).Build()).ToArray()
	if err != nil {
		return nil, false, fmt.Errorf("flags: leyendo caché: %w", err)
	}
	if len(vals) != 2 {
		return nil, false, nil
	}
	raw, err := vals[0].AsBytes()
	if valkey.IsValkeyNil(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("flags: leyendo caché: %w", err)
	}
	current, err := vals[1].AsInt64()
	if valkey.IsValkeyNil(err) {
		current, err = 0, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("flags: leyendo generación: %w", err)
	}
	var cv cachedValue
	if json.Unmarshal(raw, &cv) != nil || cv.Gen != current {
		return nil, false, nil //nolint:nilerr // valor corrupto u obsoleto = fallo de caché: se recalcula
	}
	return cv.Flags, true, nil
}

// Set implementa SharedCache.
func (c *ValkeyCache) Set(ctx context.Context, scope string, gen int64, v map[Key]bool) error {
	b, err := json.Marshal(cachedValue{Gen: gen, Flags: v})
	if err != nil {
		return fmt.Errorf("flags: serializando caché: %w", err)
	}
	if err := c.client.Do(ctx, c.client.B().Set().Key(cacheKey(scope)).Value(valkey.BinaryString(b)).Ex(l2TTL).Build()).Error(); err != nil {
		return fmt.Errorf("flags: guardando caché: %w", err)
	}
	return nil
}

// Invalidate implementa SharedCache.
func (c *ValkeyCache) Invalidate(ctx context.Context, scope string) error {
	cmds := valkey.Commands{
		c.client.B().Incr().Key(genKey(scope)).Build(),
		c.client.B().Del().Key(cacheKey(scope)).Build(),
	}
	for _, r := range c.client.DoMulti(ctx, cmds...) {
		if err := r.Error(); err != nil {
			return fmt.Errorf("flags: invalidando caché: %w", err)
		}
	}
	return nil
}

// Invalidator invalida la caché tras un cambio de flag: L1 local + L2 compartida y notifica a
// las demás instancias por pub/sub.
type Invalidator struct {
	eval   *Evaluator
	client valkey.Client
}

// NewInvalidator construye el invalidador. client puede ser nil (una sola instancia, pruebas).
func NewInvalidator(eval *Evaluator, client valkey.Client) *Invalidator {
	return &Invalidator{eval: eval, client: client}
}

// Invalidate descarta la caché del tenant (tenant nulo = switches globales) en todas las instancias.
func (i *Invalidator) Invalidate(ctx context.Context, tenant kernel.TenantID) error {
	if err := i.eval.Invalidate(ctx, tenant); err != nil {
		return err
	}
	if i.client == nil {
		return nil
	}
	msg := globalScope
	if !kernel.IsZeroID(tenant) {
		msg = tenant.String()
	}
	if err := i.client.Do(ctx, i.client.B().Publish().Channel(invalidationChannel).Message(msg).Build()).Error(); err != nil {
		return fmt.Errorf("flags: publicando invalidación: %w", err)
	}
	return nil
}

// Subscribe escucha invalidaciones de otras instancias hasta que ctx se cancele (lo lanza cmd/api
// en su errgroup). Si la conexión se pierde, reintenta y descarta toda la L1 (pudo perder
// mensajes). Los mensajes mal formados se ignoran.
func Subscribe(ctx context.Context, client valkey.Client, eval *Evaluator, logger *slog.Logger) error {
	for {
		err := client.Receive(ctx, client.B().Subscribe().Channel(invalidationChannel).Build(), func(m valkey.PubSubMessage) {
			if m.Message == globalScope {
				eval.InvalidateLocal(kernel.TenantID{})
				return
			}
			u, err := uuid.Parse(m.Message)
			if err != nil {
				logger.WarnContext(ctx, "invalidación de flags mal formada")
				return
			}
			eval.InvalidateLocal(kernel.TenantID(u))
		})
		if ctx.Err() != nil {
			return nil //nolint:nilerr // cancelación ordenada: Receive devuelve el error del contexto
		}
		logger.WarnContext(ctx, "suscripción de invalidación de flags interrumpida; reintentando", "error", err)
		eval.InvalidateAllLocal()
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(2 * time.Second):
		}
	}
}

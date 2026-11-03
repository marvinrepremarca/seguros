package flags

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Source explica de dónde sale el valor de un flag.
type Source string

// Orígenes del valor (precedencia de arriba abajo).
const (
	SourceKillSwitch Source = "kill-switch" // apagado global (ops)
	SourceGlobal     Source = "global"      // valor global de un flag ops
	SourceOverride   Source = "override"    // override del tenant
	SourcePlan       Source = "plan"        // incluido en el plan del tenant
	SourceDefault    Source = "default"     // default del registro
	SourceDependency Source = "dependency"  // apagado porque una dependencia está apagada
	SourceError      Source = "error"       // fail-closed: no se pudo evaluar
)

// ErrNoTenant indica una evaluación sin tenant (solo los flags ops se evalúan sin tenant).
var ErrNoTenant = errors.New("flags: evaluación sin tenant")

// Resolution es el valor efectivo de un flag para un tenant.
type Resolution struct {
	Enabled bool
	Source  Source
}

// State son los datos persistidos que alimentan la evaluación.
type State struct {
	Overrides map[Key]bool // override por tenant
	Global    map[Key]bool // kill switches y flags ops globales
}

// Store lee los overrides (lo implementa el módulo featuremgmt).
type Store interface {
	TenantOverrides(ctx context.Context, tenant kernel.TenantID) (map[Key]bool, error)
	GlobalSwitches(ctx context.Context) (map[Key]bool, error)
}

// SharedCache es la caché L2 compartida entre instancias (Valkey). Cada scope tiene una
// generación que Invalidate incrementa: un valor leído de la BD antes de un cambio y guardado
// después lleva la generación vieja y se ignora (evita repoblar la caché con un valor obsoleto).
type SharedCache interface {
	// Generation devuelve la generación vigente del scope (se lee ANTES de consultar la BD).
	Generation(ctx context.Context, scope string) (int64, error)
	// Get devuelve el valor solo si fue guardado con la generación vigente.
	Get(ctx context.Context, scope string) (map[Key]bool, bool, error)
	Set(ctx context.Context, scope string, gen int64, v map[Key]bool) error
	// Invalidate incrementa la generación y borra el valor.
	Invalidate(ctx context.Context, scope string) error
}

// Checker responde si un flag está encendido para la petición del contexto. Fail-closed: ante
// cualquier error devuelve false.
type Checker interface {
	IsEnabled(ctx context.Context, key Key) bool
}

// globalScope es la clave de caché de los switches globales.
const globalScope = "global"

// l1TTL acota cuánto tarda en verse un cambio si se pierde una invalidación (ARCHITECTURE.md §5).
const l1TTL = 30 * time.Second

type l1Entry struct {
	value   map[Key]bool
	expires time.Time
}

// Evaluator evalúa flags por tenant con caché L1 (memoria, 30 s) y L2 (compartida).
type Evaluator struct {
	reg    *Registry
	store  Store
	shared SharedCache
	clock  kernel.Clock
	logger *slog.Logger

	mu sync.Mutex
	l1 map[string]l1Entry
}

// NewEvaluator construye el evaluador. shared puede ser nil (sin L2).
func NewEvaluator(reg *Registry, store Store, shared SharedCache, clock kernel.Clock, logger *slog.Logger) *Evaluator {
	return &Evaluator{reg: reg, store: store, shared: shared, clock: clock, logger: logger, l1: map[string]l1Entry{}}
}

// Registry devuelve el registro.
func (e *Evaluator) Registry() *Registry { return e.reg }

// IsEnabled implementa Checker con el tenant y plan del contexto autenticado.
func (e *Evaluator) IsEnabled(ctx context.Context, key Key) bool {
	tenant, _ := reqctx.TenantID(ctx)
	res, err := e.Resolve(ctx, tenant, reqctx.TenantPlan(ctx), key)
	if err != nil {
		e.logger.WarnContext(ctx, "flag evaluado como apagado (fail-closed)", "flag", string(key), "error", err)
		return false
	}
	return res.Enabled
}

// Resolve evalúa un flag para un tenant y plan.
func (e *Evaluator) Resolve(ctx context.Context, tenant kernel.TenantID, plan string, key Key) (Resolution, error) {
	def, ok := e.reg.Get(key)
	if !ok {
		return Resolution{Source: SourceError}, fmt.Errorf("flags: clave no registrada %q", key)
	}
	state, err := e.state(ctx, tenant, def.Kind != KindOps)
	if err != nil {
		return Resolution{Source: SourceError}, err
	}
	return Evaluate(e.reg, state, plan, key), nil
}

// Snapshot evalúa todos los flags para un tenant y plan.
func (e *Evaluator) Snapshot(ctx context.Context, tenant kernel.TenantID, plan string) (map[Key]Resolution, error) {
	state, err := e.state(ctx, tenant, true)
	if err != nil {
		return nil, err
	}
	out := make(map[Key]Resolution, len(e.reg.order))
	for _, k := range e.reg.order {
		out[k] = Evaluate(e.reg, state, plan, k)
	}
	return out, nil
}

// Evaluate aplica la precedencia sobre un estado ya cargado: kill switch global → override del
// tenant → plan → default; y un flag solo queda encendido si todo lo que requiere lo está.
func Evaluate(reg *Registry, s State, plan string, key Key) Resolution {
	return evaluate(reg, s, plan, key, map[Key]bool{})
}

func evaluate(reg *Registry, s State, plan string, key Key, visiting map[Key]bool) Resolution {
	def, ok := reg.Get(key)
	if !ok || visiting[key] {
		return Resolution{Source: SourceError}
	}
	var res Resolution
	if g, ok := s.Global[key]; ok && (!g || def.Kind == KindOps) {
		src := SourceKillSwitch
		if g {
			src = SourceGlobal
		}
		return Resolution{Enabled: g, Source: src}
	}
	switch o, has := s.Overrides[key]; {
	case def.Kind == KindOps:
		res = Resolution{Enabled: def.Default, Source: SourceDefault}
	case has:
		res = Resolution{Enabled: o, Source: SourceOverride}
	case slices.Contains(def.Plans, plan):
		res = Resolution{Enabled: true, Source: SourcePlan}
	default:
		res = Resolution{Enabled: def.Default, Source: SourceDefault}
	}
	if !res.Enabled {
		return res
	}
	visiting[key] = true
	defer delete(visiting, key)
	for _, dep := range def.Requires {
		if !evaluate(reg, s, plan, dep, visiting).Enabled {
			return Resolution{Enabled: false, Source: SourceDependency}
		}
	}
	return res
}

// Invalidate descarta la caché del tenant (L1 local y L2 compartida). Con tenant nulo descarta
// los switches globales.
func (e *Evaluator) Invalidate(ctx context.Context, tenant kernel.TenantID) error {
	scope := scopeOf(tenant)
	e.InvalidateLocal(tenant)
	if e.shared == nil {
		return nil
	}
	if err := e.shared.Invalidate(ctx, scope); err != nil {
		return fmt.Errorf("flags: invalidando caché compartida: %w", err)
	}
	return nil
}

// InvalidateLocal descarta solo la L1 (al recibir la notificación de otra instancia).
func (e *Evaluator) InvalidateLocal(tenant kernel.TenantID) {
	e.mu.Lock()
	delete(e.l1, scopeOf(tenant))
	e.mu.Unlock()
}

// InvalidateAllLocal descarta toda la L1.
func (e *Evaluator) InvalidateAllLocal() {
	e.mu.Lock()
	e.l1 = map[string]l1Entry{}
	e.mu.Unlock()
}

func scopeOf(tenant kernel.TenantID) string {
	if kernel.IsZeroID(tenant) {
		return globalScope
	}
	return "t:" + tenant.String()
}

func (e *Evaluator) state(ctx context.Context, tenant kernel.TenantID, needTenant bool) (State, error) {
	global, err := e.load(ctx, globalScope, e.store.GlobalSwitches)
	if err != nil {
		return State{}, err
	}
	if !needTenant {
		return State{Global: global}, nil
	}
	if kernel.IsZeroID(tenant) {
		return State{}, ErrNoTenant
	}
	overrides, err := e.load(ctx, scopeOf(tenant), func(ctx context.Context) (map[Key]bool, error) {
		return e.store.TenantOverrides(ctx, tenant)
	})
	if err != nil {
		return State{}, err
	}
	return State{Global: global, Overrides: overrides}, nil
}

// load resuelve L1 → L2 → Store. Si el Store falla y hay un valor L1 vencido, lo usa (último
// valor conocido, ARCHITECTURE.md §7); si no hay, devuelve el error (fail-closed).
func (e *Evaluator) load(ctx context.Context, scope string, fetch func(context.Context) (map[Key]bool, error)) (map[Key]bool, error) {
	now := e.clock.Now()
	e.mu.Lock()
	entry, cached := e.l1[scope]
	e.mu.Unlock()
	if cached && now.Before(entry.expires) {
		return entry.value, nil
	}
	gen, sharedOK := int64(0), false
	if e.shared != nil {
		v, ok, err := e.shared.Get(ctx, scope)
		if err == nil && ok {
			e.put(scope, v, now)
			return v, nil
		}
		if err == nil {
			gen, err = e.shared.Generation(ctx, scope)
		}
		if err != nil {
			e.logger.WarnContext(ctx, "caché compartida de flags no disponible", "error", err)
		}
		sharedOK = err == nil
	}
	v, err := fetch(ctx)
	if err != nil {
		if cached {
			e.logger.WarnContext(ctx, "usando el último valor conocido de flags", "error", err)
			return entry.value, nil
		}
		return nil, fmt.Errorf("flags: leyendo estado: %w", err)
	}
	if v == nil {
		v = map[Key]bool{}
	}
	if sharedOK {
		if err := e.shared.Set(ctx, scope, gen, v); err != nil {
			e.logger.WarnContext(ctx, "guardando flags en caché compartida", "error", err)
		}
	}
	e.put(scope, v, now)
	return v, nil
}

func (e *Evaluator) put(scope string, v map[Key]bool, now time.Time) {
	e.mu.Lock()
	e.l1[scope] = l1Entry{value: v, expires: now.Add(l1TTL)}
	e.mu.Unlock()
}

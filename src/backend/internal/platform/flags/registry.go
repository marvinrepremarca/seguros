// Package flags implementa los feature flags por tenant (ARCHITECTURE.md §7, rules/feature-flags.md):
// el registro central (registry.yaml, embebido y validado al arrancar), el evaluador con
// precedencia kill switch global → override de tenant → plan → default, cachés L1/L2 con
// invalidación, el proveedor OpenFeature y el middleware Require (404 si está apagado).
//
// Las claves se usan siempre por sus constantes generadas (keys.gen.go): nunca literales.
package flags

//go:generate go run ./gen

import (
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

//go:embed registry.yaml
var registryYAML []byte

// Key es una clave registrada de flag.
type Key string

// Kind es el tipo de flag.
type Kind string

// Tipos de flag.
const (
	KindModule  Kind = "module"
	KindFeature Kind = "feature"
	KindRelease Kind = "release"
	KindOps     Kind = "ops"
)

// Planes comerciales (DECISIONS.md P1; mismos valores que tenancy).
func knownPlans() []string { return []string{"basico", "profesional", "empresarial"} }

// Definition es la entrada del registro de un flag.
type Definition struct {
	Key            Key         `yaml:"key"`
	Kind           Kind        `yaml:"kind"`
	Description    string      `yaml:"description"`
	Owner          string      `yaml:"owner"`
	Default        bool        `yaml:"default"`
	Plans          []string    `yaml:"plans"`
	Requires       []Key       `yaml:"requires"`
	TenantEditable bool        `yaml:"tenantEditable"`
	RemoveBy       kernel.Date `yaml:"removeBy"`
}

// Registry es el registro validado.
type Registry struct {
	defs  map[Key]Definition
	order []Key
}

//nolint:gochecknoglobals // patrones inmutables por tipo (rules/feature-flags.md §Nomenclatura)
var keyPatterns = map[Kind]*regexp.Regexp{
	KindModule:  regexp.MustCompile(`^module\.[a-z][a-z0-9-]*$`),
	KindFeature: regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9-]*$`),
	KindRelease: regexp.MustCompile(`^release\.[a-z][a-z0-9]*\.[a-z][a-z0-9-]*$`),
	KindOps:     regexp.MustCompile(`^ops\.[a-z][a-z0-9-]*\.[a-z][a-z0-9-]*$`),
}

// LoadRegistry interpreta y valida el registro embebido.
func LoadRegistry() (*Registry, error) { return ParseRegistry(registryYAML) }

// ParseRegistry interpreta y valida un registro YAML.
func ParseRegistry(b []byte) (*Registry, error) {
	var doc struct {
		Flags []Definition `yaml:"flags"`
	}
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("flags: registro inválido: %w", err)
	}
	r := &Registry{defs: map[Key]Definition{}}
	var errs []error
	for _, d := range doc.Flags {
		if _, dup := r.defs[d.Key]; dup {
			errs = append(errs, fmt.Errorf("%s: clave duplicada", d.Key))
			continue
		}
		errs = append(errs, validate(d)...)
		r.defs[d.Key] = d
		r.order = append(r.order, d.Key)
	}
	for _, d := range doc.Flags {
		for _, req := range d.Requires {
			dep, ok := r.defs[req]
			switch {
			case req == d.Key:
				errs = append(errs, fmt.Errorf("%s: se requiere a sí mismo", d.Key))
			case !ok:
				errs = append(errs, fmt.Errorf("%s: requiere %s, que no está registrado", d.Key, req))
			case dep.Kind == KindRelease:
				errs = append(errs, fmt.Errorf("%s: no puede depender del flag temporal %s", d.Key, req))
			}
		}
	}
	if len(errs) == 0 {
		if cycle := r.findCycle(); cycle != "" {
			errs = append(errs, fmt.Errorf("dependencia circular: %s", cycle))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("flags: registro inválido: %w", err)
	}
	sort.Slice(r.order, func(i, j int) bool { return r.order[i] < r.order[j] })
	return r, nil
}

func validate(d Definition) []error {
	var errs []error
	fail := func(msg string) { errs = append(errs, fmt.Errorf("%s: %s", d.Key, msg)) }
	pattern, ok := keyPatterns[d.Kind]
	switch {
	case !ok:
		fail("kind debe ser module, feature, release u ops")
	case !pattern.MatchString(string(d.Key)):
		fail("la clave no sigue la nomenclatura de su tipo")
	case d.Kind == KindFeature && (strings.HasPrefix(string(d.Key), "module.") || strings.HasPrefix(string(d.Key), "release.") || strings.HasPrefix(string(d.Key), "ops.")):
		fail("una funcionalidad no puede usar los prefijos module., release. ni ops.")
	}
	if strings.TrimSpace(d.Description) == "" {
		fail("falta description")
	}
	if strings.TrimSpace(d.Owner) == "" {
		fail("falta owner")
	}
	for _, p := range d.Plans {
		if !slices.Contains(knownPlans(), p) {
			fail("plan desconocido " + p)
		}
	}
	if d.TenantEditable && d.Kind != KindFeature {
		fail("solo las funcionalidades (kind=feature) pueden ser tenantEditable")
	}
	if d.Kind == KindRelease && d.RemoveBy.IsZero() {
		fail("un flag release exige removeBy")
	}
	if d.Kind != KindRelease && !d.RemoveBy.IsZero() {
		fail("removeBy solo aplica a flags release")
	}
	if d.Kind == KindOps && len(d.Plans) > 0 {
		fail("los flags ops son globales: no se asignan por plan")
	}
	return errs
}

func (r *Registry) findCycle() string {
	const (
		unvisited = iota
		visiting
		done
	)
	state := map[Key]int{}
	var path []Key
	var visit func(k Key) bool
	visit = func(k Key) bool {
		state[k] = visiting
		path = append(path, k)
		for _, dep := range r.defs[k].Requires {
			if state[dep] == visiting || (state[dep] == unvisited && visit(dep)) {
				path = append(path, dep)
				return true
			}
		}
		path = path[:len(path)-1]
		state[k] = done
		return false
	}
	for _, k := range r.order {
		if state[k] == unvisited && visit(k) {
			parts := make([]string, len(path))
			for i, p := range path {
				parts[i] = string(p)
			}
			return strings.Join(parts, " → ")
		}
	}
	return ""
}

// Get devuelve la definición de una clave.
func (r *Registry) Get(k Key) (Definition, bool) {
	d, ok := r.defs[k]
	return d, ok
}

// Keys devuelve las claves en orden alfabético.
func (r *Registry) Keys() []Key { return slices.Clone(r.order) }

// Dependents devuelve las claves que requieren k directamente.
func (r *Registry) Dependents(k Key) []Key {
	var out []Key
	for _, key := range r.order {
		if slices.Contains(r.defs[key].Requires, k) {
			out = append(out, key)
		}
	}
	return out
}

// ExpiredReleases devuelve los flags release cuyo removeBy ya pasó (make check falla si hay).
func (r *Registry) ExpiredReleases(today kernel.Date) []Key {
	var out []Key
	for _, k := range r.order {
		if d := r.defs[k]; d.Kind == KindRelease && d.RemoveBy.Before(today) {
			out = append(out, k)
		}
	}
	return out
}

// GoName devuelve el identificador Go de la constante de una clave: module.claims → ClaimsModule;
// billing.payment-reminders → BillingPaymentReminders; ops.integrations.insurer-x → OpsIntegrationsInsurerX.
func GoName(k Key) string {
	parts := strings.Split(string(k), ".")
	if parts[0] == "module" {
		parts = append(parts[1:], "module")
	}
	var b strings.Builder
	for _, p := range parts {
		for _, w := range strings.Split(p, "-") {
			if w != "" {
				b.WriteString(strings.ToUpper(w[:1]) + w[1:])
			}
		}
	}
	return b.String()
}

// TSPath devuelve la ruta del objeto TS `features`: module.claims → [claims, module];
// billing.payment-reminders → [billing, paymentReminders].
func TSPath(k Key) []string {
	parts := strings.Split(string(k), ".")
	if parts[0] == "module" {
		parts = append(parts[1:], "module")
	}
	out := make([]string, len(parts))
	for i, p := range parts {
		words := strings.Split(p, "-")
		for j := 1; j < len(words); j++ {
			if words[j] != "" {
				words[j] = strings.ToUpper(words[j][:1]) + words[j][1:]
			}
		}
		out[i] = strings.Join(words, "")
	}
	return out
}

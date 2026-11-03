// Package riskschema valida los datos de riesgo por ramo contra esquemas JSON versionados y
// embebidos (ARCHITECTURE.md §4.1). Usa kin-openapi, ya presente para el contrato HTTP. Ningún
// esquema admite campos desconocidos: así no entran datos Restringidos (salud) a un JSONB sin cifrar.
package riskschema

import (
	"embed"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/marvinrepremarca/seguros/internal/modules/catalog/contracts"
)

//go:embed schemas/*.json
var schemaFiles embed.FS

type schemaVersion struct {
	key     string
	version int32
}

// current lista los esquemas con su versión vigente. Publicar una versión nueva = archivo nuevo.
func current() []schemaVersion {
	return []schemaVersion{{"auto", 1}, {"life", 1}, {"home", 1}, {"business", 1}, {"generic", 1}}
}

type entry struct {
	raw     map[string]any
	label   string
	schema  *openapi3.Schema
	version int32
}

// Registry implementa contracts.RiskSchemas.
type Registry struct {
	current map[string]entry
	order   []string
}

var _ contracts.RiskSchemas = (*Registry)(nil)

// Load interpreta los esquemas embebidos; falla si alguno está mal formado o admite campos
// desconocidos.
func Load() (*Registry, error) {
	r := &Registry{current: map[string]entry{}}
	for _, k := range current() {
		b, err := schemaFiles.ReadFile(fmt.Sprintf("schemas/%s.v%d.json", k.key, k.version))
		if err != nil {
			return nil, fmt.Errorf("riskschema: %w", err)
		}
		var s openapi3.Schema
		if err := json.Unmarshal(b, &s); err != nil {
			return nil, fmt.Errorf("riskschema: %s: %w", k.key, err)
		}
		if s.AdditionalProperties.Has == nil || *s.AdditionalProperties.Has {
			return nil, fmt.Errorf("riskschema: %s debe rechazar campos desconocidos", k.key)
		}
		var raw map[string]any
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("riskschema: %s: %w", k.key, err)
		}
		label, ok := raw["x-label"].(string)
		if !ok || label == "" {
			return nil, fmt.Errorf("riskschema: %s sin x-label", k.key)
		}
		r.current[k.key] = entry{raw: raw, label: label, schema: &s, version: k.version}
		r.order = append(r.order, k.key)
	}
	return r, nil
}

// List implementa contracts.RiskSchemas.
func (r *Registry) List() []contracts.RiskSchema {
	out := make([]contracts.RiskSchema, 0, len(r.order))
	for _, k := range r.order {
		e := r.current[k]
		out = append(out, contracts.RiskSchema{Key: k, Version: e.version, Label: e.label, Schema: e.raw})
	}
	return out
}

// Known indica si la clave corresponde a un esquema publicado.
func (r *Registry) Known(key string) bool { return slices.Contains(r.order, key) }

// Current implementa contracts.RiskSchemas.
func (r *Registry) Current(key string) (int32, bool) {
	e, ok := r.current[key]
	return e.version, ok
}

// Validate implementa contracts.RiskSchemas. El error no incluye los valores recibidos.
func (r *Registry) Validate(key string, data []byte) error {
	e, ok := r.current[key]
	if !ok {
		return contracts.ErrUnknownRiskSchema
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return contracts.ErrInvalidRiskData
	}
	if err := e.schema.VisitJSON(value); err != nil {
		return contracts.ErrInvalidRiskData
	}
	return nil
}

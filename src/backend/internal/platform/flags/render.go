package flags

import (
	"bytes"
	"fmt"
	"go/format"
	"strconv"
	"strings"
)

// GeneratedGoFile y GeneratedTSFile son las salidas de `make generate` (rutas relativas a este
// paquete).
const (
	GeneratedGoFile = "keys.gen.go"
	GeneratedTSFile = "../../../../frontend/src/shared/lib/features.gen.ts"
)

// RenderGo genera las constantes Go de las claves.
func RenderGo(r *Registry) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("// Código generado por `make generate` desde registry.yaml. NO EDITAR.\n\npackage flags\n\n")
	b.WriteString("// Claves registradas (rules/feature-flags.md: prohibido usar literales).\nconst (\n")
	for _, k := range r.Keys() {
		d := r.defs[k]
		fmt.Fprintf(&b, "\t// %s: %s\n\t%s Key = %s\n", GoName(k), oneLine(d.Description), GoName(k), strconv.Quote(string(k)))
	}
	b.WriteString(")\n")
	out, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("flags: formateando Go: %w", err)
	}
	return out, nil
}

// RenderTS genera el objeto `features` y el tipo FeatureKey para el frontend.
func RenderTS(r *Registry) []byte {
	type node struct {
		children map[string]*node
		order    []string
		key      Key
	}
	root := &node{children: map[string]*node{}}
	for _, k := range r.Keys() {
		n := root
		for _, seg := range TSPath(k) {
			child, ok := n.children[seg]
			if !ok {
				child = &node{children: map[string]*node{}}
				n.children[seg] = child
				n.order = append(n.order, seg)
			}
			n = child
		}
		n.key = k
	}
	var b strings.Builder
	b.WriteString("// Código generado por `make generate` desde src/backend/internal/platform/flags/registry.yaml. NO EDITAR.\n")
	b.WriteString("// El backend es la autoridad: estas claves solo deciden qué mostrar (rules/feature-flags.md).\n\n")
	var write func(n *node, indent string)
	write = func(n *node, indent string) {
		for _, seg := range n.order {
			c := n.children[seg]
			if c.key != "" {
				fmt.Fprintf(&b, "%s%s: %s,\n", indent, seg, strconv.Quote(string(c.key)))
				continue
			}
			fmt.Fprintf(&b, "%s%s: {\n", indent, seg)
			write(c, indent+"  ")
			fmt.Fprintf(&b, "%s},\n", indent)
		}
	}
	b.WriteString("export const features = {\n")
	write(root, "  ")
	b.WriteString("} as const;\n\n")
	b.WriteString("export const featureKeys = [\n")
	for _, k := range r.Keys() {
		fmt.Fprintf(&b, "  %s,\n", strconv.Quote(string(k)))
	}
	b.WriteString("] as const;\n\nexport type FeatureKey = (typeof featureKeys)[number];\n")
	return []byte(b.String())
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

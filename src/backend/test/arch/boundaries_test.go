// Package arch verifica reglas de arquitectura que go-arch-lint no puede expresar:
// un módulo solo importa internal/modules/<otro>/contracts (CLAUDE.md regla 1) y
// no hay paquetes "cajón de sastre" dentro de los módulos.
package arch

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/repremarca/seguros"

var forbiddenPackageNames = []string{"common", "utils", "util", "helpers", "shared", "misc"}

func TestModuleBoundaries(t *testing.T) {
	violations, err := checkModules("../../internal/modules", modulePath)
	if err != nil {
		t.Fatalf("analizando módulos: %v", err)
	}
	for _, v := range violations {
		t.Error(v)
	}
}

func TestCheckerDetectsViolations(t *testing.T) {
	violations, err := checkModules("testdata/modules", "example.com/fixture")
	if err != nil {
		t.Fatalf("analizando fixture: %v", err)
	}
	want := []string{
		"alpha/app/bad.go: importa example.com/fixture/internal/modules/beta/domain (solo se permite beta/contracts)",
		"alpha/utils: paquete prohibido dentro de un módulo; lo compartido va a internal/platform",
	}
	if !slices.Equal(violations, want) {
		t.Fatalf("violaciones:\n got: %q\nwant: %q", violations, want)
	}
}

// checkModules recorre root (= internal/modules) y devuelve las violaciones ordenadas.
func checkModules(root, goModule string) ([]string, error) {
	modulesImport := goModule + "/internal/modules/"
	var violations []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root && d == nil {
				return fs.SkipDir // aún no hay módulos
			}
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("ruta relativa: %w", err)
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if d.Name() == "testdata" {
				return fs.SkipDir
			}
			if rel != "." && slices.Contains(forbiddenPackageNames, d.Name()) {
				violations = append(violations,
					rel+": paquete prohibido dentro de un módulo; lo compartido va a internal/platform")
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || rel == "." {
			return nil
		}
		owner, _, _ := strings.Cut(rel, "/")

		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		for _, imp := range f.Imports {
			ip, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return fmt.Errorf("import en %s: %w", path, err)
			}
			rest, ok := strings.CutPrefix(ip, modulesImport)
			if !ok {
				continue
			}
			other, sub, _ := strings.Cut(rest, "/")
			if other == owner {
				continue
			}
			if sub != "contracts" && !strings.HasPrefix(sub, "contracts/") {
				violations = append(violations, fmt.Sprintf(
					"%s: importa %s (solo se permite %s/contracts)", rel, ip, other))
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("recorriendo %s: %w", root, err)
	}
	slices.Sort(violations)
	return violations, nil
}

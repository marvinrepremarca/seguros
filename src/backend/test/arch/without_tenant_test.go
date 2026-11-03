package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// withoutTenantAllowed son los únicos paquetes de módulos que pueden abrir transacciones sin
// tenant (db.WithoutTenantTx): resolver el tenant antes del login y leer los switches globales.
// Cualquier otro uso podría saltarse RLS mediante funciones SECURITY DEFINER.
func withoutTenantAllowed() []string {
	return []string{
		"tenancy/adapters/postgres",
		"featuremgmt/adapters/postgres",
	}
}

func TestWithoutTenantTxIsWhitelisted(t *testing.T) {
	root := "../../internal/modules"
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		if slices.Contains(withoutTenantAllowed(), filepath.ToSlash(rel)) {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "WithoutTenantTx" {
				t.Errorf("%s: WithoutTenantTx fuera de la lista blanca (usa db.WithTenantTx)", fset.Position(sel.Pos()))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

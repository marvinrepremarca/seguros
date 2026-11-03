package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestModulesDoNotTouchThePool: los módulos acceden a la BD solo con db.WithTenantTx (CLAUDE.md
// regla 2). Ni llaman DB.Pool() ni importan pgxpool.
func TestModulesDoNotTouchThePool(t *testing.T) {
	root := "../../internal/modules"
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly|parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			if strings.Contains(imp.Path.Value, "jackc/pgx/v5/pgxpool") {
				t.Errorf("%s: importa pgxpool", path)
			}
		}
		full, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(full, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && len(call.Args) == 0 {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Pool" {
					t.Errorf("%s: llama .Pool() (usa db.WithTenantTx)", fset.Position(call.Pos()))
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

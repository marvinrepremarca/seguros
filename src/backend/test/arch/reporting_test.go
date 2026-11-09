package arch

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Reporting solo lee sus vistas: nunca las tablas de otro módulo (ARCHITECTURE §4.3, ADR 0013).
func TestReportingQueriesOnlyReadReportingViews(t *testing.T) {
	files, err := filepath.Glob("../../queries/reporting/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("sin queries de reporting: %v", err)
	}
	ref := regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+([a-z_][a-z0-9_.]*)`)
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range ref.FindAllStringSubmatch(string(raw), -1) {
			if !strings.HasPrefix(strings.ToLower(m[1]), "reporting.") {
				t.Errorf("%s: %s lee %q fuera del esquema reporting", filepath.Base(f), m[0], m[1])
			}
		}
	}
}

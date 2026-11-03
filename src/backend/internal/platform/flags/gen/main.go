// Command gen genera keys.gen.go y el features.gen.ts del frontend desde registry.yaml
// (`go generate` en internal/platform/flags, invocado por `make generate`).
package main

import (
	"log/slog"
	"os"

	"github.com/marvinrepremarca/seguros/internal/platform/flags"
)

func main() {
	if err := run(); err != nil {
		slog.Error("generando flags", "error", err)
		os.Exit(1)
	}
}

func run() error {
	reg, err := flags.LoadRegistry()
	if err != nil {
		return err
	}
	goSrc, err := flags.RenderGo(reg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(flags.GeneratedGoFile, goSrc, 0o600); err != nil {
		return err
	}
	return os.WriteFile(flags.GeneratedTSFile, flags.RenderTS(reg), 0o600)
}

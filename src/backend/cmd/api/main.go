// Command api sirve la API HTTP y el BFF. El wiring de módulos llega en la Fase 1 (ROADMAP 1.3+).
package main

import (
	"log/slog"
	"os"
)

func main() {
	slog.New(slog.NewJSONHandler(os.Stdout, nil)).Info("api: not implemented yet")
}

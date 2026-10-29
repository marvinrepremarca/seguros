// Command migrate aplica las migraciones goose por módulo con el rol seguros_migrator (ROADMAP 1.4).
package main

import (
	"log/slog"
	"os"
)

func main() {
	slog.New(slog.NewJSONHandler(os.Stdout, nil)).Info("migrate: not implemented yet")
}

// Command worker ejecuta el relay del outbox, los consumidores de eventos y los jobs River (ROADMAP 1.8).
package main

import (
	"log/slog"
	"os"
)

func main() {
	slog.New(slog.NewJSONHandler(os.Stdout, nil)).Info("worker: not implemented yet")
}

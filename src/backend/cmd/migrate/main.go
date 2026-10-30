// Command migrate aplica las migraciones goose por módulo con el rol seguros_migrator. Se ejecuta
// como job de despliegue previo al rollout; la API nunca migra al arrancar.
//
// Uso:
//
//	migrate up               aplica las migraciones pendientes de todos los módulos
//	migrate status           muestra versión y pendientes por módulo
//	migrate down <modulo>    revierte la última migración de un módulo (solo development/test)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/marvinrepremarca/seguros/internal/platform/config"
	"github.com/marvinrepremarca/seguros/internal/platform/db/migrate"
	"github.com/marvinrepremarca/seguros/internal/platform/obs"
	"github.com/marvinrepremarca/seguros/migrations"
)

const usage = "uso: migrate up | status | down <modulo>"

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("migrate terminó con error", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	cfg, err := config.LoadMigrator(os.LookupEnv)
	if err != nil {
		return err
	}
	logger := obs.NewLogger(os.Stdout, obs.LoggerOptions{
		Level: cfg.LogLevel, Service: cfg.ServiceName, Version: cfg.ServiceVersion, Env: string(cfg.Env),
	})
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	sqlDB, err := migrate.Open(cfg.DatabaseURL.Reveal())
	if err != nil {
		return err
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			logger.Warn("cerrando conexión", "error", err)
		}
	}()
	runner := migrate.New(sqlDB, migrations.FS, logger)

	switch args[0] {
	case "up":
		return runner.Up(ctx)
	case "status":
		statuses, err := runner.Status(ctx)
		if err != nil {
			return err
		}
		for _, s := range statuses {
			logger.Info("estado de migraciones", "module", s.Module, "version", s.Current, "pending", s.Pending)
		}
		return nil
	case "down":
		if len(args) != 2 {
			return errors.New(usage)
		}
		if cfg.Env.IsProductionLike() {
			return fmt.Errorf("down no está permitido en %s: corrige con una migración nueva", cfg.Env)
		}
		return runner.DownOne(ctx, args[1])
	default:
		return errors.New(usage)
	}
}

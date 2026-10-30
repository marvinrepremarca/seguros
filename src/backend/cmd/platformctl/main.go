// Command platformctl ejecuta operaciones de super-administrador de la plataforma (DECISIONS.md
// P2): alta de agencias, suspensión y cambio de plan. No hay API HTTP para esto en v1; quien
// ejecuta el binario con acceso a la BD es el super-administrador.
//
// Uso:
//
//	platformctl tenant create --name "Agencia X" --plan basico --org agencia-x
//	platformctl tenant suspend --org agencia-x
//	platformctl tenant reactivate --org agencia-x
//	platformctl tenant set-plan --org agencia-x --plan profesional
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/marvinrepremarca/seguros/internal/modules/tenancy"
	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/app"
	"github.com/marvinrepremarca/seguros/internal/platform/config"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/obs"
)

const usage = `uso:
  platformctl tenant create --name <nombre> --plan <basico|profesional|empresarial> --org <alias>
  platformctl tenant suspend --org <alias>
  platformctl tenant reactivate --org <alias>
  platformctl tenant set-plan --org <alias> --plan <plan>`

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("platformctl terminó con error", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 || args[0] != "tenant" {
		return errors.New(usage)
	}
	cfg, err := config.LoadPlatformCtl(os.LookupEnv)
	if err != nil {
		return err
	}
	logger := obs.NewLogger(os.Stderr, obs.LoggerOptions{
		Level: cfg.LogLevel, Service: cfg.ServiceName, Version: cfg.ServiceVersion, Env: string(cfg.Env),
	})
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	database, err := db.Open(ctx, cfg.DB, db.Options{ApplicationName: cfg.ServiceName})
	if err != nil {
		return err
	}
	defer database.Close()
	return runTenant(ctx, logger, tenancy.New(database, kernel.SystemClock{}), args[1], args[2:])
}

func runTenant(ctx context.Context, logger *slog.Logger, mod *tenancy.Module, sub string, args []string) error {
	fs := flag.NewFlagSet("tenant "+sub, flag.ContinueOnError)
	name := fs.String("name", "", "nombre comercial de la agencia")
	plan := fs.String("plan", "", "plan comercial")
	org := fs.String("org", "", "alias de la Organization de Keycloak")
	if err := fs.Parse(args); err != nil {
		return errors.New(usage)
	}
	switch sub {
	case "create":
		id, err := mod.Provision.Handle(ctx, app.ProvisionTenantCommand{Name: *name, Plan: *plan, OrgAlias: *org})
		if err != nil {
			return err
		}
		logger.Info("tenant creado", "tenant_id", id.String(), "org", *org, "plan", *plan)
	case "suspend", "reactivate":
		if err := mod.ChangeStatus.Handle(ctx, app.ChangeTenantStatusCommand{OrgAlias: *org, Suspend: sub == "suspend"}); err != nil {
			return err
		}
		logger.Info("estado del tenant actualizado", "org", *org, "operation", sub)
	case "set-plan":
		if err := mod.ChangePlan.Handle(ctx, app.ChangeTenantPlanCommand{OrgAlias: *org, Plan: *plan}); err != nil {
			return err
		}
		logger.Info("plan del tenant actualizado", "org", *org, "plan", *plan)
	default:
		return errors.New(usage)
	}
	return nil
}

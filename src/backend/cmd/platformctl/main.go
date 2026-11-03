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
//	platformctl flag set --org agencia-x --key module.claims --value on|off|clear --reason "add-on"
//	platformctl flag kill --key module.claims --reason "incidente"     (kill switch global)
//	platformctl flag restore --key module.claims --reason "resuelto"
//
// Los comandos de flag exigen SEGUROS_VALKEY_URL para invalidar las cachés de la API.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/valkey-io/valkey-go"

	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt"
	fmapp "github.com/marvinrepremarca/seguros/internal/modules/featuremgmt/app"
	"github.com/marvinrepremarca/seguros/internal/modules/tenancy"
	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/app"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/config"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/obs"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
	"github.com/marvinrepremarca/seguros/internal/platform/session"
)

const usage = `uso:
  platformctl tenant create --name <nombre> --plan <basico|profesional|empresarial> --org <alias>
  platformctl tenant suspend --org <alias>
  platformctl tenant reactivate --org <alias>
  platformctl tenant set-plan --org <alias> --plan <plan>
  platformctl flag set --org <alias> --key <flag> --value on|off|clear --reason <motivo>
  platformctl flag kill --key <flag> --reason <motivo>
  platformctl flag restore --key <flag> --reason <motivo>`

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("platformctl terminó con error", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 || (args[0] != "tenant" && args[0] != "flag") {
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
	tenants := tenancy.New(database, kernel.SystemClock{})
	if args[0] == "tenant" {
		return runTenant(ctx, logger, tenants, args[1], args[2:])
	}
	if cfg.Valkey.URL.IsEmpty() {
		return errors.New("los comandos de flag requieren SEGUROS_VALKEY_URL (invalidación de cachés)")
	}
	valkeyClient, err := session.NewValkeyClient(cfg.Valkey.URL.Reveal())
	if err != nil {
		return err
	}
	defer valkeyClient.Close()
	return runFlag(ctx, flagDeps{db: database, tenants: tenants, valkey: valkeyClient, logger: logger}, args[1], args[2:])
}

type flagDeps struct {
	db      *db.DB
	tenants *tenancy.Module
	valkey  valkey.Client
	logger  *slog.Logger
}

func runFlag(ctx context.Context, d flagDeps, sub string, args []string) error {
	fs := flag.NewFlagSet("flag "+sub, flag.ContinueOnError)
	org := fs.String("org", "", "alias de la Organization de Keycloak del tenant")
	key := fs.String("key", "", "clave registrada del flag")
	value := fs.String("value", "", "on | off | clear (vuelve al plan)")
	reason := fs.String("reason", "", "motivo (queda auditado)")
	if err := fs.Parse(args); err != nil {
		return errors.New(usage)
	}
	clock := kernel.SystemClock{}
	reg, err := flags.LoadRegistry()
	if err != nil {
		return err
	}
	store := featuremgmt.NewStore(d.db, audit.NewRecorder(clock))
	eval := flags.NewEvaluator(reg, store, flags.NewValkeyCache(d.valkey), clock, d.logger)
	mod := featuremgmt.New(featuremgmt.Deps{
		Store: store, Tenants: d.tenants.Directory, Registry: reg,
		Invalidator: flags.NewInvalidator(eval, d.valkey), Clock: clock, Logger: d.logger,
	})
	switch sub {
	case "set":
		var enabled *bool
		switch *value {
		case "on", "off":
			v := *value == "on"
			enabled = &v
		case "clear":
		default:
			return errors.New(usage)
		}
		info, err := d.tenants.Directory.ResolveByOrgAlias(ctx, *org)
		if err != nil {
			return err
		}
		v, err := mod.PlatformSetFlag.Handle(reqctx.WithTenantID(ctx, info.ID),
			fmapp.SetTenantFlagCommand{Key: flags.Key(*key), Enabled: enabled, Reason: *reason})
		if err != nil {
			return err
		}
		d.logger.Info("flag del tenant actualizado", "org", *org, "flag", *key, "enabled", v.Resolution.Enabled, "source", string(v.Resolution.Source))
	case "kill", "restore":
		var enabled *bool
		if sub == "kill" {
			v := false
			enabled = &v
		}
		if err := mod.SetGlobalSwitch.Handle(ctx, fmapp.SetGlobalSwitchCommand{Key: flags.Key(*key), Enabled: enabled, Reason: *reason}); err != nil {
			return err
		}
	default:
		return errors.New(usage)
	}
	return nil
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

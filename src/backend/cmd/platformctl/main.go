// Command platformctl ejecuta operaciones de super-administrador de la plataforma (DECISIONS.md
// P2): alta de agencias, suspensión y cambio de plan. No hay API HTTP para esto en v1; quien
// ejecuta el binario con acceso a la BD es el super-administrador.
//
// Uso:
//
//	platformctl tenant create --name "Agencia X" --plan basico --org agencia-x --reason "contrato" --operator ana.ops
//	platformctl tenant suspend --org agencia-x --reason "impago" --operator ana.ops
//	platformctl tenant reactivate --org agencia-x --reason "pagó" --operator ana.ops
//	platformctl tenant set-plan --org agencia-x --plan profesional --reason "upgrade" --operator ana.ops
//	platformctl flag set --org agencia-x --key module.claims --value on|off|clear --reason "add-on"
//	platformctl flag kill --key module.claims --reason "incidente" --operator ana.ops   (kill switch global)
//	platformctl flag kill --key module.compliance --confirm-control module.compliance --reason "..." --operator ana.ops
//	platformctl flag restore --key module.claims --reason "resuelto" --operator ana.ops
//	platformctl dead-letter list --org agencia-x
//	platformctl dead-letter retry --org agencia-x --id <uuid> --reason "falla corregida" --operator ana.ops
//
// Se ejecuta con el rol seguros_platform (SEGUROS_DB_URL), el único que escribe switches globales.
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

	"github.com/google/uuid"
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
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
	"github.com/marvinrepremarca/seguros/internal/platform/session"
)

const usage = `uso:
  platformctl tenant create --name <nombre> --plan <basico|profesional|empresarial> --org <alias> --reason <motivo> --operator <persona>
  platformctl tenant suspend --org <alias> --reason <motivo> --operator <persona>
  platformctl tenant reactivate --org <alias> --reason <motivo> --operator <persona>
  platformctl tenant set-plan --org <alias> --plan <plan> --reason <motivo> --operator <persona>
  platformctl flag set --org <alias> --key <flag> --value on|off|clear --reason <motivo> [--operator <persona> --confirm-control <flag>]
  platformctl flag kill --key <flag> --reason <motivo> --operator <persona> [--confirm-control <flag>]
  platformctl flag restore --key <flag> --reason <motivo> --operator <persona>
  platformctl dead-letter list --org <alias>
  platformctl dead-letter retry --org <alias> --id <uuid> --reason <motivo> --operator <persona>`

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("platformctl terminó con error", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 || (args[0] != "tenant" && args[0] != "flag" && args[0] != "dead-letter") {
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
	if args[0] == "dead-letter" {
		return runDeadLetter(ctx, database, tenants, logger, args[1], args[2:])
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
	operator := fs.String("operator", "", "persona que ejecuta el cambio (obligatorio en kill/restore)")
	confirm := fs.String("confirm-control", "", "repite la clave para apagar un flag de control regulatorio (ADR 0012)")
	if err := fs.Parse(args); err != nil {
		return errors.New(usage)
	}
	clock := kernel.SystemClock{}
	reg, err := flags.LoadRegistry()
	if err != nil {
		return err
	}
	store := featuremgmt.NewStore(d.db, audit.NewRecorder(clock), outbox.NewWriter(clock))
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
			fmapp.SetTenantFlagCommand{Key: flags.Key(*key), Enabled: enabled, Reason: *reason, Operator: *operator, ConfirmControl: *confirm})
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
		if err := mod.SetGlobalSwitch.Handle(ctx, fmapp.SetGlobalSwitchCommand{Key: flags.Key(*key), Enabled: enabled, Reason: *reason, Operator: *operator, ConfirmControl: *confirm}); err != nil {
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
	reason := fs.String("reason", "", "motivo (queda auditado; sin datos personales)")
	operator := fs.String("operator", "", "persona que ejecuta el cambio (queda auditada)")
	if err := fs.Parse(args); err != nil {
		return errors.New(usage)
	}
	switch sub {
	case "create":
		id, err := mod.Provision.Handle(ctx, app.ProvisionTenantCommand{Name: *name, Plan: *plan, OrgAlias: *org, Reason: *reason, Operator: *operator})
		if err != nil {
			return err
		}
		logger.Info("tenant creado", "tenant_id", id.String(), "org", *org, "plan", *plan)
	case "suspend", "reactivate":
		if err := mod.ChangeStatus.Handle(ctx, app.ChangeTenantStatusCommand{OrgAlias: *org, Suspend: sub == "suspend", Reason: *reason, Operator: *operator}); err != nil {
			return err
		}
		logger.Info("estado del tenant actualizado", "org", *org, "operation", sub)
	case "set-plan":
		if err := mod.ChangePlan.Handle(ctx, app.ChangeTenantPlanCommand{OrgAlias: *org, Plan: *plan, Reason: *reason, Operator: *operator}); err != nil {
			return err
		}
		logger.Info("plan del tenant actualizado", "org", *org, "plan", *plan)
	default:
		return errors.New(usage)
	}
	return nil
}

func runDeadLetter(ctx context.Context, database *db.DB, tenants *tenancy.Module, logger *slog.Logger, sub string, args []string) error {
	fs := flag.NewFlagSet("dead-letter", flag.ContinueOnError)
	org := fs.String("org", "", "alias de la organización")
	id := fs.String("id", "", "ID del mensaje")
	reason := fs.String("reason", "", "motivo")
	operator := fs.String("operator", "", "persona que ejecuta")
	if err := fs.Parse(args); err != nil {
		return err
	}
	info, err := tenants.Directory.ResolveByOrgAlias(ctx, *org)
	if err != nil {
		return err
	}
	ctx = reqctx.WithTenantID(ctx, info.ID)
	switch sub {
	case "list":
		letters, err := outbox.ListDeadLetters(ctx, database)
		if err != nil {
			return err
		}
		for _, l := range letters {
			logger.Info("mensaje fallido", "id", l.ID.String(), "consumer", l.Consumer, "topic", l.Topic, "event_id", l.EventID.String(), "failed_at", l.FailedAt)
		}
		logger.Info("mensajes fallidos pendientes", "org", *org, "count", len(letters))
		return nil
	case "retry":
		letter, err := uuid.Parse(*id)
		if err != nil || *reason == "" || *operator == "" {
			return errors.New(usage)
		}
		if err := outbox.RetryDeadLetter(ctx, database, letter, kernel.SystemClock{}.Now()); err != nil {
			return err
		}
		logger.Info("mensaje reprocesado", "org", *org, "id", letter.String(), "reason", *reason, "operator", *operator)
		return nil
	default:
		return errors.New(usage)
	}
}

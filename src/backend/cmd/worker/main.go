// Command worker ejecuta el trabajo asíncrono (ROADMAP 1.8): el relay del outbox, los consumidores
// de eventos de integración (Watermill, inbox idempotente) y los jobs River (incluidos los
// periódicos de plataforma). Varias réplicas pueden correr a la vez.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/riverqueue/river"
	"golang.org/x/sync/errgroup"

	"github.com/marvinrepremarca/seguros/internal/modules/billing"
	"github.com/marvinrepremarca/seguros/internal/modules/catalog"
	"github.com/marvinrepremarca/seguros/internal/modules/commissions"
	"github.com/marvinrepremarca/seguros/internal/modules/crm"
	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt"
	"github.com/marvinrepremarca/seguros/internal/modules/parties"
	"github.com/marvinrepremarca/seguros/internal/modules/policies"
	"github.com/marvinrepremarca/seguros/internal/modules/quoting"
	"github.com/marvinrepremarca/seguros/internal/modules/renewals"
	"github.com/marvinrepremarca/seguros/internal/modules/tenancy"
	tenancycontracts "github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/config"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/jobs"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/obs"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
	"github.com/marvinrepremarca/seguros/internal/platform/session"
)

func main() {
	command := run
	if len(os.Args) > 1 && os.Args[1] == "rebuild-scopes" {
		command = func() error { return rebuildScopes(os.Args[2:]) }
	}
	if err := command(); err != nil {
		slog.Error("worker terminó con error", "error", err)
		os.Exit(1)
	}
}

// tenantPlans adapta el directorio de tenancy a outbox.TenantPlans.
type tenantPlans struct{ dir tenancycontracts.Directory }

func (p tenantPlans) PlanOf(ctx context.Context, tenant kernel.TenantID) (string, error) {
	info, err := p.dir.Get(ctx, tenant)
	if err != nil {
		return "", err
	}
	return info.Plan, nil
}

func run() error {
	cfg, err := config.LoadWorker(os.LookupEnv)
	if err != nil {
		return err
	}
	logger := obs.NewLogger(os.Stdout, obs.LoggerOptions{
		Level: cfg.LogLevel, Service: cfg.ServiceName, Version: cfg.ServiceVersion, Env: string(cfg.Env),
	})
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTelemetry, err := obs.SetupTelemetry(ctx, obs.TelemetryOptions{
		Service: cfg.ServiceName, Version: cfg.ServiceVersion, Env: string(cfg.Env),
		Endpoint: cfg.OTLPEndpoint, SampleRatio: cfg.TraceSampleRatio,
	})
	if err != nil {
		return fmt.Errorf("telemetría: %w", err)
	}
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := shutdownTelemetry(flushCtx); err != nil {
			logger.Warn("cerrando telemetría", "error", err)
		}
	}()

	database, err := db.Open(ctx, cfg.DB, db.Options{ApplicationName: cfg.ServiceName})
	if err != nil {
		return err
	}
	defer database.Close()
	if err := database.RequireRole(ctx, "seguros_worker"); err != nil {
		return err
	}
	valkeyClient, err := session.NewValkeyClient(cfg.Valkey.URL.Reveal())
	if err != nil {
		return err
	}
	defer valkeyClient.Close()

	clock := kernel.SystemClock{}
	tenants := tenancy.New(database, clock)

	// Flags: los consumidores de módulos apagables evalúan el flag del tenant del evento.
	registry, err := flags.LoadRegistry()
	if err != nil {
		return err
	}
	flagStore := featuremgmt.NewStore(database, audit.NewRecorder(clock), outbox.NewWriter(clock))
	evaluator := flags.NewEvaluator(registry, flagStore, flags.NewValkeyCache(valkeyClient), clock, logger)
	invalidator := flags.NewInvalidator(evaluator, valkeyClient)

	// Eventos de integración: outbox → relay → Watermill (en proceso) → consumidores.
	pubsub := outbox.NewInProcessPubSub(logger)
	dispatcher, err := outbox.NewDispatcher(outbox.DispatcherOptions{
		DB: database, Subscriber: pubsub, Flags: evaluator,
		Plans: tenantPlans{dir: tenants.Directory}, Clock: clock, Logger: logger,
	})
	if err != nil {
		return err
	}
	consumers := featuremgmt.Consumers(invalidator)
	consumers = append(consumers, parties.Consumers(audit.NewRecorder(clock))...)
	consumers = append(consumers, policies.Consumers(database, audit.NewRecorder(clock), outbox.NewWriter(clock), clock)...)
	consumers = append(consumers, billing.Consumers(database, audit.NewRecorder(clock), outbox.NewWriter(clock), clock)...)
	catalogMod, err := catalog.New(catalog.Deps{DB: database, Audit: audit.NewRecorder(clock), Clock: clock, Logger: logger})
	if err != nil {
		return err
	}
	consumers = append(consumers, commissions.Consumers(database, audit.NewRecorder(clock), catalogMod.Directory, clock)...)
	consumers = append(consumers, renewals.Consumers(database, audit.NewRecorder(clock), outbox.NewWriter(clock), clock)...)
	consumers = append(consumers, crm.Consumers(database, audit.NewRecorder(clock), clock)...)
	consumers = append(consumers, quoting.Consumers(audit.NewRecorder(clock))...)
	for _, c := range consumers {
		if err := dispatcher.Register(c); err != nil {
			return err
		}
	}
	relay := outbox.NewRelay(database, pubsub, clock, logger, outbox.RelayOptions{})

	// Jobs River (periódicos de plataforma; los módulos añadirán los suyos).
	workers := river.NewWorkers()
	periodic := jobs.PlatformJobs(workers, database, clock, logger)
	periodic = append(periodic, policies.RegisterJobs(workers, database, audit.NewRecorder(clock), outbox.NewWriter(clock), clock)...)
	periodic = append(periodic, billing.RegisterJobs(workers, database, audit.NewRecorder(clock), outbox.NewWriter(clock), evaluator, tenantPlans{dir: tenants.Directory}, clock, logger)...)
	periodic = append(periodic, renewals.RegisterJobs(workers, database, audit.NewRecorder(clock), outbox.NewWriter(clock), evaluator, tenantPlans{dir: tenants.Directory}, clock, logger)...)
	riverClient, _, err := jobs.NewWorkerClient(database.Pool(), jobs.WorkerOptions{Workers: workers, Periodic: periodic, Logger: logger})
	if err != nil {
		return err
	}

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return dispatcher.Run(gctx) })
	g.Go(func() error {
		select {
		case <-dispatcher.Running(): // los consumidores deben estar suscritos antes de publicar
		case <-gctx.Done():
			return nil
		}
		logger.InfoContext(gctx, "relay del outbox iniciado")
		return relay.Run(gctx)
	})
	g.Go(func() error {
		if err := riverClient.Start(gctx); err != nil {
			return fmt.Errorf("river: %w", err)
		}
		<-gctx.Done()
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(gctx), 30*time.Second)
		defer cancel()
		if err := riverClient.Stop(stopCtx); err != nil {
			return fmt.Errorf("deteniendo river: %w", err)
		}
		return nil
	})
	g.Go(func() error { return flags.Subscribe(gctx, valkeyClient, evaluator, logger) })

	logger.InfoContext(ctx, "worker iniciado")
	if err := g.Wait(); err != nil {
		return err
	}
	if err := pubsub.Close(); err != nil {
		logger.Warn("cerrando pub/sub", "error", err)
	}
	logger.Info("worker detenido")
	return nil
}

// rebuildScopes republica el alcance de las pólizas de una agencia. Antes de producción y al reparar
// parties.policy_scopes: los consumidores deben estar corriendo en el worker desplegado.
func rebuildScopes(args []string) error {
	fs := flag.NewFlagSet("rebuild-scopes", flag.ContinueOnError)
	org := fs.String("org", "", "alias de la organización")
	operator := fs.String("operator", "", "persona que ejecuta")
	reason := fs.String("reason", "", "motivo")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *org == "" || *operator == "" || *reason == "" {
		return errors.New("uso: worker rebuild-scopes --org <alias> --operator <persona> --reason <motivo>")
	}
	cfg, err := config.LoadWorker(os.LookupEnv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	database, err := db.Open(ctx, cfg.DB, db.Options{ApplicationName: cfg.ServiceName})
	if err != nil {
		return err
	}
	defer database.Close()
	if err := database.RequireRole(ctx, "seguros_worker"); err != nil {
		return err
	}
	clock := kernel.SystemClock{}
	info, err := tenancy.New(database, clock).Directory.ResolveByOrgAlias(ctx, *org)
	if err != nil {
		return err
	}
	n, err := policies.RebuildScopes(reqctx.WithTenantID(ctx, info.ID), database, audit.NewRecorder(clock), outbox.NewWriter(clock), *operator, *reason)
	if err != nil {
		return err
	}
	slog.Info("alcance de pólizas republicado", "org", *org, "policies", n)
	return nil
}

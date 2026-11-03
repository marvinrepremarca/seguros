// Command worker ejecuta el trabajo asíncrono (ROADMAP 1.8): el relay del outbox, los consumidores
// de eventos de integración (Watermill, inbox idempotente) y los jobs River (incluidos los
// periódicos de plataforma). Varias réplicas pueden correr a la vez.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/riverqueue/river"
	"golang.org/x/sync/errgroup"

	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt"
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
	"github.com/marvinrepremarca/seguros/internal/platform/session"
)

func main() {
	if err := run(); err != nil {
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
	for _, c := range featuremgmt.Consumers(invalidator) {
		if err := dispatcher.Register(c); err != nil {
			return err
		}
	}
	relay := outbox.NewRelay(database, pubsub, clock, logger, outbox.RelayOptions{})

	// Jobs River (periódicos de plataforma; los módulos añadirán los suyos).
	workers := river.NewWorkers()
	periodic := jobs.PlatformJobs(workers, database, clock)
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

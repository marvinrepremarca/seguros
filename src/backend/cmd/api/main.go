// Command api sirve la API HTTP y el BFF.
//
// Uso:
//
//	api              arranca el servidor
//	api healthcheck  consulta /health/live del proceso local (HEALTHCHECK de la imagen distroless)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/open-feature/go-sdk/openfeature"
	"golang.org/x/sync/errgroup"

	"github.com/marvinrepremarca/seguros/internal/modules/catalog"
	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt"
	"github.com/marvinrepremarca/seguros/internal/modules/identity"
	"github.com/marvinrepremarca/seguros/internal/modules/identity/adapters/oidc"
	"github.com/marvinrepremarca/seguros/internal/modules/parties"
	"github.com/marvinrepremarca/seguros/internal/modules/policies"
	"github.com/marvinrepremarca/seguros/internal/modules/tenancy"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/config"
	"github.com/marvinrepremarca/seguros/internal/platform/crypto"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/obs"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
	"github.com/marvinrepremarca/seguros/internal/platform/session"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(); err != nil {
			slog.Error("healthcheck fallido", "error", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("api terminó con error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadAPI(os.LookupEnv)
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
	if err := database.RequireRole(ctx, "seguros_app", "seguros_worker", "seguros_platform"); err != nil {
		return err
	}

	valkeyClient, err := session.NewValkeyClient(cfg.Valkey.URL.Reveal())
	if err != nil {
		return err
	}
	defer valkeyClient.Close()

	clock := kernel.SystemClock{}
	policy, err := authz.DefaultPolicy()
	if err != nil {
		return err
	}
	discoverCtx, cancelDiscover := context.WithTimeout(ctx, 15*time.Second)
	provider, err := oidc.New(discoverCtx, cfg.Auth, oidc.NewHTTPClient())
	cancelDiscover()
	if err != nil {
		return err
	}
	tenants := tenancy.New(database, clock)
	recorder := audit.NewRecorder(clock)
	identityMod, err := identity.New(identity.Deps{
		DB: database, Clock: clock, Audit: recorder, Tenants: tenants.Directory,
		Policy: policy, Provider: provider,
		SessionStore: session.NewValkeyStore(valkeyClient, "bff:sess:", clock),
		AuthTxStore:  session.NewValkeyStore(valkeyClient, "bff:authtx:", clock),
		Auth:         cfg.Auth, Logger: logger,
	})
	if err != nil {
		return err
	}

	// Feature flags: registro → evaluador (L1 + L2 Valkey) → proveedor OpenFeature "tenantflags".
	registry, err := flags.LoadRegistry()
	if err != nil {
		return err
	}
	flagStore := featuremgmt.NewStore(database, recorder, outbox.NewWriter(clock))
	evaluator := flags.NewEvaluator(registry, flagStore, flags.NewValkeyCache(valkeyClient), clock, logger)
	if err := openfeature.SetNamedProviderAndWait(flags.ProviderName, flags.NewProvider(evaluator)); err != nil {
		return fmt.Errorf("registrando proveedor de flags: %w", err)
	}
	featureMod := featuremgmt.New(featuremgmt.Deps{
		Store: flagStore, Tenants: tenants.Directory, Registry: registry,
		Invalidator: flags.NewInvalidator(evaluator, valkeyClient),
		Checker:     flags.NewOpenFeatureChecker(openfeature.NewClient(flags.ProviderName)),
		Policy:      policy, Clock: clock, Logger: logger,
	})

	cryptoCfg, err := config.LoadCrypto(os.LookupEnv, cfg.Env)
	if err != nil {
		return err
	}
	keyWrapper, err := crypto.NewWrapper(cryptoCfg)
	if err != nil {
		return err
	}
	cipher, err := crypto.NewService(database, keyWrapper, cryptoCfg.IndexKey.Reveal(), clock)
	if err != nil {
		return err
	}
	partiesMod := parties.New(parties.Deps{DB: database, Audit: recorder, Cipher: cipher, Policy: policy, Clock: clock, Logger: logger})
	catalogMod, err := catalog.New(catalog.Deps{DB: database, Audit: recorder, Policy: policy, Clock: clock, Logger: logger})
	if err != nil {
		return err
	}
	policiesMod := policies.New(policies.Deps{
		DB: database, Audit: recorder, Outbox: outbox.NewWriter(clock), Parties: partiesMod.Directory,
		Catalog: catalogMod.Directory, Schemas: catalogMod.Schemas, Users: identityMod.Directory,
		Flags: evaluator, Policy: policy, Clock: clock, Logger: logger,
	})

	var spa http.Handler
	if cfg.HTTP.SPADir != "" {
		spa, err = httpx.NewSPAHandler(os.DirFS(cfg.HTTP.SPADir), logger)
		if err != nil {
			return err
		}
	}

	router, err := buildRouter(routerDeps{
		Logger: logger, Clock: clock, MaxBodyBytes: cfg.HTTP.MaxBodyBytes, ClientIPHdr: cfg.HTTP.ClientIPHeader,
		Readiness: []httpx.ReadinessCheck{
			{Name: "postgres", Check: database.Ping},
			{Name: "valkey", Check: func(ctx context.Context) error {
				return valkeyClient.Do(ctx, valkeyClient.B().Ping().Build()).Error()
			}},
		},
		Identity:    identityMod,
		FeatureMgmt: featureMod,
		Parties:     partiesMod,
		Catalog:     catalogMod,
		Policies:    policiesMod,
		SPA:         spa,
	})
	if err != nil {
		return err
	}

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTP.Addr)
	if err != nil {
		return fmt.Errorf("escuchando en %s: %w", cfg.HTTP.Addr, err)
	}
	logger.Info("api escuchando", "addr", ln.Addr().String())
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return httpx.Serve(gctx, httpx.NewServer(cfg.HTTP, router, logger), ln, cfg.HTTP.ShutdownTimeout)
	})
	g.Go(func() error { return flags.Subscribe(gctx, valkeyClient, evaluator, logger) })
	if err := g.Wait(); err != nil {
		return err
	}
	logger.Info("api detenida")
	return nil
}

// healthcheck consulta /health/live en el puerto configurado del propio contenedor.
func healthcheck() error {
	addr := os.Getenv("SEGUROS_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("SEGUROS_HTTP_ADDR inválida: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	target := "http://" + net.JoinHostPort("127.0.0.1", port) + "/health/live"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil) // #nosec G704 -- destino fijo 127.0.0.1; el puerto sale de la config del propio contenedor
	if err != nil {
		return fmt.Errorf("creando petición: %w", err)
	}
	resp, err := http.DefaultClient.Do(req) // #nosec G704 -- ver arriba
	if err != nil {
		return fmt.Errorf("consultando /health/live: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Debug("cerrando body", "error", cerr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return errors.New("estado " + resp.Status)
	}
	return nil
}

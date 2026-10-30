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

	"github.com/marvinrepremarca/seguros/internal/platform/config"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/obs"
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

	router := httpx.NewRouter(httpx.RouterOptions{
		Logger:       logger,
		Clock:        kernel.SystemClock{},
		MaxBodyBytes: cfg.HTTP.MaxBodyBytes,
		Readiness:    []httpx.ReadinessCheck{{Name: "postgres", Check: database.Ping}},
	})

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTP.Addr)
	if err != nil {
		return fmt.Errorf("escuchando en %s: %w", cfg.HTTP.Addr, err)
	}
	logger.Info("api escuchando", "addr", ln.Addr().String())
	if err := httpx.Serve(ctx, httpx.NewServer(cfg.HTTP, router, logger), ln, cfg.HTTP.ShutdownTimeout); err != nil {
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil) //nolint:gosec // G704: destino fijo 127.0.0.1; el puerto sale de la config del propio contenedor
	if err != nil {
		return fmt.Errorf("creando petición: %w", err)
	}
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // G704: ver arriba
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

package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/sync/errgroup"

	"github.com/marvinrepremarca/seguros/internal/platform/config"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// readinessTimeout acota cada comprobación de /health/ready.
const readinessTimeout = 2 * time.Second

// ReadinessCheck comprueba una dependencia (BD, Valkey...) para /health/ready.
type ReadinessCheck struct {
	Name  string
	Check func(ctx context.Context) error
}

// RouterOptions configura NewRouter.
type RouterOptions struct {
	Logger       *slog.Logger
	Clock        kernel.Clock
	MaxBodyBytes int64
	Readiness    []ReadinessCheck
}

// NewRouter crea el router raíz con los middlewares de plataforma, los endpoints de salud y
// respuestas problem+json para 404/405. Los módulos montan sus grupos de rutas sobre él
// (con auth → tenant → flags → rate limit, ver rules/architecture.md §API).
func NewRouter(opts RouterOptions) *chi.Mux {
	r := chi.NewRouter()
	r.Use(
		Recoverer(opts.Logger),
		RequestID,
		RouteTelemetry,
		SecurityHeaders,
		MaxBodyBytes(opts.MaxBodyBytes),
		AccessLog(opts.Logger, opts.Clock),
	)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		WriteProblem(w, r, http.StatusNotFound, CodeNotFound, titleForStatus(http.StatusNotFound))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		WriteProblem(w, r, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "Método no permitido")
	})
	r.Get("/health/live", func(w http.ResponseWriter, _ *http.Request) {
		writeHealth(w, http.StatusOK, "ok")
	})
	r.Get("/health/ready", readyHandler(opts.Logger, opts.Readiness))
	return r
}

func readyHandler(logger *slog.Logger, checks []ReadinessCheck) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		g, ctx := errgroup.WithContext(r.Context())
		for _, c := range checks {
			g.Go(func() error {
				cctx, cancel := context.WithTimeout(ctx, readinessTimeout)
				defer cancel()
				if err := c.Check(cctx); err != nil {
					return fmt.Errorf("%s: %w", c.Name, err)
				}
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			logger.WarnContext(r.Context(), "readiness fallida", "error", err)
			WriteProblem(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Servicio no disponible")
			return
		}
		writeHealth(w, http.StatusOK, "ok")
	}
}

func writeHealth(w http.ResponseWriter, status int, s string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": s}); err != nil {
		slog.Debug("escribiendo health", "error", err)
	}
}

// NewServer crea el http.Server con todos los timeouts definidos (anti-Slowloris) y la
// instrumentación OpenTelemetry por fuera del router.
func NewServer(cfg config.HTTP, handler http.Handler, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              cfg.Addr,
		Handler:           otelhttp.NewHandler(handler, "http.server"),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    1 << 16,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
}

// Serve atiende en ln hasta que ctx se cancele; entonces cierra con gracia esperando hasta
// shutdownTimeout a las peticiones en curso.
func Serve(ctx context.Context, srv *http.Server, ln net.Listener, shutdownTimeout time.Duration) error {
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		return fmt.Errorf("servidor HTTP: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("cerrando servidor HTTP: %w", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("servidor HTTP: %w", err)
	}
	return nil
}

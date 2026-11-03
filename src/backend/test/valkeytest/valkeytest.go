//go:build integration

// Package valkeytest levanta un Valkey efímero (testcontainers) con la misma imagen del compose de
// desarrollo y contraseña aleatoria.
package valkeytest

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Env es una instancia de Valkey.
type Env struct {
	// URL redis://:clave@host:puerto/0
	URL       string
	container testcontainers.Container
}

// Start levanta el contenedor.
func Start(ctx context.Context) (*Env, error) {
	image, err := composeImage("valkey")
	if err != nil {
		return nil, err
	}
	password := rand.Text()
	c, err := testcontainers.Run(ctx, image,
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithCmd("valkey-server", "--requirepass", password),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("6379/tcp")),
	)
	if err != nil {
		return nil, fmt.Errorf("valkeytest: iniciando Valkey: %w", err)
	}
	host, err := c.Host(ctx)
	if err != nil {
		return nil, errors.Join(err, testcontainers.TerminateContainer(c))
	}
	port, err := c.MappedPort(ctx, "6379/tcp")
	if err != nil {
		return nil, errors.Join(err, testcontainers.TerminateContainer(c))
	}
	u := url.URL{Scheme: "redis", User: url.UserPassword("", password), Host: net.JoinHostPort(host, port.Port()), Path: "/0"}
	return &Env{URL: u.String(), container: c}, nil
}

// Terminate detiene el contenedor.
func (e *Env) Terminate() error {
	if err := testcontainers.TerminateContainer(e.container); err != nil {
		return fmt.Errorf("valkeytest: terminando: %w", err)
	}
	return nil
}

func composeImage(service string) (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("valkeytest: no se pudo ubicar el archivo fuente")
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "deploy", "compose.dev.yml")) //nolint:gosec // ruta fija del repo
	if err != nil {
		return "", fmt.Errorf("valkeytest: leyendo compose: %w", err)
	}
	m := regexp.MustCompile(`(?m)^\s+image:\s+(valkey/` + regexp.QuoteMeta(service) + `:\S+)`).FindSubmatch(b)
	if m == nil {
		return "", errors.New("valkeytest: imagen de valkey no encontrada en el compose")
	}
	return string(m[1]), nil
}

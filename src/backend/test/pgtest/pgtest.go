//go:build integration

// Package pgtest levanta un PostgreSQL efímero (testcontainers) idéntico al de desarrollo: misma
// imagen que deploy/compose.dev.yml y mismo script de roles (deploy/postgres/init), con
// contraseñas aleatorias por ejecución.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Env es una instancia de PostgreSQL con los roles de la aplicación.
type Env struct {
	// AppURL conecta como seguros_app (sujeto a RLS, sin DDL).
	AppURL string
	// MigratorURL conecta como seguros_migrator (dueño de los esquemas).
	MigratorURL string
	// SuperuserURL conecta como postgres; solo para preparar escenarios de prueba.
	SuperuserURL string

	container *postgres.PostgresContainer
}

// Terminate detiene el contenedor.
func (e *Env) Terminate() error {
	if err := testcontainers.TerminateContainer(e.container); err != nil {
		return fmt.Errorf("pgtest: terminando contenedor: %w", err)
	}
	return nil
}

// Start levanta el contenedor y espera a que el script de roles haya corrido.
func Start(ctx context.Context) (*Env, error) {
	root, err := repoRoot()
	if err != nil {
		return nil, err
	}
	image, err := composeImage(filepath.Join(root, "deploy", "compose.dev.yml"), "postgres")
	if err != nil {
		return nil, err
	}
	pw := map[string]string{}
	for _, k := range []string{"POSTGRES_PASSWORD", "SEGUROS_MIGRATOR_PASSWORD", "SEGUROS_APP_PASSWORD", "KEYCLOAK_DB_PASSWORD"} {
		if pw[k], err = randomHex(16); err != nil {
			return nil, err
		}
	}

	c, err := postgres.Run(ctx, image,
		postgres.WithUsername("postgres"),
		postgres.WithPassword(pw["POSTGRES_PASSWORD"]),
		postgres.WithDatabase("postgres"),
		postgres.WithInitScripts(filepath.Join(root, "deploy", "postgres", "init", "01-roles.sh")),
		testcontainers.WithEnv(map[string]string{
			"SEGUROS_MIGRATOR_PASSWORD": pw["SEGUROS_MIGRATOR_PASSWORD"],
			"SEGUROS_APP_PASSWORD":      pw["SEGUROS_APP_PASSWORD"],
			"KEYCLOAK_DB_PASSWORD":      pw["KEYCLOAK_DB_PASSWORD"],
		}),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		return nil, fmt.Errorf("pgtest: iniciando PostgreSQL: %w", err)
	}
	host, err := c.Host(ctx)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("pgtest: host: %w", err), testcontainers.TerminateContainer(c))
	}
	port, err := c.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return nil, errors.Join(fmt.Errorf("pgtest: puerto: %w", err), testcontainers.TerminateContainer(c))
	}
	hostPort := net.JoinHostPort(host, port.Port())
	mk := func(user, password, db string) string {
		u := url.URL{Scheme: "postgres", User: url.UserPassword(user, password), Host: hostPort, Path: "/" + db, RawQuery: "sslmode=disable"}
		return u.String()
	}
	return &Env{
		AppURL:       mk("seguros_app", pw["SEGUROS_APP_PASSWORD"], "seguros"),
		MigratorURL:  mk("seguros_migrator", pw["SEGUROS_MIGRATOR_PASSWORD"], "seguros"),
		SuperuserURL: mk("postgres", pw["POSTGRES_PASSWORD"], "seguros"),
		container:    c,
	}, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("pgtest: aleatorio: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// repoRoot ubica la raíz del repositorio a partir de este archivo (src/backend/test/pgtest).
func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("pgtest: no se pudo ubicar el archivo fuente")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", ".."), nil
}

// composeImage devuelve la imagen (con digest) de un servicio del compose de desarrollo, para que
// las pruebas usen exactamente la misma versión de PostgreSQL.
func composeImage(composeFile, service string) (string, error) {
	b, err := os.ReadFile(composeFile) //nolint:gosec // ruta fija dentro del repositorio
	if err != nil {
		return "", fmt.Errorf("pgtest: leyendo compose: %w", err)
	}
	re := regexp.MustCompile(`(?m)^\s+image:\s+(` + regexp.QuoteMeta(service) + `:\S+)`)
	m := re.FindSubmatch(b)
	if m == nil {
		return "", fmt.Errorf("pgtest: imagen de %q no encontrada en el compose", service)
	}
	return string(m[1]), nil
}

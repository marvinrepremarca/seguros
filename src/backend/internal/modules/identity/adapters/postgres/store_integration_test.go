//go:build integration

package postgres_test

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/identity/adapters/postgres"
	"github.com/marvinrepremarca/seguros/internal/modules/identity/app"
	"github.com/marvinrepremarca/seguros/internal/modules/identity/domain"
	tenancy "github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
	"github.com/marvinrepremarca/seguros/test/pgtest"
)

var database *db.DB //nolint:gochecknoglobals // compartido por la suite (TestMain)

func TestMain(m *testing.M) { os.Exit(runMain(m)) }

func runMain(m *testing.M) int {
	ctx := context.Background()
	env, err := pgtest.Start(ctx)
	if err != nil {
		slog.Error("iniciando PostgreSQL", "error", err)
		return 1
	}
	defer env.Terminate() //nolint:errcheck // limpieza al final de la suite
	if err := env.MigrateAll(ctx); err != nil {
		slog.Error("migrando", "error", err)
		return 1
	}
	database, err = env.OpenApp(ctx)
	if err != nil {
		slog.Error("abriendo BD", "error", err)
		return 1
	}
	defer database.Close()
	return m.Run()
}

func clock() *kernel.FakeClock { return kernel.NewFakeClock(time.Now().UTC()) } //nolint:forbidigo // partición de auditoría del mes actual

// directory es un tenancy.Directory en memoria (identity solo conoce tenancy/contracts).
type directory struct {
	mu      sync.Mutex
	byAlias map[string]tenancy.TenantInfo
}

func (d *directory) ResolveByOrgAlias(_ context.Context, alias string) (tenancy.TenantInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if t, ok := d.byAlias[alias]; ok {
		return t, nil
	}
	return tenancy.TenantInfo{}, tenancy.ErrTenantNotFound
}

func (d *directory) Get(context.Context, kernel.TenantID) (tenancy.TenantInfo, error) {
	return tenancy.TenantInfo{}, tenancy.ErrTenantNotFound
}

func newDirectory() *directory { return &directory{byAlias: map[string]tenancy.TenantInfo{}} }

// newTenant registra un tenant activo con su organización.
func newTenant(t *testing.T, d *directory) (kernel.TenantID, string) {
	t.Helper()
	id := kernel.TenantID(uuid.Must(uuid.NewV7()))
	alias := "ag-" + uuid.Must(uuid.NewV7()).String()[24:]
	d.mu.Lock()
	d.byAlias[alias] = tenancy.TenantInfo{ID: id, Plan: "profesional", Active: true}
	d.mu.Unlock()
	return id, alias
}

func loginCmd(subject, alias string) app.CompleteLoginCommand {
	return app.CompleteLoginCommand{
		Subject: subject, Profile: domain.Profile{Email: "ana@example.test", DisplayName: "Ana"},
		Roles: []string{"Operations"}, Organizations: []string{alias},
	}
}

func count(ctx context.Context, t *testing.T, query string) int {
	t.Helper()
	var n int
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error { return tx.QueryRow(ctx, query).Scan(&n) }))
	return n
}

func TestLoginProvisionsUserWithAuditAndIsolation(t *testing.T) {
	t.Parallel()
	c := clock()
	tenants := newDirectory()
	h := app.NewCompleteLoginHandler(tenants, postgres.NewStore(database, audit.NewRecorder(c)), c, true)
	tenantA, aliasA := newTenant(t, tenants)
	tenantB, _ := newTenant(t, tenants)

	first, err := h.Handle(context.Background(), loginCmd("kc-a", aliasA))
	require.NoError(t, err)
	assert.Equal(t, tenantA, first.TenantID)

	c.Advance(time.Minute)
	second, err := h.Handle(context.Background(), loginCmd("kc-a", aliasA))
	require.NoError(t, err)
	assert.Equal(t, first.UserID, second.UserID, "el segundo login reutiliza el usuario")

	ctxA := reqctx.WithTenantID(context.Background(), tenantA)
	ctxB := reqctx.WithTenantID(context.Background(), tenantB)
	assert.Equal(t, 1, count(ctxA, t, `SELECT count(*) FROM identity.users`))
	assert.Equal(t, 1, count(ctxA, t, `SELECT count(*) FROM identity.users WHERE version = 2`), "update con concurrencia optimista")
	assert.Equal(t, 2, count(ctxA, t, `SELECT count(*) FROM audit.events WHERE action = 'identity.session.login' AND outcome = 'success'`))
	assert.Equal(t, 0, count(ctxB, t, `SELECT count(*) FROM identity.users`), "tenant B no ve usuarios de A")
	assert.Equal(t, 0, count(ctxB, t, `SELECT count(*) FROM audit.events`), "tenant B no ve auditoría de A")
}

func TestSameSubjectInTwoTenantsAreDistinctUsers(t *testing.T) {
	t.Parallel()
	c := clock()
	tenants := newDirectory()
	h := app.NewCompleteLoginHandler(tenants, postgres.NewStore(database, audit.NewRecorder(c)), c, true)
	_, aliasA := newTenant(t, tenants)
	_, aliasB := newTenant(t, tenants)

	a, err := h.Handle(context.Background(), loginCmd("kc-shared", aliasA))
	require.NoError(t, err)
	b, err := h.Handle(context.Background(), loginCmd("kc-shared", aliasB))
	require.NoError(t, err)
	assert.NotEqual(t, a.UserID, b.UserID)
}

func TestDeniedLoginIsAudited(t *testing.T) {
	t.Parallel()
	c := clock()
	tenants := newDirectory()
	h := app.NewCompleteLoginHandler(tenants, postgres.NewStore(database, audit.NewRecorder(c)), c, true)
	tenantID, alias := newTenant(t, tenants)

	cmd := loginCmd("kc-admin", alias)
	cmd.Roles = []string{"TenantAdmin"} // exige MFA y no hay amr
	_, err := h.Handle(context.Background(), cmd)
	require.ErrorIs(t, err, app.ErrMFARequired)

	ctx := reqctx.WithTenantID(context.Background(), tenantID)
	assert.Equal(t, 0, count(ctx, t, `SELECT count(*) FROM identity.users`))
	assert.Equal(t, 1, count(ctx, t, `SELECT count(*) FROM audit.events WHERE outcome = 'denied' AND actor_type = 'system'
		AND details->>'reason' = 'identity.login.mfa_required'`))
}

func TestInsertConflictMapsToConcurrentUpdate(t *testing.T) {
	t.Parallel()
	c := clock()
	tenantID, _ := newTenant(t, newDirectory())
	store := postgres.NewStore(database, audit.NewRecorder(c))
	ctx := reqctx.WithTenantID(context.Background(), tenantID)

	mk := func() *domain.User {
		id, err := kernel.NewID[kernel.UserID]()
		require.NoError(t, err)
		u, err := domain.NewUser(id, tenantID, "kc-dup", domain.Profile{DisplayName: "Ana"}, c.Now())
		require.NoError(t, err)
		return u
	}
	require.NoError(t, store.WithinTenant(ctx, func(ctx context.Context, uow app.UnitOfWork) error { return uow.Users().Insert(ctx, mk()) }))
	err := store.WithinTenant(ctx, func(ctx context.Context, uow app.UnitOfWork) error { return uow.Users().Insert(ctx, mk()) })
	require.ErrorIs(t, err, domain.ErrConcurrentUpdate)

	stale := mk()
	err = store.WithinTenant(ctx, func(ctx context.Context, uow app.UnitOfWork) error { return uow.Users().Update(ctx, stale) })
	require.ErrorIs(t, err, domain.ErrConcurrentUpdate, "id inexistente / versión desfasada")
}

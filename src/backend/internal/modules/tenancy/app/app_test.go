package app_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/app"
	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/tenancy/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// fakeStore simula la BD con la misma regla que RLS: dentro de WithinTenant solo se ve y se
// escribe la fila del tenant del contexto.
type fakeStore struct {
	mu   sync.Mutex
	rows map[kernel.TenantID]domain.TenantSnapshot
}

func newFakeStore() *fakeStore {
	return &fakeStore{rows: map[kernel.TenantID]domain.TenantSnapshot{}}
}

func (s *fakeStore) WithinTenant(ctx context.Context, fn func(context.Context, app.TenantRepository) error) error {
	tenant, ok := reqctx.TenantID(ctx)
	if !ok {
		return assert.AnError
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(ctx, &fakeRepo{s: s, tenant: tenant})
}

func (s *fakeStore) ResolveOrgAlias(_ context.Context, alias string) (kernel.TenantID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, r := range s.rows {
		if r.OrgAlias == alias {
			return id, nil
		}
	}
	return kernel.TenantID{}, domain.ErrNotFound
}

type fakeRepo struct {
	s      *fakeStore
	tenant kernel.TenantID
}

func (r *fakeRepo) Insert(_ context.Context, t *domain.Tenant) error {
	if t.ID() != r.tenant {
		return assert.AnError // WITH CHECK de RLS
	}
	for _, row := range r.s.rows {
		if row.OrgAlias == t.OrgAlias() {
			return domain.ErrOrgAliasTaken
		}
	}
	r.s.rows[t.ID()] = t.Snapshot()
	return nil
}

func (r *fakeRepo) Get(_ context.Context, id kernel.TenantID) (*domain.Tenant, error) {
	row, ok := r.s.rows[id]
	if !ok || id != r.tenant {
		return nil, domain.ErrNotFound
	}
	return domain.RehydrateTenant(row), nil
}

func (r *fakeRepo) Update(_ context.Context, t *domain.Tenant) error {
	s := t.Snapshot()
	cur, ok := r.s.rows[s.ID]
	if !ok || s.ID != r.tenant || cur.Version != s.Version {
		return domain.ErrConcurrentUpdate
	}
	s.Version++
	r.s.rows[s.ID] = s
	return nil
}

type fixture struct {
	store     *fakeStore
	clock     *kernel.FakeClock
	provision *app.ProvisionTenantHandler
	status    *app.ChangeTenantStatusHandler
	plan      *app.ChangeTenantPlanHandler
	dir       *app.Directory
}

func newFixture() fixture {
	store := newFakeStore()
	clock := kernel.NewFakeClock(time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC))
	return fixture{
		store: store, clock: clock,
		provision: app.NewProvisionTenantHandler(store, clock),
		status:    app.NewChangeTenantStatusHandler(store, clock),
		plan:      app.NewChangeTenantPlanHandler(store, clock),
		dir:       app.NewDirectory(store),
	}
}

func TestProvisionAndResolve(t *testing.T) {
	t.Parallel()
	f := newFixture()
	ctx := context.Background()
	id, err := f.provision.Handle(ctx, app.ProvisionTenantCommand{Name: "Agencia A", Plan: "profesional", OrgAlias: "agencia-a"})
	require.NoError(t, err)

	info, err := f.dir.ResolveByOrgAlias(ctx, "agencia-a")
	require.NoError(t, err)
	assert.Equal(t, contracts.TenantInfo{ID: id, Plan: "profesional", Active: true}, info)

	_, err = f.dir.ResolveByOrgAlias(ctx, "no-existe")
	require.ErrorIs(t, err, contracts.ErrTenantNotFound)
	_, err = f.dir.Get(ctx, kernel.TenantID{})
	require.ErrorIs(t, err, contracts.ErrTenantNotFound)
}

func TestProvisionValidation(t *testing.T) {
	t.Parallel()
	f := newFixture()
	ctx := context.Background()
	_, err := f.provision.Handle(ctx, app.ProvisionTenantCommand{Name: "A", Plan: "gratis", OrgAlias: "a"})
	require.ErrorIs(t, err, domain.ErrInvalidPlan)
	_, err = f.provision.Handle(ctx, app.ProvisionTenantCommand{Name: "", Plan: "basico", OrgAlias: "a"})
	require.ErrorIs(t, err, domain.ErrInvalidName)

	_, err = f.provision.Handle(ctx, app.ProvisionTenantCommand{Name: "A", Plan: "basico", OrgAlias: "a"})
	require.NoError(t, err)
	_, err = f.provision.Handle(ctx, app.ProvisionTenantCommand{Name: "B", Plan: "basico", OrgAlias: "a"})
	require.ErrorIs(t, err, domain.ErrOrgAliasTaken)
}

func TestSuspendReactivateAndChangePlan(t *testing.T) {
	t.Parallel()
	f := newFixture()
	ctx := context.Background()
	id, err := f.provision.Handle(ctx, app.ProvisionTenantCommand{Name: "A", Plan: "basico", OrgAlias: "a"})
	require.NoError(t, err)

	require.NoError(t, f.status.Handle(ctx, app.ChangeTenantStatusCommand{OrgAlias: "a", Suspend: true}))
	info, err := f.dir.Get(ctx, id)
	require.NoError(t, err)
	assert.False(t, info.Active)

	err = f.status.Handle(ctx, app.ChangeTenantStatusCommand{OrgAlias: "a", Suspend: true})
	require.ErrorIs(t, err, domain.ErrInvalidTransition)

	require.NoError(t, f.status.Handle(ctx, app.ChangeTenantStatusCommand{OrgAlias: "a"}))
	require.NoError(t, f.plan.Handle(ctx, app.ChangeTenantPlanCommand{OrgAlias: "a", Plan: "empresarial"}))
	info, err = f.dir.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, contracts.TenantInfo{ID: id, Plan: "empresarial", Active: true}, info)

	require.ErrorIs(t, f.plan.Handle(ctx, app.ChangeTenantPlanCommand{OrgAlias: "a", Plan: "x"}), domain.ErrInvalidPlan)
	require.ErrorIs(t, f.plan.Handle(ctx, app.ChangeTenantPlanCommand{OrgAlias: "zz", Plan: "basico"}), domain.ErrNotFound)
}

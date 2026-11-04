package app_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/identity/app"
	"github.com/marvinrepremarca/seguros/internal/modules/identity/domain"
	tenancy "github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// --- fakes ---

type fakeTenants struct {
	byAlias map[string]tenancy.TenantInfo
}

func (f *fakeTenants) ResolveByOrgAlias(_ context.Context, alias string) (tenancy.TenantInfo, error) {
	t, ok := f.byAlias[alias]
	if !ok {
		return tenancy.TenantInfo{}, tenancy.ErrTenantNotFound
	}
	return t, nil
}

func (f *fakeTenants) Get(context.Context, kernel.TenantID) (tenancy.TenantInfo, error) {
	return tenancy.TenantInfo{}, errors.New("no usado")
}

type fakeStore struct {
	mu          sync.Mutex
	users       map[string]*domain.User // por subject
	events      []audit.Entry
	eventTenant []kernel.TenantID
	eventActor  []bool
	insertErrs  []error // errores a devolver en Insert, en orden
}

func newStore() *fakeStore { return &fakeStore{users: map[string]*domain.User{}} }

func (s *fakeStore) WithinTenant(ctx context.Context, fn func(ctx context.Context, uow app.UnitOfWork) error) error {
	if _, ok := reqctx.TenantID(ctx); !ok {
		return errors.New("sin tenant")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(ctx, &fakeUoW{s: s})
}

type fakeUoW struct{ s *fakeStore }

func (u *fakeUoW) Users() app.UserRepository { return u }

func (u *fakeUoW) Audit(ctx context.Context, e audit.Entry) error {
	tid, _ := reqctx.TenantID(ctx)
	_, hasUser := reqctx.UserID(ctx)
	u.s.events = append(u.s.events, e)
	u.s.eventTenant = append(u.s.eventTenant, tid)
	u.s.eventActor = append(u.s.eventActor, hasUser)
	return nil
}

func (u *fakeUoW) GetBySubject(_ context.Context, _ kernel.TenantID, subject string) (*domain.User, error) {
	if usr, ok := u.s.users[subject]; ok {
		return usr, nil
	}
	return nil, domain.ErrNotFound
}

func (u *fakeUoW) Insert(_ context.Context, usr *domain.User) error {
	if len(u.s.insertErrs) > 0 {
		err := u.s.insertErrs[0]
		u.s.insertErrs = u.s.insertErrs[1:]
		if errors.Is(err, domain.ErrConcurrentUpdate) {
			// Otro login concurrente lo creó entretanto.
			u.s.users[usr.Subject()] = usr
		}
		return err
	}
	u.s.users[usr.Subject()] = usr
	return nil
}

func (u *fakeUoW) Update(_ context.Context, usr *domain.User) error {
	u.s.users[usr.Subject()] = usr
	return nil
}

// --- escenario ---

var (
	activeTenant    = tenancy.TenantInfo{ID: kernel.TenantID(uuid.Must(uuid.NewV7())), Plan: "profesional", Active: true} //nolint:gochecknoglobals // datos de prueba
	suspendedTenant = tenancy.TenantInfo{ID: kernel.TenantID(uuid.Must(uuid.NewV7())), Plan: "basico", Active: false}     //nolint:gochecknoglobals // datos de prueba
	now             = time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC)                                                       //nolint:gochecknoglobals // datos de prueba
)

func handler(store *fakeStore, mfa bool) *app.CompleteLoginHandler {
	tenants := &fakeTenants{byAlias: map[string]tenancy.TenantInfo{"agencia-x": activeTenant, "agencia-z": suspendedTenant}}
	return app.NewCompleteLoginHandler(tenants, store, kernel.NewFakeClock(now), mfa)
}

func cmd(mut ...func(*app.CompleteLoginCommand)) app.CompleteLoginCommand {
	c := app.CompleteLoginCommand{
		Subject: "kc-1", Profile: domain.Profile{Email: "ana@example.test", DisplayName: "Ana"},
		Roles: []string{"Collections", "Desconocido"}, Organizations: []string{"agencia-x"}, AuthMethods: []string{"pwd"},
	}
	for _, m := range mut {
		m(&c)
	}
	return c
}

func TestCompleteLoginProvisionsUserAndAudits(t *testing.T) {
	t.Parallel()
	store := newStore()
	res, err := handler(store, true).Handle(context.Background(), cmd())
	require.NoError(t, err)

	assert.Equal(t, activeTenant.ID, res.TenantID)
	assert.Equal(t, "profesional", res.Plan)
	assert.Equal(t, []authz.Role{authz.RoleCollections}, res.Roles, "roles desconocidos descartados")
	assert.Equal(t, "Ana", res.DisplayName)
	require.Contains(t, store.users, "kc-1")
	assert.Equal(t, res.UserID, store.users["kc-1"].ID())

	require.Len(t, store.events, 1)
	assert.Equal(t, "identity.session.login", store.events[0].Action)
	assert.Equal(t, audit.OutcomeSuccess, store.events[0].Outcome)
	assert.Equal(t, kernel.IDString(res.UserID), store.events[0].ResourceID)
	assert.Equal(t, activeTenant.ID, store.eventTenant[0])
	assert.True(t, store.eventActor[0], "el actor del login es el propio usuario")
}

func TestCompleteLoginUpdatesExistingUser(t *testing.T) {
	t.Parallel()
	store := newStore()
	h := handler(store, true)
	first, err := h.Handle(context.Background(), cmd())
	require.NoError(t, err)
	second, err := h.Handle(context.Background(), cmd(func(c *app.CompleteLoginCommand) { c.Profile.DisplayName = "Ana María" }))
	require.NoError(t, err)

	assert.Equal(t, first.UserID, second.UserID, "mismo usuario interno")
	assert.Equal(t, "Ana María", store.users["kc-1"].Profile().DisplayName)
	assert.Len(t, store.events, 2)
}

func TestCompleteLoginRetriesConcurrentFirstLogin(t *testing.T) {
	t.Parallel()
	store := newStore()
	store.insertErrs = []error{domain.ErrConcurrentUpdate}
	res, err := handler(store, true).Handle(context.Background(), cmd())
	require.NoError(t, err)
	assert.Equal(t, store.users["kc-1"].ID(), res.UserID)
}

func TestCompleteLoginDenials(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		mfa        bool
		mut        func(*app.CompleteLoginCommand)
		want       error
		wantAudits bool
	}{
		{"sin organización", true, func(c *app.CompleteLoginCommand) { c.Organizations = nil }, app.ErrOrganizationRequired, false},
		{"varias organizaciones", true, func(c *app.CompleteLoginCommand) { c.Organizations = []string{"agencia-x", "agencia-z"} }, app.ErrOrganizationRequired, false},
		{"organización sin tenant", true, func(c *app.CompleteLoginCommand) { c.Organizations = []string{"nadie"} }, app.ErrTenantUnavailable, false},
		{"tenant suspendido", true, func(c *app.CompleteLoginCommand) { c.Organizations = []string{"agencia-z"} }, app.ErrTenantUnavailable, true},
		{"sin roles conocidos", true, func(c *app.CompleteLoginCommand) { c.Roles = []string{"Root"} }, app.ErrNoRoles, true},
		{"rol con MFA sin segundo factor", true, func(c *app.CompleteLoginCommand) { c.Roles = []string{"Accounting"} }, app.ErrMFARequired, true},
		{"rol con MFA y amr ausente", true, func(c *app.CompleteLoginCommand) {
			c.Roles = []string{"TenantAdmin"}
			c.AuthMethods = nil
		}, app.ErrMFARequired, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := newStore()
			_, err := handler(store, tt.mfa).Handle(context.Background(), cmd(tt.mut))
			require.ErrorIs(t, err, tt.want)
			assert.Empty(t, store.users, "no se aprovisiona usuario")
			if !tt.wantAudits {
				assert.Empty(t, store.events)
				return
			}
			require.Len(t, store.events, 1)
			assert.Equal(t, audit.OutcomeDenied, store.events[0].Outcome)
			de, ok := kernel.AsDomainError(tt.want)
			require.True(t, ok)
			assert.Equal(t, de.Code(), store.events[0].Details["reason"])
			assert.False(t, store.eventActor[0], "sin usuario autenticado el actor es el sistema")
		})
	}
}

func TestCompleteLoginMFA(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mfa  bool
		amr  []string
		role string
	}{
		{"otp acredita MFA", true, []string{"pwd", "otp"}, "Manager"},
		{"llave de hardware acredita MFA", true, []string{"hwk"}, "ComplianceOfficer"},
		{"rol sin exigencia de MFA", true, []string{"pwd"}, "SalesAgent"},
		{"MFA desactivada (desarrollo)", false, []string{"pwd"}, "TenantAdmin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := handler(newStore(), tt.mfa).Handle(context.Background(), cmd(func(c *app.CompleteLoginCommand) {
				c.Roles = []string{tt.role}
				c.AuthMethods = tt.amr
			}))
			require.NoError(t, err)
		})
	}
}

func TestCompleteLoginRejectsInvalidProfile(t *testing.T) {
	t.Parallel()
	_, err := handler(newStore(), true).Handle(context.Background(), cmd(func(c *app.CompleteLoginCommand) {
		c.Profile = domain.Profile{}
	}))
	require.ErrorIs(t, err, domain.ErrInvalidDisplayName)
}

// DECISIONS N13: Operaciones revela documentos, así que exige MFA.
func TestCompleteLoginRequiresMFAForOperations(t *testing.T) {
	t.Parallel()
	_, err := handler(newStore(), true).Handle(context.Background(), cmd(func(c *app.CompleteLoginCommand) {
		c.Roles = []string{"Operations"}
		c.AuthMethods = []string{"pwd"}
	}))
	require.ErrorIs(t, err, app.ErrMFARequired)
}

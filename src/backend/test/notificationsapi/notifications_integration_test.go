//go:build integration

// Package notificationsapi prueba notificaciones contra PostgreSQL real, con correo y directorios falsos.
package notificationsapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	billing "github.com/marvinrepremarca/seguros/internal/modules/billing/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/notifications"
	"github.com/marvinrepremarca/seguros/internal/modules/notifications/domain"
	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
	"github.com/marvinrepremarca/seguros/test/pgtest"
)

//nolint:gochecknoglobals // compartidos por la suite (TestMain)
var (
	database *db.DB
	super    *pgx.Conn
)

func TestMain(m *testing.M) { os.Exit(runMain(m)) }

func runMain(m *testing.M) int {
	ctx := context.Background()
	env, err := pgtest.Start(ctx)
	if err != nil {
		return 1
	}
	defer env.Terminate() //nolint:errcheck // limpieza al final de la suite
	if err := env.MigrateAll(ctx); err != nil {
		return 1
	}
	if database, err = env.OpenApp(ctx); err != nil {
		return 1
	}
	defer database.Close()
	if super, err = pgx.Connect(ctx, env.SuperuserURL); err != nil {
		return 1
	}
	return m.Run()
}

type principalKey struct{}

type checker struct{ off map[kernel.TenantID]bool }

func (c checker) IsEnabled(ctx context.Context, _ flags.Key) bool {
	t, ok := reqctx.TenantID(ctx)
	return ok && !c.off[t]
}

type policyDir struct{ holder uuid.UUID }

func (d policyDir) Facts(_ context.Context, id uuid.UUID) (policies.PolicyFacts, bool, error) {
	return policies.PolicyFacts{ID: id, Number: "P-77", HolderPartyID: d.holder}, d.holder != uuid.Nil, nil
}

func (policyDir) IDsOfAgent(context.Context, uuid.UUID, int32) ([]uuid.UUID, error) { return nil, nil }

type partyView struct{ rec parties.Contact }

func (v *partyView) Contact(context.Context, uuid.UUID) (parties.Contact, bool, error) {
	return v.rec, true, nil
}

type sender struct {
	sent []domain.Email
	to   []string
	down bool
}

func (s *sender) Send(_ context.Context, to string, e domain.Email) error {
	if s.down {
		return errors.New("smtp caído")
	}
	s.sent, s.to = append(s.sent, e), append(s.to, to)
	return nil
}

type env struct {
	router *chi.Mux
	policy *authz.Policy
	off    map[kernel.TenantID]bool
	mail   *sender
	view   *partyView
	mod    *notifications.Module
	clock  *kernel.FakeClock
}

func newEnv(t *testing.T) *env {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	clock := kernel.NewFakeClock(time.Now().UTC()) //nolint:forbidigo // partición de auditoría del mes actual
	policy, err := authz.DefaultPolicy()
	require.NoError(t, err)
	e := &env{policy: policy, off: map[kernel.TenantID]bool{}, mail: &sender{}, clock: clock, view: &partyView{rec: parties.Contact{Name: "Ana", Email: "ana@example.com"}}}
	e.mod = notifications.New(notifications.Deps{DB: database, Audit: audit.NewRecorder(clock), Sender: e.mail, Policies: policyDir{holder: uuid.Must(uuid.NewV7())}, Parties: e.view, Checker: checker{off: e.off}, Policy: policy, Clock: clock, Logger: logger})
	root := httpx.NewRouter(httpx.RouterOptions{Logger: logger, Clock: clock, MaxBodyBytes: 1 << 20})
	var mountErr error
	root.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ctx := req.Context()
				if p, ok := ctx.Value(principalKey{}).(authz.Principal); ok {
					ctx = reqctx.WithTenantPlan(authz.WithPrincipal(ctx, p), "empresarial")
				}
				next.ServeHTTP(w, req.WithContext(ctx))
			})
		})
		mountErr = e.mod.Mount(r)
	})
	require.NoError(t, mountErr)
	e.router = root
	return e
}

func (e *env) as(tenant kernel.TenantID, roles ...authz.Role) authz.Principal {
	return authz.NewPrincipal(kernel.UserID(uuid.Must(uuid.NewV7())), tenant, roles, e.policy)
}

func newTenant() kernel.TenantID { return kernel.TenantID(uuid.Must(uuid.NewV7())) }

func (e *env) do(t *testing.T, who authz.Principal, method, path string, out any) int {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, path, nil)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req.WithContext(context.WithValue(req.Context(), principalKey{}, who)))
	if out != nil && rec.Code < 300 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), out), rec.Body.String())
	}
	return rec.Code
}

// publish entrega el evento al consumidor real dentro de una transacción del tenant.
func (e *env) publish(t *testing.T, tenant kernel.TenantID, topic string, payload any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	ctx := reqctx.WithTenantID(context.Background(), tenant)
	for _, c := range notifications.Consumers(database, audit.NewRecorder(e.clock), e.clock) {
		if c.Topic != topic {
			continue
		}
		assert.Equal(t, flags.NotificationsModule, c.Flag, "el despachador omite el evento con el flag apagado")
		require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
			return c.Handle(ctx, tx, outbox.Event{ID: uuid.Must(uuid.NewV7()), TenantID: tenant, Topic: topic, Payload: raw})
		}))
		return
	}
	t.Fatalf("sin consumidor de %s", topic)
}

type page struct {
	Items []struct {
		ID         string `json:"id"`
		Status     string `json:"status"`
		SkipReason string `json:"skipReason"`
		Attempts   int    `json:"attempts"`
	} `json:"items"`
}

func due(policy, installment uuid.UUID) billing.InstallmentDue {
	return billing.InstallmentDue{PolicyID: policy, InstallmentID: installment, DueDate: "2026-12-01", DaysLeft: 5}
}

func TestReminderSendsOnceAndIsolatedByTenant(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	policy, installment := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	e.publish(t, tenant, billing.TopicInstallmentDue, due(policy, installment))
	e.publish(t, tenant, billing.TopicInstallmentDue, due(policy, installment))
	ctx := reqctx.WithTenantID(context.Background(), tenant)
	require.NoError(t, e.mod.Service.SendPending(ctx))
	require.NoError(t, e.mod.Service.SendPending(ctx))
	require.Len(t, e.mail.sent, 1, "un recordatorio repetido no se envía dos veces")
	assert.Equal(t, "ana@example.com", e.mail.to[0])
	assert.Contains(t, e.mail.sent[0].Body, "P-77")
	assert.Contains(t, e.mail.sent[0].Body, "2026-12-01")

	var p page
	require.Equal(t, http.StatusOK, e.do(t, e.as(tenant, authz.RoleManager), http.MethodGet, "/api/v1/notifications/messages", &p))
	require.Len(t, p.Items, 1)
	assert.Equal(t, "sent", p.Items[0].Status)

	var other page
	require.Equal(t, http.StatusOK, e.do(t, e.as(newTenant(), authz.RoleManager), http.MethodGet, "/api/v1/notifications/messages", &other))
	assert.Empty(t, other.Items)
	assert.Equal(t, http.StatusForbidden, e.do(t, e.as(tenant, authz.RoleSalesAgent), http.MethodGet, "/api/v1/notifications/messages", nil))
}

func TestSkips(t *testing.T) {
	cases := []struct {
		name, reason string
		rec          parties.Contact
	}{
		{"sin correo", "no_email", parties.Contact{Name: "Ana"}},
		{"correo inválido", "no_email", parties.Contact{Name: "Ana", Email: "x\r\nBcc: y"}},
		{"revocó", "consent_revoked", parties.Contact{Name: "Ana", Email: "a@b.co", Consents: []parties.HabeasConsent{{Status: "revoked", Purpose: "p"}, {Status: "granted", Purpose: "p"}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			e.view.rec = c.rec
			tenant := newTenant()
			e.publish(t, tenant, policies.TopicPolicyIssued, policies.PolicyIssued{PolicyID: uuid.Must(uuid.NewV7())})
			require.NoError(t, e.mod.Service.SendPending(reqctx.WithTenantID(context.Background(), tenant)))
			assert.Empty(t, e.mail.sent)
			var p page
			require.Equal(t, http.StatusOK, e.do(t, e.as(tenant, authz.RoleManager), http.MethodGet, "/api/v1/notifications/messages", &p))
			require.Len(t, p.Items, 1)
			assert.Equal(t, "skipped", p.Items[0].Status)
			assert.Equal(t, c.reason, p.Items[0].SkipReason)
		})
	}
}

func TestConsentGrantedAgainSends(t *testing.T) {
	e := newEnv(t)
	e.view.rec = parties.Contact{Name: "Ana", Email: "a@b.co", Consents: []parties.HabeasConsent{{Status: "granted", Purpose: "p"}, {Status: "revoked", Purpose: "p"}}}
	tenant := newTenant()
	e.publish(t, tenant, policies.TopicPolicyIssued, policies.PolicyIssued{PolicyID: uuid.Must(uuid.NewV7())})
	require.NoError(t, e.mod.Service.SendPending(reqctx.WithTenantID(context.Background(), tenant)))
	assert.Len(t, e.mail.sent, 1)
}

func TestFailureRetriesThenFailsAndCanBeRequeued(t *testing.T) {
	e := newEnv(t)
	e.mail.down = true
	tenant := newTenant()
	ctx := reqctx.WithTenantID(context.Background(), tenant)
	e.publish(t, tenant, policies.TopicPolicyIssued, policies.PolicyIssued{PolicyID: uuid.Must(uuid.NewV7())})
	require.NoError(t, e.mod.Service.SendPending(ctx))
	require.NoError(t, e.mod.Service.SendPending(ctx))
	var waiting page
	require.Equal(t, http.StatusOK, e.do(t, e.as(tenant, authz.RoleManager), http.MethodGet, "/api/v1/notifications/messages", &waiting))
	assert.Equal(t, 1, waiting.Items[0].Attempts, "espera antes de reintentar")
	for range domain.MaxAttempts {
		e.clock.Advance(2 * time.Hour)
		require.NoError(t, e.mod.Service.SendPending(ctx))
	}
	manager := e.as(tenant, authz.RoleManager)
	var p page
	require.Equal(t, http.StatusOK, e.do(t, manager, http.MethodGet, "/api/v1/notifications/messages", &p))
	require.Len(t, p.Items, 1)
	assert.Equal(t, "failed", p.Items[0].Status)
	assert.Equal(t, domain.MaxAttempts, p.Items[0].Attempts)

	retry := "/api/v1/notifications/messages/" + p.Items[0].ID + "/retry"
	assert.Equal(t, http.StatusForbidden, e.do(t, e.as(tenant, authz.RoleOperations), http.MethodPost, retry, nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, e.as(newTenant(), authz.RoleManager), http.MethodPost, retry, nil))
	require.Equal(t, http.StatusNoContent, e.do(t, manager, http.MethodPost, retry, nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, manager, http.MethodPost, retry, nil), "ya no está fallido")
	e.mail.down = false
	require.NoError(t, e.mod.Service.SendPending(ctx))
	assert.Len(t, e.mail.sent, 1)
}

func TestFlagOff(t *testing.T) {
	e := newEnv(t)
	on, off := newTenant(), newTenant()
	e.off[off] = true
	assert.Equal(t, http.StatusOK, e.do(t, e.as(on, authz.RoleManager), http.MethodGet, "/api/v1/notifications/messages", nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, e.as(off, authz.RoleManager), http.MethodGet, "/api/v1/notifications/messages", nil))
}

// Un job que reclamó un lote lo reserva: otro no vuelve a enviar los mismos avisos.
func TestClaimedMessagesAreNotSentTwice(t *testing.T) {
	e := newEnv(t)
	tenant := newTenant()
	ctx := reqctx.WithTenantID(context.Background(), tenant)
	e.publish(t, tenant, policies.TopicPolicyIssued, policies.PolicyIssued{PolicyID: uuid.Must(uuid.NewV7())})
	require.NoError(t, database.WithTenantTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE notifications.messages SET next_attempt_at = $1`, e.clock.Now().Add(time.Hour))
		return err
	}))
	require.NoError(t, e.mod.Service.SendPending(ctx))
	assert.Empty(t, e.mail.sent)
}

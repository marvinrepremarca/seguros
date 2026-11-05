//go:build integration

package moneyapi_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/billing"
	"github.com/marvinrepremarca/seguros/internal/modules/commissions"
	policies "github.com/marvinrepremarca/seguros/internal/modules/policies/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/renewals"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

type resolver struct{ off kernel.TenantID }

func (r resolver) Resolve(_ context.Context, t kernel.TenantID, _ string, _ flags.Key) (flags.Resolution, error) {
	return flags.Resolution{Enabled: t != r.off, Source: flags.SourceOverride}, nil
}

type plans struct{}

func (plans) PlanOf(context.Context, kernel.TenantID) (string, error) { return "empresarial", nil }

// Con el flag apagado, los consumidores reales de los tres módulos no producen efectos; el tenant
// encendido sí, sobre el mismo evento y por el despachador real.
func TestConsumersRespectTheFlagPerTenant(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	clock := kernel.SystemClock{}
	recorder, writer := audit.NewRecorder(clock), outbox.NewWriter(clock)
	on, off := newTenant(), newTenant()
	ps := outbox.NewInProcessPubSub(logger)
	d, err := outbox.NewDispatcher(outbox.DispatcherOptions{
		DB: database, Subscriber: ps, Flags: resolver{off: off}, Plans: plans{}, Clock: clock, Logger: logger, MaxRetries: 1, InitialInterval: time.Millisecond,
	})
	require.NoError(t, err)
	var consumers []outbox.Consumer
	consumers = append(consumers, billing.Consumers(database, recorder, writer, clock)...)
	consumers = append(consumers, commissions.Consumers(database, recorder, &agreements{basis: "paid_premium", pct: "10"}, clock)...)
	consumers = append(consumers, renewals.Consumers(database, recorder, writer, clock)...)
	for _, c := range consumers {
		require.NoError(t, d.Register(c))
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	<-d.Running()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-done)
		require.NoError(t, ps.Close())
	})

	publish := func(tenant kernel.TenantID) {
		raw, err := json.Marshal(policies.PolicyIssued{
			PolicyID: uuid.Must(uuid.NewV7()), PolicyNumber: "P-1", InsurerID: uuid.Must(uuid.NewV7()), LineOfBusinessID: uuid.Must(uuid.NewV7()), HolderPartyID: uuid.Must(uuid.NewV7()),
			Currency: "COP", NetPremium: "100", Taxes: "0", EffectiveFrom: "2026-01-01", EffectiveTo: "2026-12-31", PaymentForm: "single_payment",
		})
		require.NoError(t, err)
		msg := message.NewMessage(uuid.Must(uuid.NewV7()).String(), raw)
		msg.Metadata.Set(outbox.MetaTenantID, tenant.String())
		msg.Metadata.Set(outbox.MetaOccurredAt, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano))
		require.NoError(t, ps.Publish(policies.TopicPolicyIssued, msg))
	}
	publish(on)
	publish(off)

	count := func(tenant kernel.TenantID, query string) int {
		var n int
		c := reqctx.WithTenantID(context.Background(), tenant)
		require.NoError(t, database.WithTenantTx(c, func(tx pgx.Tx) error { return tx.QueryRow(c, query).Scan(&n) }))
		return n
	}
	for _, q := range []string{
		`SELECT count(*) FROM billing.plans`,
		`SELECT count(*) FROM commissions.policy_terms`,
		`SELECT count(*) FROM renewals.cases`,
		`SELECT count(*) FROM messaging.inbox`,
	} {
		assert.Positive(t, count(on, q), "ON: %s", q)
		assert.Zero(t, count(off, q), "OFF: %s", q)
	}
}

func TestRenewalWritesFromAnotherTenantAre404(t *testing.T) {
	e := newEnv(t)
	a, b := newTenant(), newTenant()
	p := e.issue(t, a, flags.RenewalsModule, "2026-12-31", nil)
	other := e.as(b, authz.RoleOperations)
	base := "/api/v1/renewals/cases/" + p.id.String()
	assert.Equal(t, http.StatusNotFound, e.do(t, other, http.MethodPost, base+"/start", nil, nil))
	assert.Equal(t, http.StatusNotFound, e.do(t, other, http.MethodPost, base+"/lost", map[string]any{"reason": "Precio"}, nil))
	assert.Empty(t, e.cases(t, other, ""), "tenant B no ve los casos de A")
}

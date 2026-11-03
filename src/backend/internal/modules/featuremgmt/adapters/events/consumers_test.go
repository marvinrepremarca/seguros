package featuremgmtevents_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	featuremgmtevents "github.com/marvinrepremarca/seguros/internal/modules/featuremgmt/adapters/events"
	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
)

type inv struct {
	got []kernel.TenantID
	err error
}

func (i *inv) Invalidate(_ context.Context, t kernel.TenantID) error {
	i.got = append(i.got, t)
	return i.err
}

func TestCacheInvalidation(t *testing.T) {
	t.Parallel()
	i := &inv{}
	c := featuremgmtevents.CacheInvalidation(i)
	assert.Equal(t, contracts.TopicFeatureToggled, c.Topic)
	assert.Empty(t, c.Flag, "módulo núcleo: sin flag")

	tenant := kernel.TenantID(uuid.Must(uuid.NewV7()))
	payload, err := json.Marshal(contracts.FeatureToggled{FlagKey: "module.claims", Enabled: true})
	require.NoError(t, err)
	require.NoError(t, c.Handle(context.Background(), nil, outbox.Event{TenantID: tenant, Payload: payload}))
	assert.Equal(t, []kernel.TenantID{tenant}, i.got)

	i.err = errors.New("valkey")
	require.Error(t, c.Handle(context.Background(), nil, outbox.Event{TenantID: tenant, Payload: payload}), "se reintenta")
	require.Error(t, c.Handle(context.Background(), nil, outbox.Event{TenantID: tenant, Payload: []byte("{")}))
}

package jobs_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/jobs"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

type tenantArgs struct{ ID uuid.UUID }

func (tenantArgs) Kind() string              { return "test.tenant" }
func (a tenantArgs) Tenant() kernel.TenantID { return kernel.TenantID(a.ID) }

type fakeFlags struct {
	res flags.Resolution
	err error
}

func (f fakeFlags) Resolve(context.Context, kernel.TenantID, string, flags.Key) (flags.Resolution, error) {
	return f.res, f.err
}

type fakePlans struct{ err error }

func (f fakePlans) PlanOf(context.Context, kernel.TenantID) (string, error) { return "basico", f.err }

func TestTenantWorker(t *testing.T) {
	t.Parallel()
	tenant := uuid.Must(uuid.NewV7())
	boom := errors.New("boom")
	tests := []struct {
		name    string
		id      uuid.UUID
		flags   fakeFlags
		plans   fakePlans
		ran     bool
		wantErr bool
	}{
		{name: "encendido ejecuta", id: tenant, flags: fakeFlags{res: flags.Resolution{Enabled: true, Source: flags.SourceOverride}}, ran: true},
		{name: "apagado termina sin efectos", id: tenant, flags: fakeFlags{res: flags.Resolution{Source: flags.SourceOverride}}},
		{name: "flag con error reintenta", id: tenant, flags: fakeFlags{err: boom}, wantErr: true},
		{name: "valor de respaldo por error reintenta", id: tenant, flags: fakeFlags{res: flags.Resolution{Enabled: true, Source: flags.SourceError}}, wantErr: true},
		{name: "plan con error reintenta", id: tenant, plans: fakePlans{err: boom}, wantErr: true},
		{name: "sin tenant cancela", id: uuid.Nil, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ran := false
			w := &jobs.TenantWorker[tenantArgs]{
				Flag: flags.BillingModule, Flags: tt.flags, Plans: tt.plans, Logger: slog.New(slog.DiscardHandler),
				Do: func(ctx context.Context, _ *river.Job[tenantArgs]) error {
					got, ok := reqctx.TenantID(ctx)
					assert.True(t, ok)
					assert.Equal(t, kernel.TenantID(tenant), got)
					ran = true
					return nil
				},
			}
			err := w.Work(context.Background(), &river.Job[tenantArgs]{Args: tenantArgs{ID: tt.id}})
			assert.Equal(t, tt.ran, ran)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

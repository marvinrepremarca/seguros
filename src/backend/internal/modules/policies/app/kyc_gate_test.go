package app_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/app"
	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

type flagStore struct {
	overrides map[flags.Key]bool
	global    map[flags.Key]bool
	err       error
}

func (s *flagStore) TenantOverrides(context.Context, kernel.TenantID) (map[flags.Key]bool, error) {
	return s.overrides, s.err
}

func (s *flagStore) GlobalSwitches(context.Context) (map[flags.Key]bool, error) {
	return s.global, s.err
}

type kyc struct {
	approved bool
	err      error
}

func (k kyc) IsApproved(context.Context, uuid.UUID) (bool, error) { return k.approved, k.err }

type fixedResolver struct {
	res flags.Resolution
	err error
}

func (f fixedResolver) Resolve(context.Context, kernel.TenantID, string, flags.Key) (flags.Resolution, error) {
	return f.res, f.err
}

func tenantCtx(plan string) context.Context {
	ctx := reqctx.WithTenantID(context.Background(), kernel.TenantID(uuid.Must(uuid.NewV7())))
	return reqctx.WithTenantPlan(ctx, plan)
}

// ADR 0012 con el evaluador real: "apagado" y "no se pudo evaluar" son decisiones distintas.
func TestKycGateWithRealEvaluator(t *testing.T) {
	t.Parallel()
	reg, err := flags.LoadRegistry()
	require.NoError(t, err)
	cases := []struct {
		name      string
		plan      string
		store     *flagStore
		kyc       kyc
		clearance domain.KycClearance
		source    flags.Source
		err       error
	}{
		{"plan sin el módulo: no se exige", "profesional", &flagStore{}, kyc{}, domain.KycNotRequired, flags.SourceDefault, nil},
		{"plan con el módulo y sin KYC: bloquea", "empresarial", &flagStore{}, kyc{}, domain.KycNotApproved, flags.SourcePlan, nil},
		{"plan con el módulo y KYC aprobado", "empresarial", &flagStore{}, kyc{approved: true}, domain.KycApproved, flags.SourcePlan, nil},
		{"add-on en plan menor: se exige", "basico", &flagStore{overrides: map[flags.Key]bool{flags.ComplianceModule: true}}, kyc{}, domain.KycNotApproved, flags.SourceOverride, nil},
		{"override apagado: decisión comercial", "empresarial", &flagStore{overrides: map[flags.Key]bool{flags.ComplianceModule: false}}, kyc{}, domain.KycNotRequired, flags.SourceOverride, nil},
		{"kill switch: no se exige y queda el origen", "empresarial", &flagStore{global: map[flags.Key]bool{flags.ComplianceModule: false}}, kyc{}, domain.KycNotRequired, flags.SourceKillSwitch, nil},
		{"BD de flags caída: bloquea aunque el plan no lo incluya", "basico", &flagStore{err: errors.New("caída")}, kyc{}, domain.KycUndetermined, flags.SourceError, domain.ErrComplianceUnavailable},
		{"BD de flags caída con plan empresarial: bloquea", "empresarial", &flagStore{err: errors.New("caída")}, kyc{approved: true}, domain.KycUndetermined, flags.SourceError, domain.ErrComplianceUnavailable},
		{"sin plan en el contexto: bloquea (caería al default apagado)", "", &flagStore{}, kyc{}, domain.KycUndetermined, flags.SourceError, domain.ErrComplianceUnavailable},
		{"consulta de KYC falla: bloquea", "empresarial", &flagStore{}, kyc{err: errors.New("caída")}, domain.KycUndetermined, flags.SourcePlan, domain.ErrComplianceUnavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			clock := kernel.NewFakeClock(time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC))
			eval := flags.NewEvaluator(reg, c.store, nil, clock, slog.New(slog.DiscardHandler))
			gate := &app.KycGate{Flags: eval, Kyc: c.kyc, Logger: slog.New(slog.DiscardHandler)}
			clearance, source, err := gate.Clearance(tenantCtx(c.plan), uuid.New())
			if c.err != nil {
				require.ErrorIs(t, err, c.err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, c.clearance, clearance)
			assert.Equal(t, c.source, source)
		})
	}
}

func TestKycGateFailsOnEdgeResolutions(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.DiscardHandler)
	gate := &app.KycGate{Flags: fixedResolver{res: flags.Resolution{Source: flags.SourceError}}, Kyc: kyc{approved: true}, Logger: logger}
	_, _, err := gate.Clearance(tenantCtx("empresarial"), uuid.New())
	require.ErrorIs(t, err, domain.ErrComplianceUnavailable, "SourceError sin error también bloquea")

	_, _, err = gate.Clearance(reqctx.WithTenantPlan(context.Background(), "empresarial"), uuid.New())
	require.ErrorIs(t, err, domain.ErrComplianceUnavailable, "sin tenant")
}

// El último valor conocido (L1) sigue siendo válido si la lectura falla (ARCHITECTURE.md §7).
func TestKycGateUsesLastKnownValue(t *testing.T) {
	t.Parallel()
	reg, err := flags.LoadRegistry()
	require.NoError(t, err)
	clock := kernel.NewFakeClock(time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC))
	store := &flagStore{}
	logger := slog.New(slog.DiscardHandler)
	gate := &app.KycGate{Flags: flags.NewEvaluator(reg, store, nil, clock, logger), Kyc: kyc{}, Logger: logger}
	ctx := tenantCtx("empresarial")
	clearance, _, err := gate.Clearance(ctx, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, domain.KycNotApproved, clearance)

	store.err = errors.New("caída")
	clock.Advance(time.Minute)
	clearance, _, err = gate.Clearance(ctx, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, domain.KycNotApproved, clearance, "sigue exigiendo el KYC")
}

func TestNoKycRecordsNeverApproves(t *testing.T) {
	t.Parallel()
	approved, err := app.NoKycRecords{}.IsApproved(context.Background(), uuid.New())
	require.NoError(t, err)
	assert.False(t, approved, "hasta Compliance (Fase 5) nadie tiene KYC aprobado")
}

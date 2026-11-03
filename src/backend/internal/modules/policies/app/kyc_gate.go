package app

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/policies/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/flags"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// FlagResolver evalúa un flag distinguiendo "apagado" de "no se pudo evaluar" (flags.Evaluator).
type FlagResolver interface {
	Resolve(ctx context.Context, tenant kernel.TenantID, plan string, key flags.Key) (flags.Resolution, error)
}

// KycStatuses responde si el tomador tiene un KycCheck aprobado.
type KycStatuses interface {
	IsApproved(ctx context.Context, party uuid.UUID) (bool, error)
}

// NoKycRecords es la implementación mientras no exista Compliance (Fase 5): ningún tomador tiene un
// KycCheck aprobado, de modo que un tenant con module.compliance encendido no puede expedir. Es la
// consecuencia de N4 (sin excepciones), no un atajo: nunca debe devolver true.
type NoKycRecords struct{}

// IsApproved implementa KycStatuses.
func (NoKycRecords) IsApproved(context.Context, uuid.UUID) (bool, error) { return false, nil }

// KycGate decide si el KYC del tomador permite expedir (DECISIONS N4, ADR 0012). module.compliance
// activa un control: "apagado" y "no se pudo evaluar" son distintos, y lo segundo bloquea. Aquí está
// el único punto de decisión de este flag en la capa de aplicación.
type KycGate struct {
	Flags  FlagResolver
	Kyc    KycStatuses
	Logger *slog.Logger
}

// Clearance devuelve la decisión y el origen del valor del flag (evidencia para la auditoría), o
// domain.ErrComplianceUnavailable si no se puede determinar.
func (g *KycGate) Clearance(ctx context.Context, holder uuid.UUID) (domain.KycClearance, flags.Source, error) {
	tenant, ok := reqctx.TenantID(ctx)
	plan := reqctx.TenantPlan(ctx)
	if !ok || plan == "" {
		// Sin plan el flag caería al default apagado y se omitiría el control.
		g.Logger.WarnContext(ctx, "expedición bloqueada: evaluación de module.compliance sin tenant o plan")
		return domain.KycUndetermined, flags.SourceError, domain.ErrComplianceUnavailable
	}
	res, err := g.Flags.Resolve(ctx, tenant, plan, flags.ComplianceModule)
	if err != nil || res.Source == flags.SourceError {
		g.Logger.WarnContext(ctx, "expedición bloqueada: module.compliance no se pudo evaluar", "error", err)
		return domain.KycUndetermined, flags.SourceError, domain.ErrComplianceUnavailable
	}
	if !res.Enabled {
		return domain.KycNotRequired, res.Source, nil
	}
	approved, err := g.Kyc.IsApproved(ctx, holder)
	if err != nil {
		g.Logger.WarnContext(ctx, "expedición bloqueada: no se pudo consultar el KYC", "error", err)
		return domain.KycUndetermined, res.Source, domain.ErrComplianceUnavailable
	}
	if approved {
		return domain.KycApproved, res.Source, nil
	}
	return domain.KycNotApproved, res.Source, nil
}

package flags

import (
	"context"

	"github.com/google/uuid"
	"github.com/open-feature/go-sdk/openfeature"

	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Atributos del contexto de evaluación OpenFeature (rules/feature-flags.md §Evaluación).
const (
	AttrTenantID = "tenantId"
	AttrPlan     = "plan"
	AttrUserID   = "userId"
	AttrRoles    = "roles"
)

// ProviderName es el nombre del proveedor propio "tenantflags".
const ProviderName = "tenantflags"

// Provider es el proveedor OpenFeature respaldado por el Evaluator. Solo resuelve booleanos; los
// demás tipos devuelven TYPE_MISMATCH (fail-closed con el default).
type Provider struct {
	eval *Evaluator
}

var _ openfeature.FeatureProvider = (*Provider)(nil)

// NewProvider construye el proveedor.
func NewProvider(eval *Evaluator) *Provider { return &Provider{eval: eval} }

// Metadata implementa openfeature.FeatureProvider.
func (p *Provider) Metadata() openfeature.Metadata { return openfeature.Metadata{Name: ProviderName} }

// Hooks implementa openfeature.FeatureProvider.
func (p *Provider) Hooks() []openfeature.Hook { return nil }

// BooleanEvaluation implementa openfeature.FeatureProvider.
func (p *Provider) BooleanEvaluation(ctx context.Context, flag string, defaultValue bool, flat openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
	key := Key(flag)
	def, ok := p.eval.reg.Get(key)
	if !ok {
		return boolError(defaultValue, openfeature.NewFlagNotFoundResolutionError("flag no registrado"))
	}
	var tenant kernel.TenantID
	if raw, ok := flat[AttrTenantID].(string); ok && raw != "" {
		u, err := uuid.Parse(raw)
		if err != nil {
			return boolError(defaultValue, openfeature.NewInvalidContextResolutionError("tenantId inválido"))
		}
		tenant = kernel.TenantID(u)
	} else if def.Kind != KindOps {
		return boolError(defaultValue, openfeature.NewTargetingKeyMissingResolutionError("falta tenantId"))
	}
	plan, ok := flat[AttrPlan].(string)
	if !ok {
		plan = ""
	}
	res, err := p.eval.Resolve(ctx, tenant, plan, key)
	if err != nil {
		return boolError(defaultValue, openfeature.NewGeneralResolutionError("no se pudo evaluar"))
	}
	reason := openfeature.TargetingMatchReason
	switch res.Source {
	case SourceDefault:
		reason = openfeature.DefaultReason
	case SourceKillSwitch, SourceDependency:
		reason = openfeature.DisabledReason
	case SourceGlobal:
		reason = openfeature.StaticReason
	case SourceOverride, SourcePlan, SourceError:
	}
	return openfeature.BoolResolutionDetail{
		Value: res.Enabled,
		ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
			Reason: reason, Variant: string(res.Source),
		},
	}
}

func boolError(def bool, err openfeature.ResolutionError) openfeature.BoolResolutionDetail {
	return openfeature.BoolResolutionDetail{
		Value:                    def,
		ProviderResolutionDetail: openfeature.ProviderResolutionDetail{ResolutionError: err, Reason: openfeature.ErrorReason},
	}
}

func typeMismatch() openfeature.ProviderResolutionDetail {
	return openfeature.ProviderResolutionDetail{
		ResolutionError: openfeature.NewTypeMismatchResolutionError("solo hay flags booleanos"),
		Reason:          openfeature.ErrorReason,
	}
}

// StringEvaluation implementa openfeature.FeatureProvider.
func (p *Provider) StringEvaluation(_ context.Context, _ string, def string, _ openfeature.FlattenedContext) openfeature.StringResolutionDetail {
	return openfeature.StringResolutionDetail{Value: def, ProviderResolutionDetail: typeMismatch()}
}

// FloatEvaluation implementa openfeature.FeatureProvider (firma impuesta por la interfaz; no hay
// flags numéricos ni dinero aquí).
func (p *Provider) FloatEvaluation(_ context.Context, _ string, def float64, _ openfeature.FlattenedContext) openfeature.FloatResolutionDetail { //nolint:forbidigo // firma de la interfaz OpenFeature
	return openfeature.FloatResolutionDetail{Value: def, ProviderResolutionDetail: typeMismatch()}
}

// IntEvaluation implementa openfeature.FeatureProvider.
func (p *Provider) IntEvaluation(_ context.Context, _ string, def int64, _ openfeature.FlattenedContext) openfeature.IntResolutionDetail {
	return openfeature.IntResolutionDetail{Value: def, ProviderResolutionDetail: typeMismatch()}
}

// ObjectEvaluation implementa openfeature.FeatureProvider.
func (p *Provider) ObjectEvaluation(_ context.Context, _ string, def any, _ openfeature.FlattenedContext) openfeature.InterfaceResolutionDetail {
	return openfeature.InterfaceResolutionDetail{Value: def, ProviderResolutionDetail: typeMismatch()}
}

// OpenFeatureChecker implementa Checker con un cliente OpenFeature, construyendo el contexto de
// evaluación (tenantId, plan, userId, roles) desde la identidad autenticada.
type OpenFeatureChecker struct {
	client *openfeature.Client
}

var _ Checker = (*OpenFeatureChecker)(nil)

// NewOpenFeatureChecker construye el checker.
func NewOpenFeatureChecker(client *openfeature.Client) *OpenFeatureChecker {
	return &OpenFeatureChecker{client: client}
}

// IsEnabled implementa Checker (default false: fail-closed).
func (c *OpenFeatureChecker) IsEnabled(ctx context.Context, key Key) bool {
	v, err := c.client.BooleanValue(ctx, string(key), false, EvaluationContext(ctx))
	return err == nil && v
}

// EvaluationContext construye el contexto OpenFeature desde la petición autenticada.
func EvaluationContext(ctx context.Context) openfeature.EvaluationContext {
	attrs := map[string]any{AttrPlan: reqctx.TenantPlan(ctx)}
	targeting := ""
	if t, ok := reqctx.TenantID(ctx); ok {
		targeting = t.String()
		attrs[AttrTenantID] = targeting
	}
	if p, ok := authz.PrincipalFrom(ctx); ok {
		attrs[AttrUserID] = kernel.IDString(p.UserID)
		roles := make([]string, len(p.Roles))
		for i, r := range p.Roles {
			roles[i] = string(r)
		}
		attrs[AttrRoles] = roles
	}
	return openfeature.NewEvaluationContext(targeting, attrs)
}

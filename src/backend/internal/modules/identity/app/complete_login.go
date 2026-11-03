package app

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/marvinrepremarca/seguros/internal/modules/identity/domain"
	tenancy "github.com/marvinrepremarca/seguros/internal/modules/tenancy/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

// Errores del login (KindForbidden → 403). Un tenant inexistente y uno suspendido comparten código
// para no revelar qué organizaciones existen.
var (
	ErrOrganizationRequired = kernel.NewError(kernel.KindForbidden, "identity.login.organization_required", "el usuario debe pertenecer a exactamente una organización")
	ErrTenantUnavailable    = kernel.NewError(kernel.KindForbidden, "identity.login.tenant_unavailable", "la organización no tiene un tenant activo")
	ErrNoRoles              = kernel.NewError(kernel.KindForbidden, "identity.login.no_roles", "el usuario no tiene roles asignados")
	ErrMFARequired          = kernel.NewError(kernel.KindForbidden, "identity.login.mfa_required", "el rol exige autenticación multifactor")
)

// Valores del claim amr (RFC 8176) que acreditan un segundo factor.
func mfaMethods() []string { return []string{"otp", "mfa", "hwk", "swk"} }

// CompleteLoginCommand son los datos ya verificados del ID token (firma, iss, aud, exp, nonce).
type CompleteLoginCommand struct {
	Subject       string
	Profile       domain.Profile
	Roles         []string
	Organizations []string
	AuthMethods   []string
}

// CompleteLoginResult es la identidad con la que se crea la sesión.
type CompleteLoginResult struct {
	UserID      kernel.UserID
	TenantID    kernel.TenantID
	Plan        string
	Roles       []authz.Role
	DisplayName string
}

// CompleteLoginHandler completa el login.
type CompleteLoginHandler struct {
	tenants     tenancy.Directory
	store       Store
	clock       kernel.Clock
	mfaRequired bool
}

// NewCompleteLoginHandler construye el handler. mfaRequired=false solo en desarrollo (config).
func NewCompleteLoginHandler(tenants tenancy.Directory, store Store, clock kernel.Clock, mfaRequired bool) *CompleteLoginHandler {
	return &CompleteLoginHandler{tenants: tenants, store: store, clock: clock, mfaRequired: mfaRequired}
}

// Handle resuelve el tenant de la organización, aplica la política de acceso, aprovisiona o
// actualiza el usuario y audita el resultado en la misma transacción.
func (h *CompleteLoginHandler) Handle(ctx context.Context, cmd CompleteLoginCommand) (CompleteLoginResult, error) {
	if len(cmd.Organizations) != 1 {
		return CompleteLoginResult{}, ErrOrganizationRequired
	}
	tenant, err := h.tenants.ResolveByOrgAlias(ctx, cmd.Organizations[0])
	if errors.Is(err, tenancy.ErrTenantNotFound) {
		return CompleteLoginResult{}, ErrTenantUnavailable
	}
	if err != nil {
		return CompleteLoginResult{}, fmt.Errorf("identity: resolviendo tenant: %w", err)
	}
	ctx = reqctx.WithTenantID(ctx, tenant.ID)

	roles := authz.ParseRoles(cmd.Roles)
	if denial := h.accessDenial(tenant, roles, cmd.AuthMethods); denial != nil {
		if err := h.auditDenied(ctx, cmd.Subject, denial); err != nil {
			return CompleteLoginResult{}, err
		}
		return CompleteLoginResult{}, denial
	}

	var user *domain.User
	for attempt := 0; ; attempt++ {
		user, err = h.provision(ctx, tenant.ID, cmd)
		if !errors.Is(err, domain.ErrConcurrentUpdate) || attempt == 1 {
			break
		}
	}
	if err != nil {
		return CompleteLoginResult{}, err
	}
	return CompleteLoginResult{
		UserID: user.ID(), TenantID: tenant.ID, Plan: tenant.Plan, Roles: roles,
		DisplayName: user.Profile().DisplayName,
	}, nil
}

func (h *CompleteLoginHandler) accessDenial(tenant tenancy.TenantInfo, roles []authz.Role, amr []string) *kernel.Error {
	switch {
	case !tenant.Active:
		return ErrTenantUnavailable
	case len(roles) == 0:
		return ErrNoRoles
	case h.mfaRequired && needsMFA(roles) && !hasMFA(amr):
		return ErrMFARequired
	default:
		return nil
	}
}

func needsMFA(roles []authz.Role) bool {
	return slices.ContainsFunc(authz.MFARoles(), func(r authz.Role) bool { return slices.Contains(roles, r) })
}

func hasMFA(amr []string) bool {
	return slices.ContainsFunc(amr, func(m string) bool { return slices.Contains(mfaMethods(), m) })
}

func (h *CompleteLoginHandler) provision(ctx context.Context, tenant kernel.TenantID, cmd CompleteLoginCommand) (*domain.User, error) {
	var user *domain.User
	err := h.store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		now := h.clock.Now()
		u, err := uow.Users().GetBySubject(ctx, tenant, cmd.Subject)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			id, idErr := kernel.NewID[kernel.UserID]()
			if idErr != nil {
				return idErr
			}
			if u, err = domain.NewUser(id, tenant, cmd.Subject, cmd.Profile, now); err != nil {
				return err
			}
			if err := uow.Users().Insert(ctx, u); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if err := u.RecordLogin(cmd.Profile, now); err != nil {
				return err
			}
			if err := uow.Users().Update(ctx, u); err != nil {
				return err
			}
		}
		user = u
		ctx = reqctx.WithUserID(ctx, u.ID())
		return uow.Audit(ctx, audit.Entry{
			Action: "identity.session.login", ResourceType: "identity.user", ResourceID: kernel.IDString(u.ID()),
			Outcome: audit.OutcomeSuccess, Details: map[string]string{"method": "oidc"},
		})
	})
	if err != nil {
		return nil, fmt.Errorf("identity: aprovisionando usuario: %w", err)
	}
	return user, nil
}

func (h *CompleteLoginHandler) auditDenied(ctx context.Context, subject string, reason *kernel.Error) error {
	err := h.store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		return uow.Audit(ctx, audit.Entry{
			Action: "identity.session.login", ResourceType: "identity.user", Outcome: audit.OutcomeDenied,
			Details: map[string]string{"reason": reason.Code(), "idpSubject": truncate(subject, 200)},
		})
	})
	if err != nil {
		return fmt.Errorf("identity: auditando login denegado: %w", err)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

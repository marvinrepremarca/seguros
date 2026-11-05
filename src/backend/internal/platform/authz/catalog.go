package authz

// Role es un rol base de DOMAIN.md §4. Lo asigna Keycloak (claim `roles`); los roles
// desconocidos se descartan.
type Role string

// Roles base.
const (
	RoleTenantAdmin       Role = "TenantAdmin"
	RoleManager           Role = "Manager"
	RoleSalesAgent        Role = "SalesAgent"
	RoleOperations        Role = "Operations"
	RoleClaimsAnalyst     Role = "ClaimsAnalyst"
	RoleCollections       Role = "Collections"
	RoleAccounting        Role = "Accounting"
	RoleComplianceOfficer Role = "ComplianceOfficer"
)

// AllRoles devuelve los roles base en orden estable.
func AllRoles() []Role {
	return []Role{
		RoleTenantAdmin, RoleManager, RoleSalesAgent, RoleOperations,
		RoleClaimsAnalyst, RoleCollections, RoleAccounting, RoleComplianceOfficer,
	}
}

// MFARoles son los roles que exigen MFA (rules/security.md §Autenticación).
func MFARoles() []Role {
	return []Role{RoleTenantAdmin, RoleManager, RoleOperations, RoleCollections, RoleAccounting, RoleComplianceOfficer}
}

// Permission es un permiso fino `<modulo>.<recurso>.<accion>`.
type Permission string

// Catálogo de permisos. Cada módulo añade aquí los suyos (y su asignación en DefaultGrants) en el
// mismo cambio que crea la operación que los exige.
const (
	FeatureFlagRead   Permission = "featuremgmt.flag.read"
	FeatureFlagUpdate Permission = "featuremgmt.flag.update"

	PartyRead       Permission = "parties.party.read"
	PartyReadOwn    Permission = "parties.party.read-own"
	PartyCreate     Permission = "parties.party.create"
	PartyUpdate     Permission = "parties.party.update"
	PartyUpdateOwn  Permission = "parties.party.update-own"
	PartyAssign     Permission = "parties.party.assign"
	DocumentRectify Permission = "parties.document.rectify"
	DocumentReveal  Permission = "parties.document.reveal"
	ConsentRead     Permission = "parties.consent.read"
	ConsentCreate   Permission = "parties.consent.create"

	CatalogRead     Permission = "catalog.item.read"
	CatalogManage   Permission = "catalog.item.manage"
	AgreementRead   Permission = "catalog.commission-agreement.read"
	AgreementManage Permission = "catalog.commission-agreement.manage"

	PolicyRead           Permission = "policies.policy.read"
	PolicyReadOwn        Permission = "policies.policy.read-own"
	PolicyManage         Permission = "policies.policy.manage"
	PolicyCancel         Permission = "policies.policy.cancel"
	PolicyAssign         Permission = "policies.policy.assign"
	EndorsementManage    Permission = "policies.endorsement.manage"
	PolicySettingsManage Permission = "policies.settings.manage"

	BillingPlanRead         Permission = "billing.plan.read"
	BillingPlanAdjust       Permission = "billing.plan.adjust"
	BillingPaymentRegister  Permission = "billing.payment.register"
	BillingPaymentReverse   Permission = "billing.payment.reverse"
	BillingCollectionManage Permission = "billing.collection.manage"
	BillingSettingsManage   Permission = "billing.settings.manage"

	CommissionRead            Permission = "commissions.accrual.read"
	CommissionStatementManage Permission = "commissions.statement.manage"
	CommissionRateManage      Permission = "commissions.rate.manage"

	RenewalRead           Permission = "renewals.case.read"
	RenewalManage         Permission = "renewals.case.manage"
	RenewalSettingsManage Permission = "renewals.settings.manage"
)

// DefaultGrants es la asignación base rol → permisos (DOMAIN.md §4). Se devuelve una copia nueva
// en cada llamada (sin estado global mutable).
func DefaultGrants() map[Role][]Permission {
	return map[Role][]Permission{
		RoleTenantAdmin:       {FeatureFlagRead, FeatureFlagUpdate, CatalogRead, CatalogManage, AgreementRead, AgreementManage, BillingSettingsManage, RenewalSettingsManage, PolicySettingsManage},
		RoleManager:           {FeatureFlagRead, PartyRead, DocumentReveal, ConsentRead, CatalogRead, AgreementRead, AgreementManage, PolicyRead, PartyAssign, PolicyAssign, BillingPlanRead, CommissionRead, CommissionRateManage, RenewalRead},
		RoleSalesAgent:        {PartyReadOwn, PartyCreate, PartyUpdateOwn, ConsentRead, ConsentCreate, CatalogRead, PolicyReadOwn},
		RoleOperations:        {PartyRead, PartyCreate, PartyUpdate, DocumentRectify, DocumentReveal, ConsentRead, ConsentCreate, CatalogRead, PolicyRead, PolicyManage, PolicyCancel, EndorsementManage, PartyAssign, PolicyAssign, BillingPlanRead, BillingPlanAdjust, RenewalRead, RenewalManage},
		RoleClaimsAnalyst:     {PartyRead, CatalogRead, PolicyRead},
		RoleCollections:       {PartyRead, CatalogRead, PolicyRead, BillingPlanRead, BillingPaymentRegister, BillingPaymentReverse, BillingCollectionManage},
		RoleAccounting:        {PartyRead, CatalogRead, AgreementRead, PolicyRead, BillingPlanRead, CommissionRead, CommissionStatementManage},
		RoleComplianceOfficer: {PartyRead, PartyUpdate, DocumentRectify, DocumentReveal, ConsentRead, ConsentCreate, CatalogRead, PolicyRead},
	}
}

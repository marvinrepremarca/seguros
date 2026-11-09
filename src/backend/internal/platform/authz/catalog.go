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
	return []Role{RoleTenantAdmin, RoleManager, RoleOperations, RoleCollections, RoleAccounting, RoleComplianceOfficer, RoleClaimsAnalyst}
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

	LeadRead      Permission = "crm.lead.read"
	LeadReadOwn   Permission = "crm.lead.read-own"
	LeadCreate    Permission = "crm.lead.create"
	LeadManageOwn Permission = "crm.lead.manage-own"
	LeadAssign    Permission = "crm.lead.assign"

	QuoteRead           Permission = "quoting.quote.read"
	QuoteReadOwn        Permission = "quoting.quote.read-own"
	QuoteManageOwn      Permission = "quoting.quote.manage-own"
	QuoteSettingsManage Permission = "quoting.settings.manage"

	ClaimRead            Permission = "claims.claim.read"
	ClaimReadOwn         Permission = "claims.claim.read-own"
	ClaimReport          Permission = "claims.claim.report"
	ClaimManage          Permission = "claims.claim.manage"
	ClaimOverridePayment Permission = "claims.claim.override-payment"
	ClaimHealthRead      Permission = "claims.health.read"
	ClaimHealthWrite     Permission = "claims.health.write"

	KycRead         Permission = "compliance.kyc.read"
	KycRecord       Permission = "compliance.kyc.record"
	KycDetailsRead  Permission = "compliance.kyc.read-details"
	ComplaintManage Permission = "compliance.complaint.manage"
	HabeasExport    Permission = "compliance.habeas.export"
	HabeasSuppress  Permission = "compliance.habeas.suppress"

	DocumentRead            Permission = "documents.document.read"
	DocumentUpload          Permission = "documents.document.upload"
	DocumentSensitive       Permission = "documents.document.sensitive"
	DocumentUploadSensitive Permission = "documents.document.upload-sensitive"
	DocumentRetention       Permission = "documents.retention.read"

	NotificationRead   Permission = "notifications.message.read"
	NotificationManage Permission = "notifications.message.manage"

	ReportingRead Permission = "reporting.kpi.read"

	IntegrationRead  Permission = "integrations.connector.read"
	IntegrationQuote Permission = "integrations.quote.request"
)

// DefaultGrants es la asignación base rol → permisos (DOMAIN.md §4). Se devuelve una copia nueva
// en cada llamada (sin estado global mutable).
func DefaultGrants() map[Role][]Permission {
	return map[Role][]Permission{
		RoleTenantAdmin:       {IntegrationRead, NotificationRead, NotificationManage, FeatureFlagRead, FeatureFlagUpdate, CatalogRead, CatalogManage, AgreementRead, AgreementManage, BillingSettingsManage, RenewalSettingsManage, PolicySettingsManage, QuoteSettingsManage},
		RoleManager:           {ReportingRead, IntegrationRead, NotificationRead, NotificationManage, DocumentRead, DocumentSensitive, DocumentRetention, FeatureFlagRead, PartyRead, DocumentReveal, ConsentRead, CatalogRead, AgreementRead, AgreementManage, PolicyRead, PartyAssign, PolicyAssign, BillingPlanRead, CommissionRead, CommissionRateManage, RenewalRead, LeadRead, LeadCreate, LeadAssign, QuoteRead, ClaimRead, ClaimOverridePayment, ClaimHealthRead},
		RoleSalesAgent:        {PartyReadOwn, PartyCreate, PartyUpdateOwn, ConsentRead, ConsentCreate, CatalogRead, PolicyReadOwn, LeadReadOwn, LeadCreate, LeadManageOwn, QuoteReadOwn, QuoteManageOwn, ClaimReadOwn, ClaimReport},
		RoleOperations:        {DocumentUploadSensitive, IntegrationRead, IntegrationQuote, NotificationRead, DocumentRead, DocumentUpload, PartyRead, PartyCreate, PartyUpdate, DocumentRectify, DocumentReveal, ConsentRead, ConsentCreate, CatalogRead, PolicyRead, PolicyManage, PolicyCancel, EndorsementManage, PartyAssign, PolicyAssign, BillingPlanRead, BillingPlanAdjust, RenewalRead, RenewalManage, LeadRead, LeadAssign, QuoteRead, KycRead},
		RoleClaimsAnalyst:     {DocumentUploadSensitive, DocumentRead, DocumentUpload, DocumentSensitive, PartyRead, CatalogRead, PolicyRead, ClaimRead, ClaimReport, ClaimManage, ClaimHealthRead, ClaimHealthWrite},
		RoleCollections:       {NotificationRead, PartyRead, CatalogRead, PolicyRead, BillingPlanRead, BillingPaymentRegister, BillingPaymentReverse, BillingCollectionManage},
		RoleAccounting:        {ReportingRead, PartyRead, CatalogRead, AgreementRead, PolicyRead, BillingPlanRead, CommissionRead, CommissionStatementManage},
		RoleComplianceOfficer: {DocumentUploadSensitive, DocumentRead, DocumentUpload, DocumentSensitive, DocumentRetention, PartyRead, PartyUpdate, DocumentRectify, DocumentReveal, ConsentRead, ConsentCreate, CatalogRead, PolicyRead, KycRead, KycRecord, KycDetailsRead, ComplaintManage, HabeasExport, HabeasSuppress},
	}
}

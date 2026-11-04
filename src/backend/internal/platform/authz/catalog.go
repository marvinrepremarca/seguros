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
	return []Role{RoleTenantAdmin, RoleManager, RoleOperations, RoleAccounting, RoleComplianceOfficer}
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

	PolicyRead        Permission = "policies.policy.read"
	PolicyReadOwn     Permission = "policies.policy.read-own"
	PolicyManage      Permission = "policies.policy.manage"
	PolicyCancel      Permission = "policies.policy.cancel"
	PolicyAssign      Permission = "policies.policy.assign"
	EndorsementManage Permission = "policies.endorsement.manage"
)

// DefaultGrants es la asignación base rol → permisos (DOMAIN.md §4). Se devuelve una copia nueva
// en cada llamada (sin estado global mutable).
func DefaultGrants() map[Role][]Permission {
	return map[Role][]Permission{
		RoleTenantAdmin:       {FeatureFlagRead, FeatureFlagUpdate, CatalogRead, CatalogManage, AgreementRead, AgreementManage},
		RoleManager:           {FeatureFlagRead, PartyRead, DocumentReveal, ConsentRead, CatalogRead, AgreementRead, AgreementManage, PolicyRead, PartyAssign, PolicyAssign},
		RoleSalesAgent:        {PartyReadOwn, PartyCreate, PartyUpdateOwn, ConsentRead, ConsentCreate, CatalogRead, PolicyReadOwn},
		RoleOperations:        {PartyRead, PartyCreate, PartyUpdate, DocumentRectify, DocumentReveal, ConsentRead, ConsentCreate, CatalogRead, PolicyRead, PolicyManage, PolicyCancel, EndorsementManage, PartyAssign, PolicyAssign},
		RoleClaimsAnalyst:     {PartyRead, CatalogRead, PolicyRead},
		RoleCollections:       {PartyRead, CatalogRead, PolicyRead},
		RoleAccounting:        {PartyRead, CatalogRead, AgreementRead, PolicyRead},
		RoleComplianceOfficer: {PartyRead, PartyUpdate, DocumentRectify, DocumentReveal, ConsentRead, ConsentCreate, CatalogRead, PolicyRead},
	}
}

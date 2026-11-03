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
	return []Role{RoleTenantAdmin, RoleManager, RoleAccounting, RoleComplianceOfficer}
}

// Permission es un permiso fino `<modulo>.<recurso>.<accion>`.
type Permission string

// Catálogo de permisos. Cada módulo añade aquí los suyos (y su asignación en DefaultGrants) en el
// mismo cambio que crea la operación que los exige.
const (
	FeatureFlagRead   Permission = "featuremgmt.flag.read"
	FeatureFlagUpdate Permission = "featuremgmt.flag.update"
)

// DefaultGrants es la asignación base rol → permisos (DOMAIN.md §4). Se devuelve una copia nueva
// en cada llamada (sin estado global mutable).
func DefaultGrants() map[Role][]Permission {
	return map[Role][]Permission{
		RoleTenantAdmin: {FeatureFlagRead, FeatureFlagUpdate},
		RoleManager:     {FeatureFlagRead},
	}
}

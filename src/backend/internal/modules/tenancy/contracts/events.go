package contracts

// Tópicos de los eventos de tenancy.
const (
	TopicTenantPlanChanged   = "tenancy.tenant_plan_changed"
	TopicTenantStatusChanged = "tenancy.tenant_status_changed"
)

// TenantPlanChanged se publica cuando cambia el plan comercial. El tenant viaja en los metadatos
// del evento.
type TenantPlanChanged struct {
	Previous string `json:"previous"`
	New      string `json:"new"`
}

// TenantStatusChanged se publica en el alta (Previous vacío), la suspensión y la reactivación.
type TenantStatusChanged struct {
	Previous string `json:"previous"`
	New      string `json:"new"`
}

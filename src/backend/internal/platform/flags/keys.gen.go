// Código generado por `make generate` desde registry.yaml. NO EDITAR.

package flags

// Claves registradas (rules/feature-flags.md: prohibido usar literales).
const (
	// BillingModule: Cartera y cobro — planes de pago, cuotas, pagos informados, gestión de cobro (Flujo 4)
	BillingModule Key = "module.billing"
	// ClaimsModule: Siniestros — reporte, documentación y seguimiento con la aseguradora (Flujo 7)
	ClaimsModule Key = "module.claims"
	// CommissionsModule: Comisiones — esperada, liquidación de aseguradora, conciliación y comisión interna (Flujo 9)
	CommissionsModule Key = "module.commissions"
	// ComplianceModule: Cumplimiento — SARLAFT, listas restrictivas, PQR y habeas data (Flujo 10)
	ComplianceModule Key = "module.compliance"
	// CrmModule: CRM — prospectos, embudo, asignación de asesor, seguimientos (Flujo 1)
	CrmModule Key = "module.crm"
	// DocumentsModule: Gestión documental — versiones y retención (transversal)
	DocumentsModule Key = "module.documents"
	// IntegrationsModule: Integraciones con aseguradoras (cada aseguradora tiene además su kill switch ops.integrations.<aseguradora>)
	IntegrationsModule Key = "module.integrations"
	// NotificationsModule: Notificaciones — email, SMS y WhatsApp con plantillas
	NotificationsModule Key = "module.notifications"
	// QuotingModule: Cotización — solicitudes, propuestas y comparativo (Flujo 2)
	QuotingModule Key = "module.quoting"
	// RenewalsModule: Renovaciones — alertas, recotización y retención (Flujo 6)
	RenewalsModule Key = "module.renewals"
	// ReportingModule: Reportes — indicadores de la agencia (KPIs)
	ReportingModule Key = "module.reporting"
)

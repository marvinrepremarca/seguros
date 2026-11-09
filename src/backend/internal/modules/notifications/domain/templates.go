// Package domain contiene las plantillas y reglas de los avisos a clientes.
package domain

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Errores de negocio.
var (
	ErrNotFound = kernel.NewError(kernel.KindNotFound, "notifications.message.not_found", "aviso inexistente o sin reintento posible")
)

// MaxAttempts es el número de envíos antes de marcar un aviso como fallido.
const MaxAttempts = 5

// Status es el estado de un aviso: pending → sent | skipped | failed; failed → pending por reintento.
type Status string

// Estados de un aviso.
const (
	StatusPending Status = "pending"
	StatusSent    Status = "sent"
	StatusFailed  Status = "failed"
	StatusSkipped Status = "skipped"
)

// AfterFailure decide el estado tras el intento fallido número attempts: sigue pendiente con espera,
// o queda fallido al agotar MaxAttempts.
func AfterFailure(attempts int32) (Status, time.Duration) {
	if attempts >= MaxAttempts {
		return StatusFailed, 0
	}
	return StatusPending, Backoff(attempts)
}

// Backoff es la espera tras el intento n: 5, 10, 20 y 40 minutos, para sobrevivir a una caída del correo.
func Backoff(attempts int32) time.Duration { return 5 * time.Minute << max(attempts-1, 0) }

// Plantillas. Ninguna lleva datos Restringidos ni descripciones de siniestros.
const (
	TplPolicyIssued  = "policy_issued"
	TplDueSoon       = "installment_due_soon"
	TplDueToday      = "installment_due_today"
	TplOverdue       = "installment_overdue"
	TplArrears       = "policy_in_arrears"
	TplReinstated    = "policy_reinstated"
	TplExpiring      = "policy_expiring"
	TplClaimReported = "claim_reported"
	TplClaimStatus   = "claim_status_changed"
)

// Skip dice por qué no se envió un aviso.
type Skip string

// Motivos para no enviar.
const (
	SkipNoRecipient    Skip = "no_recipient"
	SkipNoEmail        Skip = "no_email"
	SkipConsentRevoked Skip = "consent_revoked"
)

// Params son los valores de una plantilla.
type Params struct {
	DueDate  string `json:"dueDate,omitempty"`
	DaysLeft int32  `json:"daysLeft,omitempty"`
	Status   string `json:"status,omitempty"`
}

// Email es un mensaje redactado.
type Email struct{ Subject, Body string }

func claimStatusLabel(s string) (string, bool) {
	labels := map[string]string{
		"reported": "reportado", "notified": "avisado a la aseguradora", "documenting": "en documentación",
		"evaluating": "en evaluación", "approved": "aprobado por la aseguradora", "objected": "objetado por la aseguradora",
		"paid": "pagado", "closed": "cerrado",
	}
	l, ok := labels[s]
	return l, ok
}

// Render redacta el aviso. Una plantilla desconocida devuelve false: el aviso no sale con texto vacío.
func Render(template, name, policyNumber string, p Params) (Email, bool) {
	policy := "su póliza"
	if policyNumber != "" {
		policy = "su póliza " + policyNumber
	}
	var e Email
	switch template {
	case TplPolicyIssued:
		e = Email{"Su póliza fue expedida", "La aseguradora expidió " + policy + "."}
	case TplDueSoon:
		e = Email{"Recordatorio de cuota", "La próxima cuota de " + policy + " vence el " + p.DueDate + "."}
	case TplDueToday:
		e = Email{"Su cuota vence hoy", "La cuota de " + policy + " vence hoy, " + p.DueDate + "."}
	case TplOverdue:
		e = Email{"Cuota vencida", "La cuota de " + policy + " con vencimiento " + p.DueDate + " aparece sin pago."}
	case TplArrears:
		e = Email{"Su póliza está en mora", "Registramos " + policy + " en mora. Comuníquese con su asesor."}
	case TplReinstated:
		e = Email{"Su póliza salió de mora", "Registramos " + policy + " al día."}
	case TplExpiring:
		e = Email{"Su póliza está por vencer", "A " + policy + " le quedan " + strconv.Itoa(int(p.DaysLeft)) + " días de vigencia. Su asesor le ayudará con la renovación."}
	case TplClaimReported:
		e = Email{"Recibimos el reporte de su siniestro", "Registramos el reporte del siniestro de " + policy + "."}
	case TplClaimStatus:
		label, ok := claimStatusLabel(p.Status)
		if !ok {
			return Email{}, false
		}
		e = Email{"Su siniestro cambió de estado", "El siniestro de " + policy + " quedó " + label + "."}
	default:
		return Email{}, false
	}
	// El nombre lo escribe un usuario: sin saltos de línea no puede añadir texto al mensaje.
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	e.Body = "Hola, " + name + ".\n\n" + e.Body + "\n\nEste es un mensaje automático de su agencia de seguros.\n"
	return e, true
}

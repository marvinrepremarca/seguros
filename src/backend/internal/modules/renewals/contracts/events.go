// Package contracts es la superficie pública de renewals.
package contracts

import "github.com/google/uuid"

// Tópicos de renewals.
const (
	TopicPolicyExpiring = "renewals.policy_expiring"
	TopicRenewalLost    = "renewals.renewal_lost"
)

// PolicyExpiring avisa que una póliza cruzó un umbral de alerta (DECISIONS T2).
type PolicyExpiring struct {
	PolicyID uuid.UUID `json:"policyId"`
	DaysLeft int32     `json:"daysLeft"`
}

// RenewalLost indica que el cliente no renueva. El motivo es texto libre: no viaja en el evento.
type RenewalLost struct {
	PolicyID uuid.UUID `json:"policyId"`
}

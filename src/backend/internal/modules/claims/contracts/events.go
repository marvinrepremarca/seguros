// Package contracts es la superficie pública de claims. Los eventos llevan IDs: nunca datos de salud.
package contracts

import "github.com/google/uuid"

// Tópicos de claims.
const (
	TopicClaimReported      = "claims.claim_reported"
	TopicClaimStatusChanged = "claims.claim_status_changed"
)

// ClaimReported avisa un siniestro nuevo.
type ClaimReported struct {
	ClaimID  uuid.UUID `json:"claimId"`
	PolicyID uuid.UUID `json:"policyId"`
}

// ClaimStatusChanged avisa un cambio de estado.
type ClaimStatusChanged struct {
	ClaimID  uuid.UUID `json:"claimId"`
	PolicyID uuid.UUID `json:"policyId"`
	From     string    `json:"from"`
	To       string    `json:"to"`
}

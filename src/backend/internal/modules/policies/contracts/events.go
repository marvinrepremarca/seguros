// Package contracts es la única superficie pública de policies: sus eventos de integración
// (DOMAIN.md §3). Llevan IDs y valores no Restringidos; el tenant viaja en los metadatos.
package contracts

import "github.com/google/uuid"

// Tópicos de los eventos de policies.
const (
	TopicPolicyIssued          = "policies.policy_issued"
	TopicPolicyEndorsed        = "policies.policy_endorsed"
	TopicPolicyCancelled       = "policies.policy_cancelled"
	TopicPolicyExpired         = "policies.policy_expired"
	TopicPolicyRenewed         = "policies.policy_renewed"
	TopicPolicyFellIntoArrears = "policies.policy_fell_into_arrears"
	TopicPolicyReinstated      = "policies.policy_reinstated"
)

// PolicyIssued se publica cuando la aseguradora expide la póliza.
type PolicyIssued struct {
	PolicyID            uuid.UUID  `json:"policyId"`
	PolicyNumber        string     `json:"policyNumber"`
	InsurerID           uuid.UUID  `json:"insurerId"`
	LineOfBusinessID    uuid.UUID  `json:"lineOfBusinessId"`
	ProductID           *uuid.UUID `json:"productId,omitempty"`
	HolderPartyID       uuid.UUID  `json:"holderPartyId"`
	AgentID             *uuid.UUID `json:"agentId,omitempty"`
	Currency            string     `json:"currency"`
	NetPremium          string     `json:"netPremium"` // decimal en texto
	Taxes               string     `json:"taxes"`
	EffectiveFrom       string     `json:"effectiveFrom"` // YYYY-MM-DD
	EffectiveTo         string     `json:"effectiveTo"`
	PaymentForm         string     `json:"paymentForm"`
	RenewedFromPolicyID *uuid.UUID `json:"renewedFromPolicyId,omitempty"`
}

// PolicyEndorsed se publica cuando se emite un endoso. Las variaciones pueden ser negativas.
type PolicyEndorsed struct {
	PolicyID        uuid.UUID `json:"policyId"`
	EndorsementID   uuid.UUID `json:"endorsementId"`
	Currency        string    `json:"currency"`
	NetPremiumDelta string    `json:"netPremiumDelta"`
	TaxesDelta      string    `json:"taxesDelta"`
	EffectiveDate   string    `json:"effectiveDate"`
}

// PolicyCancelled se publica cuando se registra la cancelación expedida por la aseguradora.
type PolicyCancelled struct {
	PolicyID        uuid.UUID `json:"policyId"`
	Cause           string    `json:"cause"`
	EffectiveDate   string    `json:"effectiveDate"`
	Currency        string    `json:"currency"`
	UnearnedPremium string    `json:"unearnedPremium"`
}

// PolicyExpired se publica al terminar la vigencia.
type PolicyExpired struct {
	PolicyID uuid.UUID `json:"policyId"`
}

// PolicyRenewed se publica cuando una póliza vencida se renueva en otra.
type PolicyRenewed struct {
	PolicyID  uuid.UUID `json:"policyId"`
	RenewedBy uuid.UUID `json:"renewedBy"`
}

// PolicyFellIntoArrears se publica cuando la póliza entra en mora.
type PolicyFellIntoArrears struct {
	PolicyID uuid.UUID `json:"policyId"`
}

// PolicyReinstated se publica cuando la póliza sale de mora.
type PolicyReinstated struct {
	PolicyID uuid.UUID `json:"policyId"`
}

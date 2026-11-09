// Package contracts es la superficie pública de quoting.
package contracts

import (
	"encoding/json"

	"github.com/google/uuid"
)

// TopicQuoteAccepted es el tópico del evento.
const TopicQuoteAccepted = "quoting.quote_accepted"

// QuoteCoverage es una cobertura ofertada.
type QuoteCoverage struct {
	Name       string `json:"name"`
	SumInsured string `json:"sumInsured"`
	Deductible string `json:"deductible,omitempty"`
}

// QuoteAccepted lleva los términos con los que Policies crea la póliza EnTramite (DECISIONS N28). Datos
// Confidenciales; los esquemas de riesgo no admiten datos de salud.
type QuoteAccepted struct {
	RequestID         uuid.UUID       `json:"requestId"`
	OfferID           uuid.UUID       `json:"offerId"`
	PolicyID          uuid.UUID       `json:"policyId"`
	LeadID            *uuid.UUID      `json:"leadId,omitempty"`
	InsurerID         uuid.UUID       `json:"insurerId"`
	LineOfBusinessID  uuid.UUID       `json:"lineOfBusinessId"`
	ProductID         *uuid.UUID      `json:"productId,omitempty"`
	AgentID           *uuid.UUID      `json:"agentId,omitempty"`
	HolderPartyID     uuid.UUID       `json:"holderPartyId"`
	InsuredPartyIDs   []uuid.UUID     `json:"insuredPartyIds"`
	Currency          string          `json:"currency"`
	NetPremium        string          `json:"netPremium"`
	Taxes             string          `json:"taxes"`
	EffectiveFrom     string          `json:"effectiveFrom"`
	EffectiveTo       string          `json:"effectiveTo"`
	PaymentForm       string          `json:"paymentForm"`
	Coverages         []QuoteCoverage `json:"coverages"`
	RiskDescription   string          `json:"riskDescription"`
	RiskSchemaKey     string          `json:"riskSchemaKey"`
	RiskSchemaVersion int32           `json:"riskSchemaVersion"`
	RiskData          json.RawMessage `json:"riskData"`
}

// Package contracts es la superficie pública de compliance.
package contracts

import (
	"context"

	"github.com/google/uuid"
)

// TopicKycCheckCompleted es el tópico del evento.
const TopicKycCheckCompleted = "compliance.kyc_check_completed"

// KycCheckCompleted lleva solo IDs: el resultado es Restringido y se consulta en KycStatuses.
type KycCheckCompleted struct {
	CheckID uuid.UUID `json:"checkId"`
	PartyID uuid.UUID `json:"partyId"`
}

// KycStatuses responde si el tercero tiene KYC aprobado para su documento vigente (DECISIONS N4).
type KycStatuses interface {
	IsApproved(ctx context.Context, party uuid.UUID) (bool, error)
}

package contracts

import (
	"context"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Estados de póliza que otros módulos consultan.
const (
	StatusActive     = "active"
	StatusInArrears  = "in_arrears"
	StatusExpired    = "expired"
	StatusRenewed    = "renewed"
	StatusNotRenewed = "not_renewed"
)

// PolicyFacts son los datos de una póliza para otros módulos.
type PolicyFacts struct {
	ID            uuid.UUID
	Status        string
	Number        string
	InsurerID     uuid.UUID
	HolderPartyID uuid.UUID
	AgentID       uuid.NullUUID
	Currency      kernel.Currency
	EffectiveFrom kernel.Date
	EffectiveTo   kernel.Date
}

// Directory consulta pólizas del tenant del contexto. Quien la invoca ya autorizó su operación.
type Directory interface {
	Facts(ctx context.Context, id uuid.UUID) (PolicyFacts, bool, error)
	// IDsOfAgent devuelve hasta limit pólizas del asesor, las más recientes primero.
	IDsOfAgent(ctx context.Context, agent uuid.UUID, limit int32) ([]uuid.UUID, error)
}

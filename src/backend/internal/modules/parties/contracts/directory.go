// Package contracts es la única superficie pública del módulo parties.
package contracts

import (
	"context"

	"github.com/google/uuid"
)

// PartySummary es la vista mínima de un tercero. El nombre es dato Confidencial; el documento nunca
// sale del módulo.
type PartySummary struct {
	ID   uuid.UUID
	Kind string // person | organization
	Name string
}

// Directory consulta terceros del tenant del contexto. Lo implementa parties y se inyecta en los
// módulos que referencian terceros (Policies).
type Directory interface {
	// Summaries devuelve los terceros existentes entre ids; los que no existen no aparecen.
	Summaries(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]PartySummary, error)
}

// AgentScope dice si un tercero es cliente de un asesor (DECISIONS N10, N11). Interfaz aparte: solo la
// necesitan los módulos donde el asesor elige clientes.
type AgentScope interface {
	VisibleToAgent(ctx context.Context, party, agent uuid.UUID) (bool, error)
}

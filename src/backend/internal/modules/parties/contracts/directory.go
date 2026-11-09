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

// HabeasRecord es lo que la agencia guarda de un titular (DECISIONS N33). El documento va enmascarado.
type HabeasRecord struct {
	Kind, Name, DocumentType, DocumentLast4, Email, Phone, Address, City string
	Consents                                                             []HabeasConsent
}

// HabeasConsent es una autorización o revocación registrada.
type HabeasConsent struct {
	Status, Purpose, TextVersion, ConsentDate string
}

// Contact es lo mínimo para avisarle a un titular.
type Contact struct {
	Name, Email string
	// Consents va del más reciente al más antiguo.
	Consents []HabeasConsent
}

// ContactView la usan los avisos a clientes: solo nombre, correo y autorizaciones.
type ContactView interface {
	Contact(ctx context.Context, party uuid.UUID) (Contact, bool, error)
}

// ComplianceView lo usa Compliance: la huella del documento vigente, para ligar el KYC (ADR 0012), y
// los datos del titular. Quien la invoca ya autorizó su operación y audita el acceso.
type ComplianceView interface {
	// DocumentFingerprint devuelve el blind index del documento; found es false si no existe el tercero.
	DocumentFingerprint(ctx context.Context, party uuid.UUID) (fingerprint []byte, found bool, err error)
	HabeasExport(ctx context.Context, party uuid.UUID) (HabeasRecord, bool, error)
}

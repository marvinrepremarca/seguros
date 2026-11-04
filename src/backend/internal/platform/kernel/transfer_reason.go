package kernel

import (
	"strings"
	"unicode/utf8"
)

// Motivos de traspaso de un asesor (DECISIONS N12). Son un catálogo cerrado para que no lleguen datos
// personales a eventos ni a la bitácora, que son inmutables.
const (
	TransferEmployeeDeparture = "employee_departure"
	TransferRoleChange        = "role_change"
	TransferProlongedAbsence  = "prolonged_absence"
	TransferRedistribution    = "portfolio_redistribution"
	TransferClientRequest     = "client_request"
	TransferBranchChange      = "branch_change"
	TransferAssignmentFix     = "assignment_correction"
	TransferOther             = "other"
)

const (
	minTransferNoteRunes = 3
	maxTransferNoteRunes = 200
)

// TransferReason es un motivo del catálogo más una nota opcional, obligatoria con "other". La nota es
// texto libre: se guarda solo en el historial, nunca en eventos ni en la bitácora.
type TransferReason struct {
	Code string
	Note string
}

// NewTransferReason valida el motivo. Devuelve false si el código no existe o la nota no sirve.
func NewTransferReason(code, note string) (TransferReason, bool) {
	switch code {
	case TransferEmployeeDeparture, TransferRoleChange, TransferProlongedAbsence, TransferRedistribution,
		TransferClientRequest, TransferBranchChange, TransferAssignmentFix, TransferOther:
	default:
		return TransferReason{}, false
	}
	note = strings.TrimSpace(note)
	n := utf8.RuneCountInString(note)
	if n > maxTransferNoteRunes || (n > 0 && n < minTransferNoteRunes) || (code == TransferOther && n == 0) {
		return TransferReason{}, false
	}
	return TransferReason{Code: code, Note: note}, true
}

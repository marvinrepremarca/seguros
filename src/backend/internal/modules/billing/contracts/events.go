// Package contracts es la superficie pública de billing: sus eventos de integración (DOMAIN.md §3).
package contracts

import "github.com/google/uuid"

// Tópicos de billing.
const (
	TopicInstallmentOverdue = "billing.installment_overdue"
	TopicInstallmentDue     = "billing.installment_due"
	TopicPaymentApplied     = "billing.payment_applied"
	TopicPaymentReversed    = "billing.payment_reversed"
	TopicArrearsCleared     = "billing.arrears_cleared"
)

// InstallmentOverdue se publica cuando una cuota supera los días de gracia (DECISIONS N2).
type InstallmentOverdue struct {
	PolicyID      uuid.UUID `json:"policyId"`
	InstallmentID uuid.UUID `json:"installmentId"`
	DueDate       string    `json:"dueDate"` // YYYY-MM-DD
}

// PaymentApplied se publica al registrar un pago informado.
type PaymentApplied struct {
	PolicyID  uuid.UUID `json:"policyId"`
	PaymentID uuid.UUID `json:"paymentId"`
	Currency  string    `json:"currency"`
	// Amount es lo aplicado a cuotas, sin saldo a favor: es lo que comisiona (DECISIONS N3, N14).
	Amount string `json:"amount"`
	PaidOn string `json:"paidOn"` // YYYY-MM-DD
	// InArrears indica si aún quedan cuotas fuera de los días de gracia (DECISIONS N19).
	InArrears bool `json:"inArrears"`
}

// PaymentReversed se publica al reversar un pago. Commissions deshace lo causado por él.
type PaymentReversed struct {
	PolicyID  uuid.UUID `json:"policyId"`
	PaymentID uuid.UUID `json:"paymentId"`
	Currency  string    `json:"currency"`
	Amount    string    `json:"amount"` // lo que se había aplicado a cuotas
}

// ArrearsCleared se publica cuando un ajuste o un endoso deja la póliza sin cuotas fuera de gracia (N23).
type ArrearsCleared struct {
	PolicyID uuid.UUID `json:"policyId"`
}

// InstallmentDue recuerda una cuota pendiente que vence pronto (DECISIONS T1).
type InstallmentDue struct {
	PolicyID      uuid.UUID `json:"policyId"`
	InstallmentID uuid.UUID `json:"installmentId"`
	DueDate       string    `json:"dueDate"` // YYYY-MM-DD
	DaysLeft      int32     `json:"daysLeft"`
}

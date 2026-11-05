// Package domain contiene el plan de pagos de una póliza (Flujo 4). La agencia nunca custodia primas
// (DECISIONS N1): los pagos son informados por el cliente o la aseguradora.
package domain

import (
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Errores de negocio.
var (
	ErrInvalidPaymentForm = kernel.NewError(kernel.KindValidation, "billing.plan.invalid_payment_form", "forma de pago desconocida")
	ErrInvalidPeriod      = kernel.NewError(kernel.KindValidation, "billing.plan.invalid_period", "la vigencia no es válida")
	ErrNegativeTotal      = kernel.NewError(kernel.KindValidation, "billing.plan.negative_total", "el total del plan no puede ser negativo")
	ErrPlanCancelled      = kernel.NewError(kernel.KindConflict, "billing.plan.cancelled", "el plan de una póliza cancelada no admite este cambio")
	ErrScheduleMismatch   = kernel.NewError(kernel.KindValidation, "billing.plan.schedule_mismatch", "las cuotas ajustadas deben sumar lo mismo que las reemplazadas")
	ErrInvalidInstallment = kernel.NewError(kernel.KindValidation, "billing.installment.invalid", "cada cuota necesita fecha y un valor mayor que cero")
	ErrInvalidPayment     = kernel.NewError(kernel.KindValidation, "billing.payment.invalid", "el pago necesita un valor mayor que cero en la moneda del plan")
	ErrPaymentDateRange   = kernel.NewError(kernel.KindValidation, "billing.payment.date_out_of_range", "la fecha de pago es anterior al inicio de la gestión de la póliza")
	ErrInvalidLeadDays    = kernel.NewError(kernel.KindValidation, "billing.settings.invalid_lead_days", "los días de anticipación deben estar entre 0 y 3650")
	ErrFuturePayment      = kernel.NewError(kernel.KindValidation, "billing.payment.future_date", "la fecha de pago no puede ser futura")
	ErrInvalidGraceDays   = kernel.NewError(kernel.KindValidation, "billing.settings.invalid_grace_days", "los días de gracia deben estar entre 0 y 365")
	ErrPaymentReversed    = kernel.NewError(kernel.KindConflict, "billing.payment.already_reversed", "el pago ya fue reversado")
	ErrConcurrentUpdate   = kernel.NewError(kernel.KindConflict, "billing.plan.concurrent_update", "el plan cambió; vuelva a cargarlo")
)

// Formas de pago (las publica policies en PolicyIssued).
const (
	formSingle     = "single_payment"
	formMonthly    = "monthly_installments"
	formQuarterly  = "quarterly_installments"
	formDebit      = "automatic_debit"
	formFinancing  = "premium_financing"
	monthsPerMonth = 1
	monthsPerQtr   = 3
)

// DefaultGraceDays son los días tras el vencimiento para marcar mora (DECISIONS N2).
const DefaultGraceDays = 30

// InstallmentStatus es el estado de una cuota.
type InstallmentStatus string

// Estados de cuota.
const (
	InstallmentPending InstallmentStatus = "pending"
	InstallmentPaid    InstallmentStatus = "paid"
	InstallmentVoided  InstallmentStatus = "voided"
)

// Installment es una cuota. Es una copia de lectura: solo el agregado cambia las suyas.
type Installment struct {
	ID              uuid.UUID
	Number          int32
	DueDate         kernel.Date
	Amount          kernel.Money
	Paid            kernel.Money
	Status          InstallmentStatus
	OverdueNotified bool
}

// Outstanding es el saldo de la cuota; cero si no está pendiente.
func (i Installment) Outstanding() (kernel.Money, error) {
	if i.Status != InstallmentPending {
		return kernel.Zero(i.Amount.Currency())
	}
	return i.Amount.Sub(i.Paid)
}

// Allocation es la parte de un pago aplicada a una cuota.
type Allocation struct {
	InstallmentID uuid.UUID
	Amount        kernel.Money
}

// Refund es la devolución informada al cancelar; la paga la aseguradora (DECISIONS N20).
type Refund struct {
	EffectiveDate kernel.Date
	Amount        kernel.Money
}

// PlanInput son los datos de la póliza expedida.
type PlanInput struct {
	ID            uuid.UUID
	PolicyID      uuid.UUID
	PolicyNumber  string
	InsurerID     uuid.UUID
	HolderPartyID uuid.UUID
	PaymentForm   string
	Total         kernel.Money
	From, To      kernel.Date
	NewID         func() (uuid.UUID, error)
}

// PaymentPlan es el agregado.
type PaymentPlan struct {
	id            uuid.UUID
	policyID      uuid.UUID
	policyNumber  string
	insurerID     uuid.UUID
	holderPartyID uuid.UUID
	total         kernel.Money
	credit        kernel.Money
	cancelled     bool
	refund        *Refund
	installments  []Installment
	version       int32
}

// addMonths suma meses desde la fecha ancla y ajusta al último día si el mes es más corto (31 ene + 1 mes
// = 28 feb). Siempre se calcula desde el ancla para no arrastrar el ajuste a los meses siguientes.
func addMonths(d kernel.Date, n int) (kernel.Date, error) {
	first, err := kernel.NewDate(d.Year(), d.Month(), 1)
	if err != nil {
		return kernel.Date{}, err
	}
	start := first.StartOf(time.UTC).AddDate(0, n, 0)
	last := start.AddDate(0, 1, -1).Day()
	day := min(d.Day(), last)
	return kernel.NewDate(start.Year(), start.Month(), day)
}

func dueDates(form string, from, to kernel.Date) ([]kernel.Date, error) {
	step := 0
	switch form {
	case formSingle, formFinancing:
		return []kernel.Date{from}, nil
	case formMonthly, formDebit:
		step = monthsPerMonth
	case formQuarterly:
		step = monthsPerQtr
	default:
		return nil, ErrInvalidPaymentForm
	}
	var out []kernel.Date
	for i := 0; ; i++ {
		d, err := addMonths(from, i*step)
		if err != nil {
			return nil, err
		}
		if d.After(to) {
			return out, nil
		}
		out = append(out, d)
	}
}

// NewPaymentPlan genera el plan según la forma de pago (DECISIONS N15). Una prima en cero no genera
// cuotas. El residuo del reparto va a la última cuota.
func NewPaymentPlan(in PlanInput) (*PaymentPlan, error) {
	if in.Total.IsNegative() {
		return nil, ErrNegativeTotal
	}
	if in.From.IsZero() || in.To.IsZero() || in.To.Before(in.From) {
		return nil, ErrInvalidPeriod
	}
	dates, err := dueDates(in.PaymentForm, in.From, in.To)
	if err != nil {
		return nil, err
	}
	zero, err := kernel.Zero(in.Total.Currency())
	if err != nil {
		return nil, err
	}
	p := &PaymentPlan{
		id: in.ID, policyID: in.PolicyID, policyNumber: in.PolicyNumber, insurerID: in.InsurerID, holderPartyID: in.HolderPartyID,
		total: in.Total, credit: zero, version: 1,
	}
	if in.Total.IsZero() {
		return p, nil
	}
	parts, err := in.Total.Allocate(len(dates))
	if err != nil {
		return nil, err
	}
	for i, due := range dates {
		id, err := in.NewID()
		if err != nil {
			return nil, err
		}
		p.installments = append(p.installments, Installment{ID: id, Number: int32(i + 1), DueDate: due, Amount: parts[i], Paid: zero, Status: InstallmentPending}) //nolint:gosec // pocas cuotas
	}
	return p, nil
}

// PlanSnapshot son los datos del plan para rehidratar y persistir.
type PlanSnapshot struct {
	ID            uuid.UUID
	PolicyID      uuid.UUID
	PolicyNumber  string
	InsurerID     uuid.UUID
	HolderPartyID uuid.UUID
	Total         kernel.Money
	Credit        kernel.Money
	Cancelled     bool
	Refund        *Refund
	Installments  []Installment
	Version       int32
}

// RehydratePlan reconstruye el agregado desde la persistencia.
func RehydratePlan(s PlanSnapshot) *PaymentPlan {
	return &PaymentPlan{
		id: s.ID, policyID: s.PolicyID, policyNumber: s.PolicyNumber, insurerID: s.InsurerID, holderPartyID: s.HolderPartyID,
		total: s.Total, credit: s.Credit, cancelled: s.Cancelled, refund: s.Refund,
		installments: append([]Installment(nil), s.Installments...), version: s.Version,
	}
}

// Snapshot devuelve una copia del estado.
func (p *PaymentPlan) Snapshot() PlanSnapshot {
	var refund *Refund
	if p.refund != nil {
		r := *p.refund
		refund = &r
	}
	return PlanSnapshot{
		ID: p.id, PolicyID: p.policyID, PolicyNumber: p.policyNumber, InsurerID: p.insurerID, HolderPartyID: p.holderPartyID,
		Total: p.total, Credit: p.credit, Cancelled: p.cancelled, Refund: refund,
		Installments: append([]Installment(nil), p.installments...), Version: p.version,
	}
}

func (p *PaymentPlan) settle(i *Installment) error {
	if i.Status == InstallmentVoided {
		return nil
	}
	c, err := i.Paid.Cmp(i.Amount)
	if err != nil {
		return err
	}
	if c >= 0 {
		i.Status = InstallmentPaid
	} else {
		i.Status = InstallmentPending
	}
	return nil
}

func (p *PaymentPlan) sortInstallments() {
	sort.SliceStable(p.installments, func(a, b int) bool {
		x, y := p.installments[a], p.installments[b]
		if c := x.DueDate.Compare(y.DueDate); c != 0 {
			return c < 0
		}
		return x.Number < y.Number
	})
}

// ApplyPayment aplica el valor a la cuota pendiente más antigua primero; el sobrante final queda como
// saldo a favor (DECISIONS N6). Devuelve lo aplicado a cada cuota y ese sobrante.
func (p *PaymentPlan) ApplyPayment(amount kernel.Money) ([]Allocation, kernel.Money, error) {
	if p.cancelled {
		return nil, kernel.Money{}, ErrPlanCancelled
	}
	if amount.IsZero() || amount.IsNegative() || amount.Currency() != p.total.Currency() {
		return nil, kernel.Money{}, ErrInvalidPayment
	}
	p.sortInstallments()
	left := amount
	var out []Allocation
	for idx := range p.installments {
		if left.IsZero() {
			break
		}
		i := &p.installments[idx]
		due, err := i.Outstanding()
		if err != nil {
			return nil, kernel.Money{}, err
		}
		if due.IsZero() {
			continue
		}
		part := left
		if c, err := left.Cmp(due); err != nil {
			return nil, kernel.Money{}, err
		} else if c > 0 {
			part = due
		}
		if i.Paid, err = i.Paid.Add(part); err != nil {
			return nil, kernel.Money{}, err
		}
		if err := p.settle(i); err != nil {
			return nil, kernel.Money{}, err
		}
		if left, err = left.Sub(part); err != nil {
			return nil, kernel.Money{}, err
		}
		out = append(out, Allocation{InstallmentID: i.ID, Amount: part})
	}
	var err error
	if p.credit, err = p.credit.Add(left); err != nil {
		return nil, kernel.Money{}, err
	}
	return out, left, nil
}

// ReversePayment deshace lo que aplicó un pago. Una cuota que vuelve a tener saldo vuelve a poder
// generar aviso de mora.
func (p *PaymentPlan) ReversePayment(allocations []Allocation, surplus kernel.Money) error {
	var err error
	for _, a := range allocations {
		for idx := range p.installments {
			i := &p.installments[idx]
			if i.ID != a.InstallmentID {
				continue
			}
			if i.Paid, err = i.Paid.Sub(a.Amount); err != nil {
				return err
			}
			if err := p.settle(i); err != nil {
				return err
			}
			if p.cancelled && i.Status == InstallmentPending {
				// La póliza terminó: la cuota no vuelve a cartera (DECISIONS N20).
				i.Status = InstallmentVoided
			}
			if i.Status == InstallmentPending {
				i.OverdueNotified = false
			}
		}
	}
	p.credit, err = p.credit.Sub(surplus)
	return err
}

// ScheduledInstallment es una cuota del ajuste manual.
type ScheduledInstallment struct {
	DueDate kernel.Date
	Amount  kernel.Money
}

// AdjustSchedule reemplaza las cuotas sin abonos por las que informó la aseguradora (DECISIONS N15).
// Las pagadas o abonadas no cambian, y la suma se conserva.
func (p *PaymentPlan) AdjustSchedule(schedule []ScheduledInstallment, newID func() (uuid.UUID, error)) error {
	if p.cancelled {
		return ErrPlanCancelled
	}
	zero, err := kernel.Zero(p.total.Currency())
	if err != nil {
		return err
	}
	replaced, proposed := zero, zero
	kept := make([]Installment, 0, len(p.installments)+len(schedule))
	for _, i := range p.installments {
		if i.Status == InstallmentPending && i.Paid.IsZero() {
			if replaced, err = replaced.Add(i.Amount); err != nil {
				return err
			}
			continue
		}
		kept = append(kept, i)
	}
	for _, s := range schedule {
		if s.DueDate.IsZero() || s.Amount.IsZero() || s.Amount.IsNegative() || s.Amount.Currency() != p.total.Currency() {
			return ErrInvalidInstallment
		}
		if proposed, err = proposed.Add(s.Amount); err != nil {
			return err
		}
		id, err := newID()
		if err != nil {
			return err
		}
		kept = append(kept, Installment{ID: id, DueDate: s.DueDate, Amount: s.Amount, Paid: zero, Status: InstallmentPending})
	}
	if !replaced.Equal(proposed) {
		return ErrScheduleMismatch
	}
	p.installments = kept
	p.sortInstallments()
	for idx := range p.installments {
		p.installments[idx].Number = int32(idx + 1) //nolint:gosec // pocas cuotas
	}
	return nil
}

// ApplyEndorsement reparte la variación de prima entre las cuotas con saldo, en proporción a su saldo,
// con el residuo en la última (DECISIONS N18). Sin cuotas con saldo, un aumento crea una cuota con
// vencimiento en la fecha efectiva; lo que una disminución no alcanza a descontar queda como saldo a favor.
func (p *PaymentPlan) ApplyEndorsement(delta kernel.Money, effective kernel.Date, newID func() (uuid.UUID, error)) error {
	if p.cancelled {
		return ErrPlanCancelled
	}
	total, err := p.total.Add(delta)
	if err != nil {
		return err
	}
	if total.IsNegative() {
		return ErrNegativeTotal
	}
	p.total = total
	if delta.IsZero() {
		return nil
	}
	open, balance, err := p.openBalance()
	if err != nil {
		return err
	}
	if len(open) == 0 {
		return p.endorseWithoutOpen(delta, effective, newID)
	}
	if delta.IsNegative() {
		zero, err := kernel.Zero(p.total.Currency())
		if err != nil {
			return err
		}
		abs, err := zero.Sub(delta)
		if err != nil {
			return err
		}
		c, err := abs.Cmp(balance)
		if err != nil {
			return err
		}
		if c >= 0 { // alcanza para todo el saldo; lo demás es saldo a favor
			for _, idx := range open {
				i := &p.installments[idx]
				i.Amount = i.Paid
				if err := p.settle(i); err != nil {
					return err
				}
			}
			excess, err := abs.Sub(balance)
			if err != nil {
				return err
			}
			p.credit, err = p.credit.Add(excess)
			return err
		}
	}
	return p.distribute(open, balance, delta)
}

func (p *PaymentPlan) openBalance() ([]int, kernel.Money, error) {
	balance, err := kernel.Zero(p.total.Currency())
	if err != nil {
		return nil, kernel.Money{}, err
	}
	var open []int
	p.sortInstallments()
	for idx, i := range p.installments {
		o, err := i.Outstanding()
		if err != nil {
			return nil, kernel.Money{}, err
		}
		if o.IsZero() {
			continue
		}
		open = append(open, idx)
		if balance, err = balance.Add(o); err != nil {
			return nil, kernel.Money{}, err
		}
	}
	return open, balance, nil
}

func (p *PaymentPlan) endorseWithoutOpen(delta kernel.Money, effective kernel.Date, newID func() (uuid.UUID, error)) error {
	zero, err := kernel.Zero(p.total.Currency())
	if err != nil {
		return err
	}
	if delta.IsNegative() {
		abs, err := zero.Sub(delta)
		if err != nil {
			return err
		}
		p.credit, err = p.credit.Add(abs)
		return err
	}
	id, err := newID()
	if err != nil {
		return err
	}
	p.installments = append(p.installments, Installment{
		ID: id, Number: int32(len(p.installments) + 1), DueDate: effective, Amount: delta, Paid: zero, Status: InstallmentPending, //nolint:gosec // pocas cuotas
	})
	return nil
}

// distribute reparte delta entre las cuotas abiertas. En una disminución, |delta| < balance: el ajuste
// del redondeo recorre desde la última sin bajar ninguna por debajo de lo abonado.
func (p *PaymentPlan) distribute(open []int, balance, delta kernel.Money) error {
	left := delta
	for n, idx := range open {
		i := &p.installments[idx]
		o, err := i.Outstanding()
		if err != nil {
			return err
		}
		part := left
		if n < len(open)-1 {
			ratio := o.Amount().Div(balance.Amount())
			scaled, err := delta.Mul(ratio)
			if err != nil {
				return err
			}
			part = truncate(scaled)
		}
		if part, err = capDecrease(part, o); err != nil {
			return err
		}
		if i.Amount, err = i.Amount.Add(part); err != nil {
			return err
		}
		if left, err = left.Sub(part); err != nil {
			return err
		}
	}
	// Solo queda residuo si la última alcanzó su tope.
	for n := len(open) - 1; n >= 0 && !left.IsZero(); n-- {
		i := &p.installments[open[n]]
		o, err := i.Outstanding()
		if err != nil {
			return err
		}
		part, err := capDecrease(left, o)
		if err != nil {
			return err
		}
		if i.Amount, err = i.Amount.Add(part); err != nil {
			return err
		}
		if left, err = left.Sub(part); err != nil {
			return err
		}
	}
	for _, idx := range open {
		if err := p.settle(&p.installments[idx]); err != nil {
			return err
		}
	}
	return nil
}

// truncate corta a la unidad menor hacia cero, para que el residuo siempre tenga el signo de delta.
func truncate(m kernel.Money) kernel.Money {
	places := int32(2)
	if u, ok := m.Currency().MinorUnits(); ok {
		places = u
	}
	out, err := kernel.NewMoney(m.Amount().Truncate(places), m.Currency())
	if err != nil {
		return m
	}
	return out
}

// capDecrease limita una disminución al saldo de la cuota.
func capDecrease(part, outstanding kernel.Money) (kernel.Money, error) {
	if !part.IsNegative() {
		return part, nil
	}
	zero, err := kernel.Zero(part.Currency())
	if err != nil {
		return kernel.Money{}, err
	}
	floor, err := zero.Sub(outstanding)
	if err != nil {
		return kernel.Money{}, err
	}
	c, err := part.Cmp(floor)
	if err != nil {
		return kernel.Money{}, err
	}
	if c < 0 {
		return floor, nil
	}
	return part, nil
}

// Cancel anula las cuotas con saldo y registra la devolución informada, sin tocar los pagos ni
// convertirla en saldo a favor (DECISIONS N20). Repetirlo no tiene efecto.
func (p *PaymentPlan) Cancel(effective kernel.Date, unearned kernel.Money) error {
	if p.cancelled {
		return nil
	}
	for idx := range p.installments {
		if p.installments[idx].Status == InstallmentPending {
			p.installments[idx].Status = InstallmentVoided
		}
	}
	p.cancelled = true
	p.refund = &Refund{EffectiveDate: effective, Amount: unearned}
	return nil
}

func overdue(i Installment, today kernel.Date, graceDays int) bool {
	return i.Status == InstallmentPending && !today.Before(i.DueDate.AddDays(graceDays))
}

// MarkOverdue devuelve las cuotas que acaban de superar los días de gracia y aún no se avisaron (N2).
func (p *PaymentPlan) MarkOverdue(today kernel.Date, graceDays int) []Installment {
	var out []Installment
	for idx := range p.installments {
		i := &p.installments[idx]
		if overdue(*i, today, graceDays) && !i.OverdueNotified {
			i.OverdueNotified = true
			out = append(out, *i)
		}
	}
	return out
}

// InArrears indica si queda alguna cuota fuera de los días de gracia (DECISIONS N19).
func (p *PaymentPlan) InArrears(today kernel.Date, graceDays int) bool {
	for _, i := range p.installments {
		if overdue(i, today, graceDays) {
			return true
		}
	}
	return false
}

// ValidateGraceDays valida el parámetro de mora.
func ValidateGraceDays(days int32) error {
	if days < 0 || days > 365 {
		return ErrInvalidGraceDays
	}
	return nil
}

// DefaultPaymentLeadDays es cuánto antes del primer vencimiento se acepta un pago, editable por agencia
// (DECISIONS N22).
const DefaultPaymentLeadDays = 60

// ValidatePaymentDate rechaza fechas futuras o anteriores a la gestión de la póliza (DECISIONS N22).
func (p *PaymentPlan) ValidatePaymentDate(paidOn, today kernel.Date, leadDays int) error {
	if paidOn.After(today) {
		return ErrFuturePayment
	}
	for _, i := range p.installments {
		if paidOn.Before(i.DueDate.AddDays(-leadDays)) {
			continue
		}
		return nil
	}
	if len(p.installments) == 0 {
		return nil
	}
	return ErrPaymentDateRange
}

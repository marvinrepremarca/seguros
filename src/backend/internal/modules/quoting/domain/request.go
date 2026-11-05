// Package domain contiene la solicitud de cotización y sus ofertas (Flujo 2).
package domain

import (
	"log/slog"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Errores de negocio.
var (
	ErrNotFound         = kernel.NewError(kernel.KindNotFound, "quoting.request.not_found", "solicitud inexistente")
	ErrOfferNotFound    = kernel.NewError(kernel.KindNotFound, "quoting.offer.not_found", "oferta inexistente")
	ErrNotOpen          = kernel.NewError(kernel.KindConflict, "quoting.request.not_open", "la solicitud ya no admite cambios")
	ErrOfferExpired     = kernel.NewError(kernel.KindConflict, "quoting.offer.expired", "la oferta está vencida: no se puede aceptar")
	ErrConcurrentUpdate = kernel.NewError(kernel.KindConflict, "quoting.request.concurrent_update", "la solicitud cambió; vuelva a cargarla")
	ErrInvalidRisk      = kernel.NewError(kernel.KindValidation, "quoting.request.invalid_risk", "los datos del riesgo no son válidos")
	ErrInvalidParty     = kernel.NewError(kernel.KindValidation, "quoting.request.invalid_party", "el cliente no existe o está fuera de su alcance")
	ErrInvalidOffer     = kernel.NewError(kernel.KindValidation, "quoting.offer.invalid", "los datos de la oferta no son válidos")
	ErrInvalidValidity  = kernel.NewError(kernel.KindValidation, "quoting.offer.invalid_validity", "la oferta no puede nacer vencida")
	ErrInvalidReason    = kernel.NewError(kernel.KindValidation, "quoting.request.invalid_reason", "el motivo es obligatorio")
	ErrInvalidPayment   = kernel.NewError(kernel.KindValidation, "quoting.offer.invalid_payment_form", "forma de pago desconocida")
	ErrInactiveAgent    = kernel.NewError(kernel.KindConflict, "quoting.offer.inactive_agent", "el asesor ya no está activo")
	ErrTooLarge         = kernel.NewError(kernel.KindValidation, "quoting.offer.too_large", "la oferta tiene demasiados datos para tramitarla")
	ErrInvalidDays      = kernel.NewError(kernel.KindValidation, "quoting.settings.invalid_days", "los días de vigencia deben estar entre 1 y 365")
)

// DefaultOfferValidDays es la vigencia por defecto de una oferta (DECISIONS T3).
const DefaultOfferValidDays = 30

const maxCoverages = 20

// RequestStatus es el estado de la solicitud.
type RequestStatus string

// Estados de la solicitud.
const (
	RequestOpen     RequestStatus = "open"
	RequestAccepted RequestStatus = "accepted"
	RequestDeclined RequestStatus = "declined"
)

// OfferStatus es el estado de una oferta.
type OfferStatus string

// Estados de la oferta.
const (
	OfferOffered   OfferStatus = "offered"
	OfferAccepted  OfferStatus = "accepted"
	OfferDiscarded OfferStatus = "discarded"
)

// Coverage es un amparo ofertado.
type Coverage struct {
	Name       string
	SumInsured kernel.Money
	Deductible string
}

// Offer es la propuesta de una aseguradora.
type Offer struct {
	ID                uuid.UUID
	InsurerID         uuid.UUID
	ProductID         uuid.NullUUID
	Reference         string
	NetPremium        kernel.Money
	Taxes             kernel.Money
	EffectiveFrom     kernel.Date
	EffectiveTo       kernel.Date
	ValidUntil        kernel.Date
	Status            OfferStatus
	RiskSchemaKey     string
	RiskSchemaVersion int32
	Coverages         []Coverage
}

// Snapshot son los datos de la solicitud.
type Snapshot struct {
	ID               uuid.UUID
	LeadID           uuid.NullUUID
	HolderPartyID    uuid.UUID
	LineOfBusinessID uuid.UUID
	AgentID          uuid.UUID
	RiskDescription  string
	RiskData         []byte
	Status           RequestStatus
	DeclineReason    string
	PolicyID         uuid.NullUUID
	Offers           []Offer
	Version          int32
}

// Request es el agregado.
type Request struct{ s Snapshot }

// NewRequest crea una solicitud abierta.
func NewRequest(s Snapshot) (*Request, error) {
	s.RiskDescription = strings.TrimSpace(s.RiskDescription)
	if n := utf8.RuneCountInString(s.RiskDescription); n < 2 || n > 200 || len(s.RiskData) == 0 || len(s.RiskData) > 8<<10 {
		return nil, ErrInvalidRisk
	}
	if s.HolderPartyID == uuid.Nil {
		return nil, ErrInvalidParty
	}
	s.Status, s.Version = RequestOpen, 1
	return &Request{s: s}, nil
}

// Rehydrate reconstruye la solicitud.
func Rehydrate(s Snapshot) *Request { return &Request{s: s} }

// Snapshot devuelve una copia.
func (r *Request) Snapshot() Snapshot {
	out := r.s
	out.Offers = slices.Clone(r.s.Offers)
	return out
}

// LogValue evita que los datos del riesgo lleguen a los logs.
func (r *Request) LogValue() slog.Value { return slog.StringValue(r.s.ID.String()) }

// AddOffer agrega una oferta; sin validUntil, vale los días por defecto del tenant (DECISIONS T3).
func (r *Request) AddOffer(o Offer, today kernel.Date, validDays int32) (Offer, error) {
	if r.s.Status != RequestOpen {
		return Offer{}, ErrNotOpen
	}
	if o.InsurerID == uuid.Nil || o.NetPremium.Currency() == "" || o.NetPremium.Currency() != o.Taxes.Currency() || o.NetPremium.IsNegative() || o.Taxes.IsNegative() ||
		!o.EffectiveTo.After(o.EffectiveFrom) || len(o.Coverages) > maxCoverages || utf8.RuneCountInString(o.Reference) > 60 {
		return Offer{}, ErrInvalidOffer
	}
	if _, err := o.NetPremium.Add(o.Taxes); err != nil {
		return Offer{}, ErrInvalidOffer
	}
	for _, c := range o.Coverages {
		n := utf8.RuneCountInString(strings.TrimSpace(c.Name))
		if n < 2 || n > 100 || c.SumInsured.IsNegative() || c.SumInsured.Currency() != o.NetPremium.Currency() || utf8.RuneCountInString(c.Deductible) > 100 {
			return Offer{}, ErrInvalidOffer
		}
	}
	if o.ValidUntil.IsZero() {
		o.ValidUntil = today.AddDays(int(validDays))
	}
	if o.ValidUntil.Before(today) {
		return Offer{}, ErrInvalidValidity
	}
	o.Status = OfferOffered
	r.s.Offers = append(r.s.Offers, o)
	return o, nil
}

// Accept acepta una oferta vigente y descarta las demás (DECISIONS N28). ValidUntil vale inclusive.
func (r *Request) Accept(offerID, policyID uuid.UUID, today kernel.Date) (Offer, error) {
	if r.s.Status != RequestOpen {
		return Offer{}, ErrNotOpen
	}
	i := slices.IndexFunc(r.s.Offers, func(o Offer) bool { return o.ID == offerID })
	if i < 0 {
		return Offer{}, ErrOfferNotFound
	}
	if today.After(r.s.Offers[i].ValidUntil) {
		return Offer{}, ErrOfferExpired
	}
	for j := range r.s.Offers {
		r.s.Offers[j].Status = OfferDiscarded
	}
	r.s.Offers[i].Status = OfferAccepted
	r.s.Status, r.s.PolicyID = RequestAccepted, uuid.NullUUID{UUID: policyID, Valid: true}
	return r.s.Offers[i], nil
}

// Decline cierra la solicitud con su motivo.
func (r *Request) Decline(reason string) error {
	if r.s.Status != RequestOpen {
		return ErrNotOpen
	}
	reason = strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(reason); n < 3 || n > 500 {
		return ErrInvalidReason
	}
	for j := range r.s.Offers {
		r.s.Offers[j].Status = OfferDiscarded
	}
	r.s.Status, r.s.DeclineReason = RequestDeclined, reason
	return nil
}

// ValidatePaymentForm valida contra las formas de pago de flujos.md Flujo 4.
func ValidatePaymentForm(form string) error {
	switch form {
	case "single_payment", "monthly_installments", "quarterly_installments", "automatic_debit", "premium_financing":
		return nil
	default:
		return ErrInvalidPayment
	}
}

// ValidateOfferValidDays valida el parámetro T3.
func ValidateOfferValidDays(days int32) error {
	if days < 1 || days > 365 {
		return ErrInvalidDays
	}
	return nil
}

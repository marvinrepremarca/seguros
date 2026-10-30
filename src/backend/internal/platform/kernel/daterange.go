package kernel

// ErrInvalidDateRange indica un rango con fin no posterior al inicio o con extremos vacíos.
var ErrInvalidDateRange = NewError(KindValidation, "kernel.date_range.invalid", "el fin debe ser posterior al inicio")

// DateRange es un periodo de fechas [From, To] con To estrictamente posterior a From
// (DOMAIN.md: EffectiveTo > EffectiveFrom). Inmutable.
type DateRange struct {
	from Date
	to   Date
}

// NewDateRange valida que ambos extremos existan y que to sea posterior a from.
func NewDateRange(from, to Date) (DateRange, error) {
	if from.IsZero() || to.IsZero() || !to.After(from) {
		return DateRange{}, ErrInvalidDateRange
	}
	return DateRange{from: from, to: to}, nil
}

// From devuelve el inicio.
func (r DateRange) From() Date { return r.from }

// To devuelve el fin.
func (r DateRange) To() Date { return r.to }

// Contains indica si d está entre From y To, ambos inclusive. Las reglas de negocio que necesiten
// otra semántica en los extremos deben expresarla en su propio dominio.
func (r DateRange) Contains(d Date) bool {
	return !d.Before(r.from) && !d.After(r.to)
}

// Days devuelve los días calendario entre From y To.
func (r DateRange) Days() int { return r.from.DaysUntil(r.to) }

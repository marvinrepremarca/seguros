package kernel

import (
	"database/sql/driver"
	"fmt"
	"time"
)

const dateLayout = time.DateOnly

// ErrInvalidDate indica una fecha calendario inexistente o mal formada.
var ErrInvalidDate = NewError(KindValidation, "kernel.date.invalid", "fecha inválida")

// Date es una fecha calendario sin hora ni zona (vigencias, vencimientos de cuota). Comparable
// con ==; el valor cero representa "sin fecha" (IsZero).
type Date struct {
	year  int
	month time.Month
	day   int
}

// NewDate valida que la fecha exista (ej. rechaza 2026-02-30).
func NewDate(year int, month time.Month, day int) (Date, error) {
	t := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if t.Year() != year || t.Month() != month || t.Day() != day || year < 1 || year > 9999 {
		return Date{}, ErrInvalidDate.WithMessage("fecha inexistente: %04d-%02d-%02d", year, month, day)
	}
	return Date{year: year, month: month, day: day}, nil
}

// ParseDate interpreta "YYYY-MM-DD" (ISO 8601, formato del wire).
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return Date{}, ErrInvalidDate.WithMessage("formato esperado YYYY-MM-DD")
	}
	return DateOf(t, time.UTC), nil
}

// DateOf devuelve la fecha calendario de un instante visto en la zona loc.
func DateOf(t time.Time, loc *time.Location) Date {
	y, m, d := t.In(loc).Date()
	return Date{year: y, month: m, day: d}
}

// Today devuelve la fecha de negocio actual (zona America/Bogota).
func Today(c Clock) Date {
	return DateOf(c.Now(), BusinessLocation())
}

// Year devuelve el año.
func (d Date) Year() int { return d.year }

// Month devuelve el mes.
func (d Date) Month() time.Month { return d.month }

// Day devuelve el día del mes.
func (d Date) Day() int { return d.day }

// IsZero indica si es el valor cero ("sin fecha").
func (d Date) IsZero() bool { return d == Date{} }

// StartOf devuelve el instante 00:00 de la fecha en la zona loc.
func (d Date) StartOf(loc *time.Location) time.Time {
	return time.Date(d.year, d.month, d.day, 0, 0, 0, 0, loc)
}

func (d Date) utc() time.Time { return d.StartOf(time.UTC) }

// Compare devuelve -1, 0 o 1.
func (d Date) Compare(o Date) int { return d.utc().Compare(o.utc()) }

// Before indica si d es anterior a o.
func (d Date) Before(o Date) bool { return d.Compare(o) < 0 }

// After indica si d es posterior a o.
func (d Date) After(o Date) bool { return d.Compare(o) > 0 }

// AddDays suma (o resta) días calendario.
func (d Date) AddDays(n int) Date { return DateOf(d.utc().AddDate(0, 0, n), time.UTC) }

// DaysUntil devuelve los días calendario de d a o (negativo si o es anterior).
func (d Date) DaysUntil(o Date) int {
	return int(o.utc().Sub(d.utc()) / (24 * time.Hour))
}

// String devuelve "YYYY-MM-DD" ("" para el valor cero).
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return d.utc().Format(dateLayout)
}

// MarshalText implementa encoding.TextMarshaler (JSON "YYYY-MM-DD").
func (d Date) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

// UnmarshalText implementa encoding.TextUnmarshaler.
func (d *Date) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*d = Date{}
		return nil
	}
	parsed, err := ParseDate(string(b))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// Scan implementa sql.Scanner para columnas `date` (pgx entrega time.Time a medianoche UTC).
func (d *Date) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*d = Date{}
		return nil
	case time.Time:
		*d = DateOf(v, time.UTC)
		return nil
	case string:
		return d.UnmarshalText([]byte(v))
	case []byte:
		return d.UnmarshalText(v)
	default:
		return fmt.Errorf("kernel.Date: tipo no soportado %T", src)
	}
}

// Value implementa driver.Valuer. El valor cero se guarda como NULL.
func (d Date) Value() (driver.Value, error) {
	if d.IsZero() {
		return nil, nil
	}
	return d.utc(), nil
}

// BusinessLocation es la zona horaria del negocio (America/Bogota, UTC−5 sin horario de verano
// desde 1993). Se usa una zona fija para no depender de tzdata en la imagen distroless.
func BusinessLocation() *time.Location {
	return time.FixedZone("America/Bogota", -5*60*60)
}

package domain_test

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/parties/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

var now = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // constante de prueba

func newID() domain.PartyID { return domain.PartyID(uuid.Must(uuid.NewV7())) }

func doc(t *testing.T, ty domain.DocumentType, n string) domain.DocumentID {
	t.Helper()
	d, err := domain.NewDocumentID(ty, n)
	require.NoError(t, err)
	return d
}

func TestNewDocumentID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		ty     domain.DocumentType
		raw    string
		number string
		ok     bool
	}{
		{"CC con puntos", domain.DocumentCC, "1.020.304.050", "1020304050", true},
		{"CC cero inicial", domain.DocumentCC, "0123", "", false},
		{"CC con letras", domain.DocumentCC, "12A45", "", false},
		{"CC demasiado larga", domain.DocumentCC, "12345678901", "", false},
		{"NIT con DV correcto", domain.DocumentNIT, "900.123.456-8", "9001234568", true},
		{"NIT con DV incorrecto", domain.DocumentNIT, "900123456-1", "", false},
		{"NIT corto", domain.DocumentNIT, "1234-5", "", false},
		{"CE alfanumérica", domain.DocumentCE, "ab 12345", "AB12345", true},
		{"CE con símbolo", domain.DocumentCE, "AB#123", "", false},
		{"PPT", domain.DocumentPPT, "123456", "123456", true},
		{"tipo desconocido", domain.DocumentType("XX"), "123456", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			d, err := domain.NewDocumentID(c.ty, c.raw)
			if !c.ok {
				require.ErrorIs(t, err, domain.ErrInvalidDocument)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.number, d.Number())
		})
	}
}

func TestDocumentIDNeverPrintsTheNumber(t *testing.T) {
	t.Parallel()
	d := doc(t, domain.DocumentCC, "1020304050")
	assert.NotContains(t, d.String(), "1020304050")
	assert.Equal(t, "4050", d.Last4())
	assert.Empty(t, doc(t, domain.DocumentCC, "123").Last4(), "un documento corto no se muestra completo")

	var sb strings.Builder
	slog.New(slog.NewTextHandler(&sb, nil)).Info("x", "doc", d)
	assert.NotContains(t, sb.String(), "1020304050")
}

func TestNewPartyInvariants(t *testing.T) {
	t.Parallel()
	cc := doc(t, domain.DocumentCC, "1020304050")
	nit := doc(t, domain.DocumentNIT, "900123456-8")
	cases := []struct {
		name    string
		id      domain.PartyID
		kind    domain.Kind
		pName   string
		doc     domain.DocumentID
		contact domain.Contact
		err     error
	}{
		{"persona válida", newID(), domain.KindPerson, " Ana Pérez ", cc, domain.Contact{Email: "ana@example.com"}, nil},
		{"organización válida", newID(), domain.KindOrganization, "ACME S.A.S.", nit, domain.Contact{}, nil},
		{"sin id", domain.PartyID{}, domain.KindPerson, "Ana", cc, domain.Contact{}, kernel.ErrInvalidID},
		{"tipo inválido", newID(), domain.Kind("x"), "Ana", cc, domain.Contact{}, domain.ErrInvalidKind},
		{"nombre corto", newID(), domain.KindPerson, "A", cc, domain.Contact{}, domain.ErrInvalidName},
		{"nombre largo", newID(), domain.KindPerson, strings.Repeat("a", 201), cc, domain.Contact{}, domain.ErrInvalidName},
		{"email inválido", newID(), domain.KindPerson, "Ana", cc, domain.Contact{Email: "ana"}, domain.ErrInvalidContact},
		{"teléfono largo", newID(), domain.KindPerson, "Ana", cc, domain.Contact{Phone: strings.Repeat("1", 31)}, domain.ErrInvalidContact},
		{"persona con NIT", newID(), domain.KindPerson, "Ana", nit, domain.Contact{}, domain.ErrDocumentKind},
		{"organización con CC", newID(), domain.KindOrganization, "ACME", cc, domain.Contact{}, domain.ErrDocumentKind},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p, err := domain.NewParty(c.id, c.kind, c.pName, c.doc, c.contact, now)
			if c.err != nil {
				require.ErrorIs(t, err, c.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, int32(1), p.Version())
			assert.Equal(t, strings.TrimSpace(c.pName), p.Snapshot().Name)
		})
	}
}

func TestRectify(t *testing.T) {
	t.Parallel()
	cc := doc(t, domain.DocumentCC, "1020304050")
	p, err := domain.NewParty(newID(), domain.KindPerson, "Ana", cc, domain.Contact{}, now)
	require.NoError(t, err)

	later := now.Add(time.Hour)
	ce := doc(t, domain.DocumentCE, "AB12345")
	require.NoError(t, p.Rectify("Ana María", domain.Contact{City: "Bogotá"}, &ce, later))
	s := p.Snapshot()
	assert.Equal(t, "Ana María", s.Name)
	assert.Equal(t, domain.DocumentCE, s.DocumentType)
	assert.Equal(t, "2345", s.DocumentLast4)
	assert.Equal(t, later, s.UpdatedAt)

	require.ErrorIs(t, p.Rectify("", domain.Contact{}, nil, later), domain.ErrInvalidName)
	nit := doc(t, domain.DocumentNIT, "900123456-8")
	require.ErrorIs(t, p.Rectify("Ana", domain.Contact{}, &nit, later), domain.ErrDocumentKind)
	assert.Equal(t, s.ID, domain.RehydrateParty(p.Snapshot()).ID())
}

func TestPartyLogValueHasNoPersonalData(t *testing.T) {
	t.Parallel()
	p, err := domain.NewParty(newID(), domain.KindPerson, "Ana Pérez", doc(t, domain.DocumentCC, "1020304050"), domain.Contact{Email: "ana@example.com"}, now)
	require.NoError(t, err)
	var sb strings.Builder
	slog.New(slog.NewTextHandler(&sb, nil)).Info("x", "party", p)
	assert.NotContains(t, sb.String(), "Ana")
	assert.NotContains(t, sb.String(), "ana@example.com")
}

func TestNewConsent(t *testing.T) {
	t.Parallel()
	today, err := kernel.ParseDate("2026-11-03")
	require.NoError(t, err)
	id := uuid.Must(uuid.NewV7())
	c, err := domain.NewConsent(id, newID(), domain.ConsentGranted, "Gestión de pólizas", "v1", today, today, now)
	require.NoError(t, err)
	assert.Equal(t, domain.ConsentGranted, c.Status)

	_, err = domain.NewConsent(id, newID(), domain.ConsentGranted, "Gestión", "v1", today.AddDays(1), today, now)
	require.ErrorIs(t, err, domain.ErrFutureConsent)
	cases := map[string]struct {
		status        domain.ConsentStatus
		purpose, text string
		date          kernel.Date
	}{
		"estado":    {"x", "Gestión", "v1", today},
		"finalidad": {domain.ConsentRevoked, "x", "v1", today},
		"versión":   {domain.ConsentRevoked, "Gestión", " ", today},
		"fecha":     {domain.ConsentRevoked, "Gestión", "v1", kernel.Date{}},
	}
	for name, c := range cases {
		_, err := domain.NewConsent(id, newID(), c.status, c.purpose, c.text, c.date, today, now)
		require.ErrorIs(t, err, domain.ErrInvalidConsent, name)
	}
}

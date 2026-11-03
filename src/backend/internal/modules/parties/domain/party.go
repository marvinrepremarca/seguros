// Package domain contiene el agregado Party (cliente o tercero de la agencia) y sus autorizaciones
// de tratamiento de datos. El número de documento en claro solo existe en DocumentID, de vida corta:
// el agregado guarda el tipo y los últimos 4 caracteres.
package domain

import (
	"log/slog"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Errores de dominio de parties (códigos estables). Los mensajes nunca incluyen datos personales.
var (
	ErrInvalidKind      = kernel.NewError(kernel.KindValidation, "parties.party.invalid_kind", "tipo de tercero desconocido")
	ErrInvalidName      = kernel.NewError(kernel.KindValidation, "parties.party.invalid_name", "nombre de 2 a 200 caracteres")
	ErrInvalidContact   = kernel.NewError(kernel.KindValidation, "parties.party.invalid_contact", "dato de contacto inválido")
	ErrInvalidDocument  = kernel.NewError(kernel.KindValidation, "parties.party.invalid_document", "documento inválido para su tipo")
	ErrDocumentKind     = kernel.NewError(kernel.KindValidation, "parties.party.document_kind_mismatch", "el tipo de documento no corresponde al tipo de tercero")
	ErrDocumentTaken    = kernel.NewError(kernel.KindConflict, "parties.party.document_taken", "ya existe un tercero con ese documento")
	ErrNotFound         = kernel.NewError(kernel.KindNotFound, "parties.party.not_found", "tercero inexistente")
	ErrConcurrentUpdate = kernel.NewError(kernel.KindConflict, "parties.party.concurrent_update", "el tercero cambió; recargue y reintente")
	ErrInvalidConsent   = kernel.NewError(kernel.KindValidation, "parties.consent.invalid", "autorización inválida")
	ErrFutureConsent    = kernel.NewError(kernel.KindValidation, "parties.consent.future_date", "la fecha de la autorización no puede ser futura")
)

// PartyID identifica un tercero.
type PartyID uuid.UUID

// String devuelve la forma canónica.
func (id PartyID) String() string { return kernel.IDString(id) }

// Kind es el tipo de tercero.
type Kind string

// Tipos de tercero.
const (
	KindPerson       Kind = "person"
	KindOrganization Kind = "organization"
)

// DocumentType es el tipo de documento (rules/frontend.md §Formularios).
type DocumentType string

// Tipos de documento.
const (
	DocumentCC  DocumentType = "CC"
	DocumentCE  DocumentType = "CE"
	DocumentPPT DocumentType = "PPT"
	DocumentNIT DocumentType = "NIT"
)

//nolint:gochecknoglobals // patrones inmutables
var (
	ccPattern    = regexp.MustCompile(`^[1-9]\d{2,9}$`)
	nitPattern   = regexp.MustCompile(`^(\d{6,14})(\d)$`)
	alnumPattern = regexp.MustCompile(`^[A-Z0-9]{3,15}$`)
	nitWeights   = []int{3, 7, 13, 17, 19, 23, 29, 37, 41, 43, 47, 53, 59, 67, 71}
)

// DocumentID es un documento de identidad validado y normalizado. Dato Restringido: no se registra
// en logs (LogValue lo redacta) ni se guarda en el agregado.
type DocumentID struct {
	docType DocumentType
	number  string
}

// NewDocumentID normaliza (sin puntos, espacios ni guiones; en mayúsculas) y valida el formato.
func NewDocumentID(docType DocumentType, raw string) (DocumentID, error) {
	number := strings.ToUpper(strings.NewReplacer(".", "", " ", "", "-", "").Replace(raw))
	valid := false
	switch docType {
	case DocumentCC:
		valid = ccPattern.MatchString(number)
	case DocumentNIT:
		valid = validNIT(number)
	case DocumentCE, DocumentPPT:
		valid = alnumPattern.MatchString(number)
	}
	if !valid {
		return DocumentID{}, ErrInvalidDocument
	}
	return DocumentID{docType: docType, number: number}, nil
}

func validNIT(number string) bool {
	m := nitPattern.FindStringSubmatch(number)
	if m == nil {
		return false
	}
	base, sum := m[1], 0
	for i := range len(base) {
		sum += int(base[len(base)-1-i]-'0') * nitWeights[i]
	}
	check := sum % 11
	if check > 1 {
		check = 11 - check
	}
	return int(m[2][0]-'0') == check
}

// Type devuelve el tipo.
func (d DocumentID) Type() DocumentType { return d.docType }

// Number devuelve el número normalizado. Solo para cifrarlo o calcular su blind index.
func (d DocumentID) Number() string { return d.number }

// Last4 devuelve los últimos 4 caracteres, lo único que se muestra sin revelar. Un documento de 4
// caracteres o menos no muestra nada: sería el número completo.
func (d DocumentID) Last4() string {
	if len(d.number) <= 4 {
		return ""
	}
	return d.number[len(d.number)-4:]
}

// String redacta el número para que un error de programación no lo filtre.
func (d DocumentID) String() string { return string(d.docType) + " [redactado]" }

// LogValue implementa slog.LogValuer.
func (d DocumentID) LogValue() slog.Value { return slog.StringValue(d.String()) }

// Contact son los datos de contacto (Confidenciales; todos opcionales).
type Contact struct {
	Email   string
	Phone   string
	Address string
	City    string
}

func (c Contact) normalized() (Contact, error) {
	out := Contact{
		Email: strings.TrimSpace(c.Email), Phone: strings.TrimSpace(c.Phone),
		Address: strings.TrimSpace(c.Address), City: strings.TrimSpace(c.City),
	}
	if out.Email != "" {
		if addr, err := mail.ParseAddress(out.Email); err != nil || addr.Address != out.Email || len(out.Email) > 254 {
			return Contact{}, ErrInvalidContact
		}
	}
	if len(out.Phone) > 30 || utf8.RuneCountInString(out.Address) > 200 || utf8.RuneCountInString(out.City) > 100 {
		return Contact{}, ErrInvalidContact
	}
	return out, nil
}

// Party es la raíz del agregado.
type Party struct {
	id            PartyID
	kind          Kind
	name          string
	documentType  DocumentType
	documentLast4 string
	contact       Contact
	createdAt     time.Time
	updatedAt     time.Time
	version       int32
}

// NewParty crea un tercero validando sus invariantes.
func NewParty(id PartyID, kind Kind, name string, doc DocumentID, contact Contact, now time.Time) (*Party, error) {
	if kernel.IsZeroID(id) {
		return nil, kernel.ErrInvalidID
	}
	if kind != KindPerson && kind != KindOrganization {
		return nil, ErrInvalidKind
	}
	p := &Party{id: id, kind: kind, createdAt: now.UTC(), updatedAt: now.UTC(), version: 1}
	if err := p.setDetails(name, contact); err != nil {
		return nil, err
	}
	if err := p.setDocument(doc); err != nil {
		return nil, err
	}
	return p, nil
}

// Rectify cambia nombre y contacto y, si doc no es nil, el documento (habeas data: rectificación).
func (p *Party) Rectify(name string, contact Contact, doc *DocumentID, now time.Time) error {
	if err := p.setDetails(name, contact); err != nil {
		return err
	}
	if doc != nil {
		if err := p.setDocument(*doc); err != nil {
			return err
		}
	}
	p.updatedAt = now.UTC()
	return nil
}

func (p *Party) setDetails(name string, contact Contact) error {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 2 || n > 200 {
		return ErrInvalidName
	}
	normalized, err := contact.normalized()
	if err != nil {
		return err
	}
	p.name, p.contact = name, normalized
	return nil
}

func (p *Party) setDocument(doc DocumentID) error {
	if (p.kind == KindOrganization) != (doc.Type() == DocumentNIT) {
		return ErrDocumentKind
	}
	p.documentType, p.documentLast4 = doc.Type(), doc.Last4()
	return nil
}

// PartySnapshot es el estado persistido, usado para rehidratar.
type PartySnapshot struct {
	ID            PartyID
	Kind          Kind
	Name          string
	DocumentType  DocumentType
	DocumentLast4 string
	Contact       Contact
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Version       int32
}

// RehydrateParty reconstruye desde la BD sin validaciones de creación.
func RehydrateParty(s PartySnapshot) *Party {
	return &Party{
		id: s.ID, kind: s.Kind, name: s.Name, documentType: s.DocumentType, documentLast4: s.DocumentLast4,
		contact: s.Contact, createdAt: s.CreatedAt, updatedAt: s.UpdatedAt, version: s.Version,
	}
}

// Snapshot devuelve el estado para persistirlo o exponerlo.
func (p *Party) Snapshot() PartySnapshot {
	return PartySnapshot{
		ID: p.id, Kind: p.kind, Name: p.name, DocumentType: p.documentType, DocumentLast4: p.documentLast4,
		Contact: p.contact, CreatedAt: p.createdAt, UpdatedAt: p.updatedAt, Version: p.version,
	}
}

// ID devuelve el identificador.
func (p *Party) ID() PartyID { return p.id }

// Kind devuelve el tipo de tercero.
func (p *Party) Kind() Kind { return p.kind }

// Version devuelve la versión para concurrencia optimista.
func (p *Party) Version() int32 { return p.version }

// LogValue evita que el agregado completo (datos Confidenciales) llegue a un log.
func (p *Party) LogValue() slog.Value { return slog.StringValue(p.id.String()) }

// ConsentStatus indica si el registro autoriza o revoca.
type ConsentStatus string

// Estados de una autorización.
const (
	ConsentGranted ConsentStatus = "granted"
	ConsentRevoked ConsentStatus = "revoked"
)

// Consent es una autorización de tratamiento de datos (Ley 1581/2012). Inmutable.
type Consent struct {
	ID          uuid.UUID
	PartyID     PartyID
	Status      ConsentStatus
	Purpose     string
	TextVersion string
	ConsentDate kernel.Date
	RecordedAt  time.Time
}

// NewConsent valida el registro: finalidad, versión del texto y una fecha que no sea futura.
func NewConsent(id uuid.UUID, party PartyID, status ConsentStatus, purpose, textVersion string, date, today kernel.Date, now time.Time) (Consent, error) {
	purpose, textVersion = strings.TrimSpace(purpose), strings.TrimSpace(textVersion)
	statusOK := status == ConsentGranted || status == ConsentRevoked
	if n := utf8.RuneCountInString(purpose); !statusOK || n < 3 || n > 200 || textVersion == "" || utf8.RuneCountInString(textVersion) > 40 || date.IsZero() {
		return Consent{}, ErrInvalidConsent
	}
	if date.After(today) {
		return Consent{}, ErrFutureConsent
	}
	return Consent{ID: id, PartyID: party, Status: status, Purpose: purpose, TextVersion: textVersion, ConsentDate: date, RecordedAt: now.UTC()}, nil
}

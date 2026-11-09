// Package domain contiene las reglas de la gestión documental.
package domain

import (
	"bytes"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Errores de negocio.
var (
	ErrNotFound         = kernel.NewError(kernel.KindNotFound, "documents.document.not_found", "documento inexistente")
	ErrInvalid          = kernel.NewError(kernel.KindValidation, "documents.document.invalid", "los datos del documento no son válidos")
	ErrFileType         = kernel.NewError(kernel.KindValidation, "documents.file.type_not_allowed", "solo se admiten PDF, PNG o JPEG")
	ErrFileSize         = kernel.NewError(kernel.KindValidation, "documents.file.size_not_allowed", "el archivo está vacío o excede el tamaño permitido")
	ErrNotScanned       = kernel.NewError(kernel.KindConflict, "documents.version.not_scanned", "el archivo sigue en revisión antivirus")
	ErrInfected         = kernel.NewError(kernel.KindConflict, "documents.version.infected", "el antivirus bloqueó este archivo")
	ErrConcurrentUpdate = kernel.NewError(kernel.KindConflict, "documents.concurrent_update", "el documento cambió; vuelva a cargarlo")
)

// OwnerType es el registro al que pertenece un documento.
type OwnerType string

// Dueños admitidos.
const (
	OwnerParty  OwnerType = "party"
	OwnerPolicy OwnerType = "policy"
	OwnerClaim  OwnerType = "claim"
)

// ScanStatus es el resultado del antivirus.
type ScanStatus string

// Estados del antivirus: solo un archivo limpio se descarga.
const (
	ScanPending  ScanStatus = "pending"
	ScanClean    ScanStatus = "clean"
	ScanInfected ScanStatus = "infected"
)

// Document es un documento y su versión vigente.
type Document struct {
	ID            uuid.UUID
	OwnerType     OwnerType
	OwnerID       uuid.UUID
	Title         string
	Sensitive     bool
	CurrentNumber int32
	CreatedOn     kernel.Date
	Version       int32
}

// Validate comprueba los datos al crear el documento.
func (d Document) Validate() error {
	n := utf8.RuneCountInString(strings.TrimSpace(d.Title))
	if n < 2 || n > 200 || d.OwnerID == uuid.Nil {
		return ErrInvalid
	}
	switch d.OwnerType {
	case OwnerParty, OwnerPolicy, OwnerClaim:
		return nil
	default:
		return ErrInvalid
	}
}

// IsSensitive decide la clasificación: los soportes de un siniestro siempre son sensibles porque
// suelen traer historia clínica; lo demás, según quien carga (hallazgo de auditoría de la Fase 5).
func IsSensitive(owner OwnerType, requested bool) bool { return requested || owner == OwnerClaim }

// RetentionUntil es la fecha en que el documento entra a revisión de retención.
func (d Document) RetentionUntil(days int32) kernel.Date { return d.CreatedOn.AddDays(int(days)) }

// Version es un archivo inmutable de un documento.
type Version struct {
	ID         uuid.UUID
	DocumentID uuid.UUID
	Number     int32
	StorageKey string
	MimeType   string
	SizeBytes  int64
	Sha256     []byte
	Encrypted  bool
	Scan       ScanStatus
	CreatedAt  time.Time
}

// Downloadable falla cerrado: solo un archivo limpio se entrega.
func (v Version) Downloadable() error {
	switch v.Scan {
	case ScanClean:
		return nil
	case ScanInfected:
		return ErrInfected
	case ScanPending:
		return ErrNotScanned
	default:
		return ErrNotScanned
	}
}

// DetectFile valida el tamaño y el tipo real del archivo por sus magic bytes, sin creer lo que
// declare el cliente. Devuelve el tipo y la extensión con que se servirá.
func DetectFile(content []byte, maxBytes int64) (mime, ext string, err error) {
	if len(content) == 0 || int64(len(content)) > maxBytes {
		return "", "", ErrFileSize
	}
	switch {
	case bytes.HasPrefix(content, []byte("%PDF-")):
		return "application/pdf", "pdf", nil
	case bytes.HasPrefix(content, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png", "png", nil
	case bytes.HasPrefix(content, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg", "jpg", nil
	default:
		return "", "", ErrFileType
	}
}

// Extension devuelve la extensión de un tipo admitido.
func Extension(mime string) string {
	switch mime {
	case "application/pdf":
		return "pdf"
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpg"
	default:
		return "bin"
	}
}

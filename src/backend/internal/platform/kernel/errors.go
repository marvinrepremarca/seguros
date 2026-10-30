// Package kernel contiene los tipos compartidos por todos los módulos: dinero, fechas de negocio,
// reloj, identificadores y errores de dominio. Solo depende de la stdlib, decimal y uuid.
package kernel

import (
	"errors"
	"fmt"
)

// ErrorKind clasifica un error de dominio para su traducción a HTTP (problem+json) sin que el
// dominio conozca HTTP.
type ErrorKind int

// Clases de error de dominio.
const (
	KindValidation   ErrorKind = iota + 1 // entrada inválida o invariante violada → 422
	KindNotFound                          // recurso inexistente o de otro tenant → 404
	KindConflict                          // estado o versión incompatible → 409
	KindForbidden                         // sin permiso para la operación → 403
	KindUnauthorized                      // sin sesión válida → 401
)

// DomainError es un error de negocio con código estable (ej. "policies.endorsement.outside_coverage_period").
// El código es parte del contrato público: no se renombra.
type DomainError interface {
	error
	Code() string
	Kind() ErrorKind
}

// Error es la implementación estándar de DomainError. Dos *Error son equivalentes para errors.Is
// si tienen el mismo código, de modo que los módulos declaran centinelas y pueden enriquecerlos.
type Error struct {
	code    string
	kind    ErrorKind
	message string
}

// NewError crea un error de dominio. message es un texto para desarrolladores (no se muestra al
// usuario final) y nunca debe contener datos personales.
func NewError(kind ErrorKind, code, message string) *Error {
	return &Error{code: code, kind: kind, message: message}
}

// Error implementa error.
func (e *Error) Error() string { return e.code + ": " + e.message }

// Code devuelve el código estable.
func (e *Error) Code() string { return e.code }

// Kind devuelve la clase del error.
func (e *Error) Kind() ErrorKind { return e.kind }

// Is compara por código.
func (e *Error) Is(target error) bool {
	var t *Error
	if !errors.As(target, &t) {
		return false
	}
	return t.code == e.code
}

// WithMessage devuelve una copia con el mismo código y otro mensaje de detalle.
func (e *Error) WithMessage(format string, args ...any) *Error {
	return &Error{code: e.code, kind: e.kind, message: fmt.Sprintf(format, args...)}
}

// AsDomainError extrae el DomainError de una cadena de errores envueltos.
func AsDomainError(err error) (DomainError, bool) {
	var de DomainError
	if errors.As(err, &de) {
		return de, true
	}
	return nil, false
}

// Package domain contiene el agregado User: el vínculo entre un sujeto de Keycloak y un usuario
// interno de un tenant. Credenciales, MFA y roles viven en Keycloak (rules/security.md: no se
// implementa login propio); aquí solo se guarda lo necesario para identificar al actor.
package domain

import (
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Errores de dominio de identity (códigos estables).
var (
	ErrInvalidSubject     = kernel.NewError(kernel.KindValidation, "identity.user.invalid_subject", "sujeto del proveedor vacío o demasiado largo")
	ErrInvalidDisplayName = kernel.NewError(kernel.KindValidation, "identity.user.invalid_display_name", "nombre vacío o de más de 200 caracteres")
	ErrInvalidEmail       = kernel.NewError(kernel.KindValidation, "identity.user.invalid_email", "correo inválido")
	ErrNotFound           = kernel.NewError(kernel.KindNotFound, "identity.user.not_found", "usuario inexistente")
	ErrConcurrentUpdate   = kernel.NewError(kernel.KindConflict, "identity.user.concurrent_update", "el usuario cambió; reintente")
)

const (
	maxSubject     = 255
	maxDisplayName = 200
	maxEmail       = 254
)

// Profile son los datos del usuario que entrega el proveedor de identidad (Confidenciales).
type Profile struct {
	Email       string
	DisplayName string
}

// LogValue evita que el perfil llegue a los logs (rules/security.md §Logging).
func (Profile) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

func (p Profile) normalized() (Profile, error) {
	p.Email = strings.TrimSpace(p.Email)
	p.DisplayName = strings.TrimSpace(p.DisplayName)
	if p.DisplayName == "" {
		p.DisplayName = p.Email
	}
	if p.DisplayName == "" || utf8.RuneCountInString(p.DisplayName) > maxDisplayName {
		return Profile{}, ErrInvalidDisplayName
	}
	if p.Email != "" && (len(p.Email) > maxEmail || strings.Count(p.Email, "@") != 1 || strings.ContainsAny(p.Email, " \t\r\n")) {
		return Profile{}, ErrInvalidEmail
	}
	return p, nil
}

// User es la raíz del agregado.
type User struct {
	id          kernel.UserID
	tenantID    kernel.TenantID
	subject     string
	profile     Profile
	lastLoginAt time.Time
	createdAt   time.Time
	updatedAt   time.Time
	version     int32
}

// NewUser aprovisiona un usuario en su primer login.
func NewUser(id kernel.UserID, tenant kernel.TenantID, subject string, profile Profile, now time.Time) (*User, error) {
	if kernel.IsZeroID(id) || kernel.IsZeroID(tenant) {
		return nil, kernel.ErrInvalidID
	}
	if subject == "" || len(subject) > maxSubject {
		return nil, ErrInvalidSubject
	}
	p, err := profile.normalized()
	if err != nil {
		return nil, err
	}
	now = now.UTC()
	return &User{
		id: id, tenantID: tenant, subject: subject, profile: p,
		lastLoginAt: now, createdAt: now, updatedAt: now, version: 1,
	}, nil
}

// RehydrateUser reconstruye un usuario desde la persistencia (sin validaciones de creación).
func RehydrateUser(id kernel.UserID, tenant kernel.TenantID, subject string, profile Profile,
	lastLoginAt, createdAt, updatedAt time.Time, version int32,
) *User {
	return &User{
		id: id, tenantID: tenant, subject: subject, profile: profile,
		lastLoginAt: lastLoginAt, createdAt: createdAt, updatedAt: updatedAt, version: version,
	}
}

// RecordLogin registra un login y sincroniza el perfil con el proveedor de identidad.
func (u *User) RecordLogin(profile Profile, now time.Time) error {
	p, err := profile.normalized()
	if err != nil {
		return err
	}
	now = now.UTC()
	u.profile = p
	u.lastLoginAt = now
	u.updatedAt = now
	return nil
}

// ID devuelve el identificador interno.
func (u *User) ID() kernel.UserID { return u.id }

// TenantID devuelve el tenant del usuario.
func (u *User) TenantID() kernel.TenantID { return u.tenantID }

// Subject devuelve el sujeto (sub) del proveedor de identidad.
func (u *User) Subject() string { return u.subject }

// Profile devuelve el perfil (Confidencial).
func (u *User) Profile() Profile { return u.profile }

// LastLoginAt devuelve el instante del último login.
func (u *User) LastLoginAt() time.Time { return u.lastLoginAt }

// CreatedAt devuelve el instante de creación.
func (u *User) CreatedAt() time.Time { return u.createdAt }

// UpdatedAt devuelve el instante de la última modificación.
func (u *User) UpdatedAt() time.Time { return u.updatedAt }

// Version devuelve la versión para concurrencia optimista.
func (u *User) Version() int32 { return u.version }

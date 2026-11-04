// Package domain contiene el agregado Tenant (agencia cliente del SaaS): plan comercial, estado y
// vínculo con la Organization de Keycloak que agrupa a sus usuarios. Sin lógica de seguros.
package domain

import (
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Plan es el plan comercial del tenant (DECISIONS.md P1 y §3). Determina los módulos encendidos
// por defecto; los add-ons se otorgan con overrides de flags, no cambiando el plan.
type Plan string

// Planes comerciales.
const (
	PlanBasic        Plan = "basico"
	PlanProfessional Plan = "profesional"
	PlanEnterprise   Plan = "empresarial"
)

// ParsePlan valida un plan.
func ParsePlan(s string) (Plan, error) {
	switch p := Plan(s); p {
	case PlanBasic, PlanProfessional, PlanEnterprise:
		return p, nil
	default:
		return "", ErrInvalidPlan
	}
}

// Status es el estado del tenant. Un tenant suspendido no puede usar la aplicación, pero sus
// datos se conservan intactos.
type Status string

// Estados del tenant.
const (
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
)

// transitions es la tabla explícita de transiciones válidas.
var transitions = map[Status][]Status{ //nolint:gochecknoglobals // tabla inmutable de transiciones
	StatusActive:    {StatusSuspended},
	StatusSuspended: {StatusActive},
}

const maxNameLength = 120

// orgAliasPattern acota el alias de la Organization de Keycloak (minúsculas, dígitos y guiones).
var orgAliasPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`) //nolint:gochecknoglobals // regexp inmutable

// ChangeNote justifica una operación de plataforma sobre un tenant: quién y por qué. Queda en la
// bitácora, así que no debe contener datos personales de clientes (rules/security.md).
type ChangeNote struct {
	reason   string
	operator string
}

// NewChangeNote valida motivo (3 a 200 bytes, el límite de audit.Entry) y operador (1 a 120 bytes).
func NewChangeNote(reason, operator string) (ChangeNote, error) {
	reason, operator = strings.TrimSpace(reason), strings.TrimSpace(operator)
	if len(reason) < 3 || len(reason) > 200 {
		return ChangeNote{}, ErrInvalidReason
	}
	if operator == "" || len(operator) > 120 {
		return ChangeNote{}, ErrInvalidOperator
	}
	return ChangeNote{reason: reason, operator: operator}, nil
}

// Reason devuelve el motivo.
func (n ChangeNote) Reason() string { return n.reason }

// Operator devuelve la persona que ejecuta la operación.
func (n ChangeNote) Operator() string { return n.operator }

// Tenant es la raíz del agregado.
type Tenant struct {
	id          kernel.TenantID
	name        string
	plan        Plan
	status      Status
	orgAlias    string
	createdAt   time.Time
	updatedAt   time.Time
	version     int32
	statusSince time.Time
}

// NewTenant crea un tenant activo validando sus invariantes.
func NewTenant(id kernel.TenantID, name string, plan Plan, orgAlias string, now time.Time) (*Tenant, error) {
	if kernel.IsZeroID(id) {
		return nil, kernel.ErrInvalidID
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxNameLength {
		return nil, ErrInvalidName
	}
	if _, err := ParsePlan(string(plan)); err != nil {
		return nil, err
	}
	if !orgAliasPattern.MatchString(orgAlias) {
		return nil, ErrInvalidOrgAlias
	}
	now = now.UTC()
	return &Tenant{
		id: id, name: name, plan: plan, status: StatusActive, orgAlias: orgAlias,
		createdAt: now, updatedAt: now, version: 1, statusSince: now,
	}, nil
}

// TenantSnapshot es el estado persistido de un Tenant, usado para rehidratarlo.
type TenantSnapshot struct {
	ID          kernel.TenantID
	Name        string
	Plan        Plan
	Status      Status
	OrgAlias    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	StatusSince time.Time
	Version     int32
}

// RehydrateTenant reconstruye un Tenant desde la BD sin validaciones de creación.
func RehydrateTenant(s TenantSnapshot) *Tenant {
	return &Tenant{
		id: s.ID, name: s.Name, plan: s.Plan, status: s.Status, orgAlias: s.OrgAlias,
		createdAt: s.CreatedAt, updatedAt: s.UpdatedAt, statusSince: s.StatusSince, version: s.Version,
	}
}

// Snapshot devuelve el estado para persistirlo.
func (t *Tenant) Snapshot() TenantSnapshot {
	return TenantSnapshot{
		ID: t.id, Name: t.name, Plan: t.plan, Status: t.status, OrgAlias: t.orgAlias,
		CreatedAt: t.createdAt, UpdatedAt: t.updatedAt, StatusSince: t.statusSince, Version: t.version,
	}
}

// ID devuelve el identificador.
func (t *Tenant) ID() kernel.TenantID { return t.id }

// Name devuelve el nombre comercial de la agencia.
func (t *Tenant) Name() string { return t.name }

// Plan devuelve el plan comercial.
func (t *Tenant) Plan() Plan { return t.plan }

// Status devuelve el estado.
func (t *Tenant) Status() Status { return t.status }

// OrgAlias devuelve el alias de la Organization de Keycloak vinculada.
func (t *Tenant) OrgAlias() string { return t.orgAlias }

// Version devuelve la versión para concurrencia optimista.
func (t *Tenant) Version() int32 { return t.version }

// IsActive indica si el tenant puede usar la aplicación.
func (t *Tenant) IsActive() bool { return t.status == StatusActive }

// Suspend suspende el tenant (impago, solicitud, incidente). Sus datos se conservan.
func (t *Tenant) Suspend(now time.Time) error { return t.transitionTo(StatusSuspended, now) }

// Reactivate vuelve a activar un tenant suspendido.
func (t *Tenant) Reactivate(now time.Time) error { return t.transitionTo(StatusActive, now) }

// ChangePlan cambia el plan comercial.
func (t *Tenant) ChangePlan(plan Plan, now time.Time) error {
	if _, err := ParsePlan(string(plan)); err != nil {
		return err
	}
	if plan == t.plan {
		return nil
	}
	t.plan = plan
	t.touch(now)
	return nil
}

func (t *Tenant) transitionTo(to Status, now time.Time) error {
	if !slices.Contains(transitions[t.status], to) {
		return ErrInvalidTransition.WithMessage("%s → %s", t.status, to)
	}
	t.status = to
	t.statusSince = now.UTC()
	t.touch(now)
	return nil
}

func (t *Tenant) touch(now time.Time) {
	t.updatedAt = now.UTC()
}

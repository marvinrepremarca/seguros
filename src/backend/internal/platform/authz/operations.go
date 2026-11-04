package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// Valores especiales de x-permission.
const (
	// XPermission es la extensión OpenAPI que declara el permiso de cada operación.
	XPermission = "x-permission"
	// PermPublic: operación sin sesión (lista blanca: login, callback).
	PermPublic = "public"
	// PermAuthenticated: basta una sesión válida (ej. /bff/me).
	PermAuthenticated = "authenticated"
	// permAlternative separa permisos alternativos: "x.y.read|x.y.read-own". Basta tener uno; el caso de
	// uso decide el alcance.
	permAlternative = "|"
)

// OperationGuard aplica el x-permission declarado en el contrato OpenAPI a cada operación. Es
// deny-by-default: una operación sin x-permission impide construir el guardia (falla al arrancar),
// y una operación desconocida en tiempo de ejecución se rechaza.
type OperationGuard struct {
	reqs map[string]string // operationId → public | authenticated | permiso
}

// NewOperationGuard lee el x-permission de todas las operaciones del contrato. Falla si alguna no
// lo declara o declara un permiso que no está en la política.
func NewOperationGuard(spec *openapi3.T, policy *Policy) (*OperationGuard, error) {
	g := &OperationGuard{reqs: map[string]string{}}
	if spec == nil || spec.Paths == nil {
		return nil, fmt.Errorf("authz: contrato vacío")
	}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			if op.OperationID == "" {
				return nil, fmt.Errorf("authz: %s %s sin operationId", method, path)
			}
			raw, ok := op.Extensions[XPermission].(string)
			if !ok || raw == "" {
				return nil, fmt.Errorf("authz: %s %s (%s) no declara %s", method, path, op.OperationID, XPermission)
			}
			if raw != PermPublic && raw != PermAuthenticated {
				for _, p := range strings.Split(raw, permAlternative) {
					if !policy.Known(Permission(p)) {
						return nil, fmt.Errorf("authz: %s declara un permiso fuera del catálogo: %q", op.OperationID, p)
					}
				}
			}
			g.reqs[op.OperationID] = raw
		}
	}
	return g, nil
}

// Check autoriza la operación para el principal del contexto.
func (g *OperationGuard) Check(ctx context.Context, operationID string) error {
	req, ok := g.reqs[operationID]
	switch {
	case !ok:
		return ErrForbidden.WithMessage("operación sin permiso registrado: %s", operationID)
	case req == PermPublic:
		return nil
	case req == PermAuthenticated:
		return RequireAuthenticated(ctx)
	default:
		return requireAny(ctx, strings.Split(req, permAlternative))
	}
}

func requireAny(ctx context.Context, permissions []string) error {
	var err error
	for _, p := range permissions {
		if err = Require(ctx, Permission(p)); err == nil || !errors.Is(err, ErrForbidden) {
			return err
		}
	}
	return err
}

// Requirement devuelve el x-permission declarado para la operación.
func (g *OperationGuard) Requirement(operationID string) (string, bool) {
	r, ok := g.reqs[operationID]
	return r, ok
}

// PublicOperations lista las operaciones públicas (para la prueba de lista blanca).
func (g *OperationGuard) PublicOperations() []string {
	var out []string
	for op, r := range g.reqs {
		if r == PermPublic {
			out = append(out, op)
		}
	}
	sort.Strings(out)
	return slices.Clip(out)
}

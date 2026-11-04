package app

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/parties/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// GetPartyHandler consulta un tercero.
type GetPartyHandler struct{ Deps }

// Handle devuelve el tercero (404 si es de otro tenant).
func (h *GetPartyHandler) Handle(ctx context.Context, id domain.PartyID) (domain.PartySnapshot, error) {
	scope, err := readScope(ctx)
	if err != nil {
		return domain.PartySnapshot{}, err
	}
	var out domain.PartySnapshot
	err = h.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		p, err := getInScope(ctx, uow.Parties(), id, scope)
		if err != nil {
			return err
		}
		out = p.Snapshot()
		return nil
	})
	return out, err
}

// ListPartiesQuery filtra y pagina.
type ListPartiesQuery struct {
	Q      string
	Cursor string
	Limit  int32
}

// PartyPage es una página de terceros.
type PartyPage struct {
	Items      []domain.PartySnapshot
	NextCursor string
}

// ListPartiesHandler lista terceros.
type ListPartiesHandler struct{ Deps }

// Handle pagina por ID (keyset), con búsqueda opcional por parte del nombre.
func (h *ListPartiesHandler) Handle(ctx context.Context, q ListPartiesQuery) (PartyPage, error) {
	scope, err := readScope(ctx)
	if err != nil {
		return PartyPage{}, err
	}
	before, err := kernel.DecodeCursor(q.Cursor)
	if err != nil {
		return PartyPage{}, err
	}
	size := kernel.PageSize(q.Limit)
	filter := ListFilter{Before: before, Limit: size + 1, VisibleTo: scope}
	if t := strings.TrimSpace(q.Q); t != "" {
		filter.NamePattern = "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(t) + "%"
	}
	var page PartyPage
	err = h.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		rows, err := uow.Parties().List(ctx, filter)
		if err != nil {
			return err
		}
		if int32(len(rows)) > size { //nolint:gosec // acotado por MaxPageSize + 1
			rows = rows[:size]
			page.NextCursor = kernel.EncodeCursor(uuid.UUID(rows[size-1].ID()))
		}
		page.Items = make([]domain.PartySnapshot, 0, len(rows))
		for _, p := range rows {
			page.Items = append(page.Items, p.Snapshot())
		}
		return nil
	})
	return page, err
}

// ListConsentsHandler consulta el historial de autorizaciones.
type ListConsentsHandler struct{ Deps }

// Handle devuelve el historial del tercero (404 si no existe).
func (h *ListConsentsHandler) Handle(ctx context.Context, id domain.PartyID) ([]domain.Consent, error) {
	if err := authz.Require(ctx, authz.ConsentRead); err != nil {
		return nil, err
	}
	scope, err := readScope(ctx)
	if err != nil {
		return nil, err
	}
	var out []domain.Consent
	err = h.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if _, err := getInScope(ctx, uow.Parties(), id, scope); err != nil {
			return err
		}
		var err error
		out, err = uow.Parties().ListConsents(ctx, id)
		return err
	})
	return out, err
}

package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/renewals/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// View es un caso con el nombre del tomador.
type View struct {
	domain.Snapshot
	HolderName string
}

func (s *Service) views(ctx context.Context, rows []domain.Snapshot) ([]View, error) {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.HolderPartyID)
	}
	names, err := s.Parties.Summaries(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("renewals: consultando tomadores: %w", err)
	}
	out := make([]View, 0, len(rows))
	for _, r := range rows {
		out = append(out, View{Snapshot: r, HolderName: names[r.HolderPartyID].Name})
	}
	return out, nil
}

// ListCases consulta los casos.
func (s *Service) ListCases(ctx context.Context, status, cursor string, limit int32) ([]View, string, error) {
	if err := authz.Require(ctx, authz.RenewalRead); err != nil {
		return nil, "", err
	}
	after, err := kernel.DecodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	size := kernel.PageSize(limit)
	var rows []domain.Snapshot
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		rows, err = uow.Renewals().ListCases(ctx, status, after, size+1)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if int32(len(rows)) > size { //nolint:gosec // acotado por el límite
		rows = rows[:size]
		next = kernel.EncodeCursor(rows[size-1].ID)
	}
	out, err := s.views(ctx, rows)
	return out, next, err
}

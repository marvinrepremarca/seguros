package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// bucketDays traduce un tramo a días de vencida.
func bucketDays(bucket string) (int32, int32) {
	const never = 1 << 30
	switch bucket {
	case "not_due":
		return -never, -1
	case "d0_30":
		return 0, 30
	case "d31_60":
		return 31, 60
	case "d61_90":
		return 61, 90
	case "d90_plus":
		return 91, never
	default:
		return -never, never
	}
}

// ReceivablesPage es una página de cartera.
type ReceivablesPage struct {
	Items      []Receivable
	NextCursor string
}

// ListReceivables consulta la cartera.
func (s *Service) ListReceivables(ctx context.Context, bucket, cursor string, limit int32) (ReceivablesPage, error) {
	if err := authz.Require(ctx, authz.BillingPlanRead); err != nil {
		return ReceivablesPage{}, err
	}
	after, err := kernel.DecodeCursor(cursor)
	if err != nil {
		return ReceivablesPage{}, err
	}
	size := kernel.PageSize(limit)
	minDays, maxDays := bucketDays(bucket)
	var rows []Receivable
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		rows, err = uow.Billing().ListReceivables(ctx, s.today(), minDays, maxDays, after, size+1)
		return err
	})
	if err != nil {
		return ReceivablesPage{}, err
	}
	var page ReceivablesPage
	if int32(len(rows)) > size { //nolint:gosec // acotado por el límite
		rows = rows[:size]
		page.NextCursor = kernel.EncodeCursor(rows[size-1].InstallmentID)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.HolderPartyID)
	}
	names, err := s.Parties.Summaries(ctx, ids)
	if err != nil {
		return ReceivablesPage{}, fmt.Errorf("billing: consultando tomadores: %w", err)
	}
	for i := range rows {
		rows[i].HolderName = names[rows[i].HolderPartyID].Name
	}
	page.Items = rows
	return page, nil
}

// Summary resume la cartera por antigüedad.
func (s *Service) Summary(ctx context.Context) ([]Bucket, error) {
	if err := authz.Require(ctx, authz.BillingPlanRead); err != nil {
		return nil, err
	}
	var out []Bucket
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		out, err = uow.Billing().Summary(ctx, s.today())
		return err
	})
	return out, err
}

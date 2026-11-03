package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	identitysqlc "github.com/marvinrepremarca/seguros/internal/modules/identity/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/modules/identity/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

var _ contracts.Directory = (*Store)(nil)

// Users implementa contracts.Directory.
func (s *Store) Users(ctx context.Context) ([]contracts.User, error) {
	tenant, ok := reqctx.TenantID(ctx)
	if !ok {
		return nil, db.ErrNoTenant
	}
	var out []contracts.User
	err := s.db.WithTenantTx(ctx, func(tx pgx.Tx) error {
		rows, err := identitysqlc.New(tx).ListUsers(ctx, uuid.UUID(tenant))
		if err != nil {
			return fmt.Errorf("identity: listando usuarios: %w", err)
		}
		out = make([]contracts.User, 0, len(rows))
		for _, r := range rows {
			out = append(out, contracts.User{ID: r.ID, DisplayName: r.DisplayName})
		}
		return nil
	})
	return out, err
}

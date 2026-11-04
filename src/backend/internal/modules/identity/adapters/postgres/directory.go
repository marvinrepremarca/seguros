package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	identitysqlc "github.com/marvinrepremarca/seguros/internal/modules/identity/adapters/postgres/sqlc"
	"github.com/marvinrepremarca/seguros/internal/modules/identity/app"
	"github.com/marvinrepremarca/seguros/internal/platform/db"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

var _ app.TenantUsers = (*Store)(nil)

// TenantUsers implementa app.TenantUsers.
func (s *Store) TenantUsers(ctx context.Context) ([]app.TenantUser, error) {
	tenant, ok := reqctx.TenantID(ctx)
	if !ok {
		return nil, db.ErrNoTenant
	}
	var out []app.TenantUser
	err := s.db.WithTenantTx(ctx, func(tx pgx.Tx) error {
		rows, err := identitysqlc.New(tx).ListUsers(ctx, uuid.UUID(tenant))
		if err != nil {
			return fmt.Errorf("identity: listando usuarios: %w", err)
		}
		out = make([]app.TenantUser, 0, len(rows))
		for _, r := range rows {
			out = append(out, app.TenantUser{ID: r.ID, Subject: r.IdpSubject, DisplayName: r.DisplayName, LeftOrganization: r.LeftOrganizationAt != nil})
		}
		return nil
	})
	return out, err
}

// MarkLeftOrganization registra que el usuario salió de la organización del tenant. El siguiente login
// lo revierte. Fija el tenant aquí: la revalidación corre antes de que el contexto tenga principal.
func (s *Store) MarkLeftOrganization(ctx context.Context, tenant kernel.TenantID, user kernel.UserID, at time.Time) error {
	ctx = reqctx.WithTenantID(ctx, tenant)
	return s.db.WithTenantTx(ctx, func(tx pgx.Tx) error {
		if err := identitysqlc.New(tx).MarkUserLeftOrganization(ctx, identitysqlc.MarkUserLeftOrganizationParams{
			LeftAt: &at, TenantID: uuid.UUID(tenant), ID: uuid.UUID(user),
		}); err != nil {
			return fmt.Errorf("identity: registrando salida de la organización: %w", err)
		}
		return nil
	})
}

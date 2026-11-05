package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/claims/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

const healthField = "claims.health"

// SetHealth cifra y guarda el detalle de salud, ligado al ID del siniestro (DECISIONS N30).
func (s *Service) SetHealth(ctx context.Context, id uuid.UUID, details string) error {
	if err := authz.Require(ctx, authz.ClaimHealthWrite); err != nil {
		return err
	}
	if err := domain.ValidateHealth(details); err != nil {
		return err
	}
	sealed, err := s.Cipher.EncryptFor(ctx, healthField, id.String(), []byte(details))
	if err != nil {
		return fmt.Errorf("claims: cifrando: %w", err)
	}
	return s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Claims().SetHealth(ctx, id, sealed, s.Clock.Now().UTC()); err != nil {
			return err
		}
		return uow.Audit(ctx, entry("claims.health.written", id))
	})
}

// Health descifra el detalle. La lectura queda auditada antes de entregarlo.
func (s *Service) Health(ctx context.Context, id uuid.UUID) (string, error) {
	if err := authz.Require(ctx, authz.ClaimHealthRead); err != nil {
		return "", err
	}
	var sealed []byte
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		sealed, err = uow.Claims().Health(ctx, id)
		if err != nil {
			return err
		}
		return uow.Audit(ctx, entry("claims.health.read", id))
	})
	if err != nil {
		return "", err
	}
	plain, err := s.Cipher.DecryptFor(ctx, healthField, id.String(), sealed)
	if err != nil {
		return "", fmt.Errorf("claims: descifrando: %w", err)
	}
	return string(plain), nil
}

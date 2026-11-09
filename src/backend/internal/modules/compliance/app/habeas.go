package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/compliance/domain"
	parties "github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// Export entrega los datos del titular. El exporte queda auditado (rules/security.md).
func (s *Service) Export(ctx context.Context, party uuid.UUID) (parties.HabeasRecord, error) {
	if err := authz.Require(ctx, authz.HabeasExport); err != nil {
		return parties.HabeasRecord{}, err
	}
	rec, found, err := s.Parties.HabeasExport(ctx, party)
	if err != nil {
		return parties.HabeasRecord{}, fmt.Errorf("compliance: consultando titular: %w", err)
	}
	if !found {
		return parties.HabeasRecord{}, domain.ErrPartyNotFound
	}
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		return uow.Audit(ctx, entry("compliance.habeas.exported", "parties.party", party))
	})
	if err != nil {
		return parties.HabeasRecord{}, err
	}
	return rec, nil
}

// RequestSuppression registra la solicitud. No borra datos: se decide a mano (DECISIONS N33).
func (s *Service) RequestSuppression(ctx context.Context, party uuid.UUID) (uuid.UUID, error) {
	if err := authz.Require(ctx, authz.HabeasSuppress); err != nil {
		return uuid.Nil, err
	}
	if _, found, err := s.Parties.DocumentFingerprint(ctx, party); err != nil {
		return uuid.Nil, fmt.Errorf("compliance: consultando titular: %w", err)
	} else if !found {
		return uuid.Nil, domain.ErrPartyNotFound
	}
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("compliance: %w", err)
	}
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Records().InsertSuppression(ctx, id, party, s.Clock.Now().UTC()); err != nil {
			return err
		}
		return uow.Audit(ctx, entry("compliance.suppression.requested", "compliance.suppression", id))
	})
	return id, err
}

// ResolveSuppression registra la decisión del oficial.
func (s *Service) ResolveSuppression(ctx context.Context, id uuid.UUID, accepted bool, resolution string) error {
	if err := authz.Require(ctx, authz.HabeasSuppress); err != nil {
		return err
	}
	if err := domain.ValidateResolution(resolution); err != nil {
		return err
	}
	return s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Records().ResolveSuppression(ctx, id, accepted, resolution, s.Clock.Now().UTC()); err != nil {
			return err
		}
		return uow.Audit(ctx, entry("compliance.suppression.resolved", "compliance.suppression", id))
	})
}

// ListSuppressions pagina las solicitudes.
func (s *Service) ListSuppressions(ctx context.Context, cursor string, limit int32) ([]Suppression, string, error) {
	if err := authz.Require(ctx, authz.HabeasSuppress); err != nil {
		return nil, "", err
	}
	before, err := kernel.DecodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	size := kernel.PageSize(limit)
	var rows []Suppression
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		rows, err = uow.Records().Suppressions(ctx, before, size+1)
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
	return rows, next, nil
}

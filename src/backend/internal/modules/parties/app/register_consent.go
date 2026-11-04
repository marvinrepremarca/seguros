package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/parties/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// RegisterConsentCommand registra una autorización o revocación.
type RegisterConsentCommand struct {
	PartyID     domain.PartyID
	Status      domain.ConsentStatus
	Purpose     string
	TextVersion string
	ConsentDate kernel.Date
}

// RegisterConsentHandler registra autorizaciones.
type RegisterConsentHandler struct{ Deps }

// Handle guarda un registro inmutable y lo audita.
func (h *RegisterConsentHandler) Handle(ctx context.Context, cmd RegisterConsentCommand) (domain.Consent, error) {
	if err := authz.Require(ctx, authz.ConsentCreate); err != nil {
		return domain.Consent{}, err
	}
	scope, err := readScope(ctx)
	if err != nil {
		return domain.Consent{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return domain.Consent{}, fmt.Errorf("parties: %w", err)
	}
	consent, err := domain.NewConsent(id, cmd.PartyID, cmd.Status, cmd.Purpose, cmd.TextVersion, cmd.ConsentDate, kernel.Today(h.Clock), h.Clock.Now())
	if err != nil {
		return domain.Consent{}, err
	}
	err = h.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if _, err := getInScope(ctx, uow.Parties(), cmd.PartyID, scope); err != nil {
			return err
		}
		if err := uow.Parties().InsertConsent(ctx, consent); err != nil {
			return err
		}
		return uow.Audit(ctx, partyEntry("parties.consent.registered", cmd.PartyID, map[string]string{
			"status": string(consent.Status), "textVersion": consent.TextVersion,
		}))
	})
	if err != nil {
		return domain.Consent{}, err
	}
	return consent, nil
}

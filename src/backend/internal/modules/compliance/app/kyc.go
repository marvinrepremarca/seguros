package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/compliance/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/compliance/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

const (
	kycField    = "compliance.kyc.details"
	resultField = "compliance.kyc.result"
	sourceField = "compliance.kyc.source"
)

// RecordInput son los datos de la consulta hecha por el oficial.
type RecordInput struct {
	PartyID   uuid.UUID
	Approved  bool
	Source    string
	CheckedOn kernel.Date
	Details   string
}

// Record registra una verificación inmutable y publica su resultado (DECISIONS N33).
func (s *Service) Record(ctx context.Context, in RecordInput) (domain.KycCheck, error) {
	if err := authz.Require(ctx, authz.KycRecord); err != nil {
		return domain.KycCheck{}, err
	}
	fingerprint, found, err := s.Parties.DocumentFingerprint(ctx, in.PartyID)
	if err != nil {
		return domain.KycCheck{}, fmt.Errorf("compliance: consultando tercero: %w", err)
	}
	if !found {
		return domain.KycCheck{}, domain.ErrPartyNotFound
	}
	id, err := uuid.NewV7()
	if err != nil {
		return domain.KycCheck{}, fmt.Errorf("compliance: %w", err)
	}
	result := domain.ResultRejected
	if in.Approved {
		result = domain.ResultApproved
	}
	check, err := domain.NewKycCheck(domain.KycCheck{ID: id, PartyID: in.PartyID, Fingerprint: fingerprint, Result: result, Source: in.Source, CheckedOn: in.CheckedOn}, kernel.Today(s.Clock))
	if err != nil {
		return domain.KycCheck{}, err
	}
	var sealed SealedCheck
	if sealed.Result, err = s.Cipher.EncryptFor(ctx, resultField, id.String(), []byte(check.Result)); err != nil {
		return domain.KycCheck{}, fmt.Errorf("compliance: cifrando: %w", err)
	}
	if sealed.Source, err = s.Cipher.EncryptFor(ctx, sourceField, id.String(), []byte(check.Source)); err != nil {
		return domain.KycCheck{}, fmt.Errorf("compliance: cifrando: %w", err)
	}
	if in.Details != "" {
		if sealed.Details, err = s.Cipher.EncryptFor(ctx, kycField, id.String(), []byte(in.Details)); err != nil {
			return domain.KycCheck{}, fmt.Errorf("compliance: cifrando: %w", err)
		}
		check.HasDetails = true
	}
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Records().InsertCheck(ctx, check, sealed, s.Clock.Now().UTC()); err != nil {
			return err
		}
		if err := uow.Publish(ctx, contracts.TopicKycCheckCompleted, contracts.KycCheckCompleted{CheckID: id, PartyID: in.PartyID}); err != nil {
			return err
		}
		e := entry("compliance.kyc.recorded", "compliance.kyc_check", id)
		e.Details = map[string]string{"party": in.PartyID.String()}
		return uow.Audit(ctx, e)
	})
	return check, err
}

// Status es la situación vigente de KYC de un tercero.
type Status struct {
	Approved bool
	Stale    bool // la última verificación corresponde a otro documento
	Checks   []domain.KycCheck
}

// History devuelve las verificaciones y si la última aprueba el documento vigente.
func (s *Service) History(ctx context.Context, party uuid.UUID) (Status, error) {
	if err := authz.Require(ctx, authz.KycRead); err != nil {
		return Status{}, err
	}
	current, found, err := s.Parties.DocumentFingerprint(ctx, party)
	if err != nil {
		return Status{}, fmt.Errorf("compliance: consultando tercero: %w", err)
	}
	if !found {
		return Status{}, domain.ErrPartyNotFound
	}
	var out Status
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		// Un resultado SARLAFT es Restringido: su lectura queda en la bitácora.
		e := entry("compliance.kyc.history_read", "compliance.party", party)
		if err := uow.Audit(ctx, e); err != nil {
			return err
		}
		stored, err := uow.Records().Checks(ctx, party)
		if err != nil {
			return err
		}
		for _, c := range stored {
			open, err := s.open(ctx, c)
			if err != nil {
				return err
			}
			out.Checks = append(out.Checks, open)
		}
		return nil
	})
	if err != nil {
		return Status{}, err
	}
	if len(out.Checks) > 0 {
		out.Approved = out.Checks[0].Approves(current)
		out.Stale = out.Checks[0].Result == domain.ResultApproved && !out.Approved
	}
	return out, nil
}

// Details descifra el detalle de una verificación. La lectura queda auditada antes de entregarlo.
func (s *Service) Details(ctx context.Context, party, check uuid.UUID) (string, error) {
	if err := authz.Require(ctx, authz.KycDetailsRead); err != nil {
		return "", err
	}
	var sealed []byte
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		if sealed, err = uow.Records().CheckDetails(ctx, party, check); err != nil {
			return err
		}
		return uow.Audit(ctx, entry("compliance.kyc.details_read", "compliance.kyc_check", check))
	})
	if err != nil {
		return "", err
	}
	plain, err := s.Cipher.DecryptFor(ctx, kycField, check.String(), sealed)
	if err != nil {
		return "", fmt.Errorf("compliance: descifrando: %w", err)
	}
	return string(plain), nil
}

// Statuses implementa contracts.KycStatuses para la expedición (DECISIONS N4).
type Statuses struct{ Service *Service }

var _ contracts.KycStatuses = Statuses{}

// IsApproved responde sobre la verificación más reciente y el documento vigente. Sin permiso propio: la
// invoca Policies al expedir, que ya autorizó su operación.
func (st Statuses) IsApproved(ctx context.Context, party uuid.UUID) (bool, error) {
	current, found, err := st.Service.Parties.DocumentFingerprint(ctx, party)
	if err != nil || !found {
		return false, err
	}
	var approved bool
	err = st.Service.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		stored, ok, err := uow.Records().LatestCheck(ctx, party)
		if err != nil || !ok {
			return err
		}
		c, err := st.Service.open(ctx, stored)
		approved = err == nil && c.Approves(current)
		return err
	})
	return approved, err
}

// open descifra resultado y fuente; las filas previas al cifrado ya traen el texto plano.
func (s *Service) open(ctx context.Context, c StoredCheck) (domain.KycCheck, error) {
	out := c.Check
	if c.ResultSealed == nil {
		return out, nil
	}
	result, err := s.Cipher.DecryptFor(ctx, resultField, out.ID.String(), c.ResultSealed)
	if err != nil {
		return domain.KycCheck{}, fmt.Errorf("compliance: descifrando resultado: %w", err)
	}
	source, err := s.Cipher.DecryptFor(ctx, sourceField, out.ID.String(), c.SourceSealed)
	if err != nil {
		return domain.KycCheck{}, fmt.Errorf("compliance: descifrando fuente: %w", err)
	}
	out.Result, out.Source = domain.Result(result), string(source)
	return out, nil
}

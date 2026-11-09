package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/parties/contracts"
	"github.com/marvinrepremarca/seguros/internal/modules/parties/domain"
)

// Directory implementa contracts.Directory para otros módulos.
type Directory struct{ Deps }

var _ contracts.Directory = (*Directory)(nil)

// Summaries implementa contracts.Directory. Sin permiso propio: lo invoca un caso de uso que ya
// autorizó su operación, y solo expone el nombre (nunca el documento).
func (d *Directory) Summaries(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]contracts.PartySummary, error) {
	var out map[uuid.UUID]contracts.PartySummary
	err := d.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		out, err = uow.Parties().Summaries(ctx, ids)
		return err
	})
	return out, err
}

var _ contracts.AgentScope = (*Directory)(nil)

// VisibleToAgent implementa contracts.AgentScope.
func (d *Directory) VisibleToAgent(ctx context.Context, party, agent uuid.UUID) (bool, error) {
	var visible bool
	err := d.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		visible, err = uow.Parties().IsVisibleTo(ctx, domain.PartyID(party), agent)
		return err
	})
	return visible, err
}

var _ contracts.ComplianceView = (*Directory)(nil)

// DocumentFingerprint implementa contracts.ComplianceView.
func (d *Directory) DocumentFingerprint(ctx context.Context, party uuid.UUID) ([]byte, bool, error) {
	var out []byte
	err := d.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		out, err = uow.Parties().DocumentIndex(ctx, domain.PartyID(party))
		return err
	})
	if errors.Is(err, domain.ErrNotFound) {
		return nil, false, nil
	}
	return out, err == nil, err
}

// Contact implementa contracts.ContactView.
func (d *Directory) Contact(ctx context.Context, party uuid.UUID) (contracts.Contact, bool, error) {
	r, ok, err := d.HabeasExport(ctx, party)
	return contracts.Contact{Name: r.Name, Email: r.Email, Consents: r.Consents}, ok, err
}

// HabeasExport implementa contracts.ComplianceView.
func (d *Directory) HabeasExport(ctx context.Context, party uuid.UUID) (contracts.HabeasRecord, bool, error) {
	var out contracts.HabeasRecord
	err := d.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		p, err := uow.Parties().Get(ctx, domain.PartyID(party))
		if err != nil {
			return err
		}
		s := p.Snapshot()
		out = contracts.HabeasRecord{
			Kind: string(s.Kind), Name: s.Name, DocumentType: string(s.DocumentType), DocumentLast4: s.DocumentLast4,
			Email: s.Contact.Email, Phone: s.Contact.Phone, Address: s.Contact.Address, City: s.Contact.City,
		}
		consents, err := uow.Parties().ListConsents(ctx, domain.PartyID(party))
		if err != nil {
			return err
		}
		for _, c := range consents {
			out.Consents = append(out.Consents, contracts.HabeasConsent{Status: string(c.Status), Purpose: c.Purpose, TextVersion: c.TextVersion, ConsentDate: c.ConsentDate.String()})
		}
		return nil
	})
	if errors.Is(err, domain.ErrNotFound) {
		return contracts.HabeasRecord{}, false, nil
	}
	return out, err == nil, err
}

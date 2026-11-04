package app

import (
	"context"
	"fmt"

	"github.com/marvinrepremarca/seguros/internal/modules/parties/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
)

// RevealDocumentHandler devuelve el documento completo.
type RevealDocumentHandler struct{ Deps }

// Handle audita la lectura ANTES de descifrar: no hay lectura de dato Restringido sin rastro.
func (h *RevealDocumentHandler) Handle(ctx context.Context, id domain.PartyID) (domain.DocumentType, string, error) {
	if err := authz.Require(ctx, authz.DocumentReveal); err != nil {
		return "", "", err
	}
	var docType domain.DocumentType
	var ciphertext []byte
	err := h.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		docType, ciphertext, err = uow.Parties().GetDocument(ctx, id)
		if err != nil {
			return err
		}
		return uow.Audit(ctx, partyEntry("parties.document.revealed", id, nil))
	})
	if err != nil {
		return "", "", err
	}
	plain, err := h.Cipher.DecryptFor(ctx, documentField, id.String(), ciphertext)
	if err != nil {
		return "", "", fmt.Errorf("parties: descifrando documento: %w", err)
	}
	return docType, string(plain), nil
}

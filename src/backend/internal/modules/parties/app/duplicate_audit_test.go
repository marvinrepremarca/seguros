package app

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/parties/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

type failingStore struct {
	Store
	err error
}

func (s failingStore) WithinTenant(context.Context, func(context.Context, UnitOfWork) error) error {
	return s.err
}

// Sin rastro en la bitácora no se confirma que el documento existe (rules/security.md, fail-closed).
func TestDuplicateIsNotConfirmedIfItCannotBeAudited(t *testing.T) {
	t.Parallel()
	d := Deps{Store: failingStore{err: errors.New("bd caída")}}
	wrapped := fmt.Errorf("guardando: %w", domain.ErrDocumentTaken)

	err := d.withDuplicateAudit(context.Background(), wrapped, domain.DocumentCC, []byte("indice"))
	require.Error(t, err)
	var domainErr kernel.DomainError
	assert.False(t, errors.As(err, &domainErr), "debe responder 500 genérico, no 409")
	assert.NotErrorIs(t, err, domain.ErrDocumentTaken)
}

func TestOtherErrorsPassThroughWithoutAudit(t *testing.T) {
	t.Parallel()
	d := Deps{Store: failingStore{err: errors.New("no debería usarse")}}
	assert.ErrorIs(t, d.withDuplicateAudit(context.Background(), domain.ErrInvalidName, domain.DocumentCC, nil), domain.ErrInvalidName)
	assert.NoError(t, d.withDuplicateAudit(context.Background(), nil, domain.DocumentCC, nil))
}

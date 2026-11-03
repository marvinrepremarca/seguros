package domain_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/identity/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

var now = time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // instante fijo de prueba

func ids() (kernel.UserID, kernel.TenantID) {
	return kernel.UserID(uuid.Must(uuid.NewV7())), kernel.TenantID(uuid.Must(uuid.NewV7()))
}

func TestNewUser(t *testing.T) {
	t.Parallel()
	uid, tid := ids()
	tests := []struct {
		name    string
		user    kernel.UserID
		tenant  kernel.TenantID
		subject string
		profile domain.Profile
		wantErr error
		wantDN  string
	}{
		{"válido", uid, tid, "sub-1", domain.Profile{Email: "a@b.co", DisplayName: " Ana "}, nil, "Ana"},
		{"sin nombre usa el correo", uid, tid, "sub-1", domain.Profile{Email: "a@b.co"}, nil, "a@b.co"},
		{"sin correo", uid, tid, "sub-1", domain.Profile{DisplayName: "Ana"}, nil, "Ana"},
		{"sin id", kernel.UserID{}, tid, "sub-1", domain.Profile{DisplayName: "Ana"}, kernel.ErrInvalidID, ""},
		{"sin tenant", uid, kernel.TenantID{}, "sub-1", domain.Profile{DisplayName: "Ana"}, kernel.ErrInvalidID, ""},
		{"sin sujeto", uid, tid, "", domain.Profile{DisplayName: "Ana"}, domain.ErrInvalidSubject, ""},
		{"sujeto largo", uid, tid, strings.Repeat("s", 256), domain.Profile{DisplayName: "Ana"}, domain.ErrInvalidSubject, ""},
		{"sin nombre ni correo", uid, tid, "sub-1", domain.Profile{}, domain.ErrInvalidDisplayName, ""},
		{"nombre largo", uid, tid, "sub-1", domain.Profile{DisplayName: strings.Repeat("n", 201)}, domain.ErrInvalidDisplayName, ""},
		{"correo sin arroba", uid, tid, "sub-1", domain.Profile{Email: "ab.co", DisplayName: "Ana"}, domain.ErrInvalidEmail, ""},
		{"correo con espacios", uid, tid, "sub-1", domain.Profile{Email: "a @b.co", DisplayName: "Ana"}, domain.ErrInvalidEmail, ""},
		{"correo largo", uid, tid, "sub-1", domain.Profile{Email: strings.Repeat("a", 250) + "@b.co", DisplayName: "Ana"}, domain.ErrInvalidEmail, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			u, err := domain.NewUser(tt.user, tt.tenant, tt.subject, tt.profile, now)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantDN, u.Profile().DisplayName)
			assert.Equal(t, tt.user, u.ID())
			assert.Equal(t, tt.tenant, u.TenantID())
			assert.Equal(t, tt.subject, u.Subject())
			assert.Equal(t, int32(1), u.Version())
			assert.Equal(t, now, u.LastLoginAt())
			assert.Equal(t, now, u.CreatedAt())
			assert.Equal(t, now, u.UpdatedAt())
		})
	}
}

func TestRecordLogin(t *testing.T) {
	t.Parallel()
	uid, tid := ids()
	u := domain.RehydrateUser(uid, tid, "sub", domain.Profile{DisplayName: "Ana"}, now, now, now, 3)
	later := now.Add(time.Hour)

	require.NoError(t, u.RecordLogin(domain.Profile{Email: "ana@b.co", DisplayName: "Ana M"}, later))
	assert.Equal(t, "Ana M", u.Profile().DisplayName)
	assert.Equal(t, later, u.LastLoginAt())
	assert.Equal(t, later, u.UpdatedAt())
	assert.Equal(t, now, u.CreatedAt())
	assert.Equal(t, int32(3), u.Version(), "la versión la incrementa la persistencia")

	require.ErrorIs(t, u.RecordLogin(domain.Profile{}, later), domain.ErrInvalidDisplayName)
	assert.Equal(t, "Ana M", u.Profile().DisplayName, "un perfil inválido no modifica el usuario")
}

func TestProfileIsRedactedInLogs(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "profile", domain.Profile{Email: "ana@b.co", DisplayName: "Ana"})
	assert.NotContains(t, buf.String(), "ana@b.co")
	assert.NotContains(t, buf.String(), "Ana")
}

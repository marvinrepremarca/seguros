package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

func TestDetectFile(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content []byte
		mime    string
		err     error
	}{
		{"pdf", []byte("%PDF-1.7 x"), "application/pdf", nil},
		{"png", []byte("\x89PNG\r\n\x1a\nx"), "image/png", nil},
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg", nil},
		{"html disfrazado", []byte("<html><script>"), "", ErrFileType},
		{"ejecutable", []byte("MZ\x90\x00"), "", ErrFileType},
		{"vacío", nil, "", ErrFileSize},
		{"excede", []byte("%PDF-" + string(make([]byte, 20))), "", ErrFileSize},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mime, ext, err := DetectFile(c.content, 20)
			if c.err != nil {
				require.ErrorIs(t, err, c.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.mime, mime)
			assert.Equal(t, ext, Extension(mime))
		})
	}
	assert.Equal(t, "bin", Extension("text/html"))
}

func TestValidate(t *testing.T) {
	t.Parallel()
	ok := Document{OwnerType: OwnerClaim, OwnerID: uuid.New(), Title: "Denuncia"}
	require.NoError(t, ok.Validate())
	for name, mut := range map[string]func(*Document){
		"título corto": func(d *Document) { d.Title = " a " },
		"sin dueño":    func(d *Document) { d.OwnerID = uuid.Nil },
		"dueño raro":   func(d *Document) { d.OwnerType = "lead" },
	} {
		d := ok
		mut(&d)
		require.ErrorIs(t, d.Validate(), ErrInvalid, name)
	}
}

func TestDownloadableFailsClosed(t *testing.T) {
	t.Parallel()
	require.NoError(t, Version{Scan: ScanClean}.Downloadable())
	require.ErrorIs(t, Version{Scan: ScanPending}.Downloadable(), ErrNotScanned)
	require.ErrorIs(t, Version{Scan: ScanInfected}.Downloadable(), ErrInfected)
	require.ErrorIs(t, Version{Scan: "otro"}.Downloadable(), ErrNotScanned)
}

func TestRetentionUntil(t *testing.T) {
	t.Parallel()
	d := Document{CreatedOn: kernel.DateOf(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.UTC)}
	assert.Equal(t, "2027-01-01", d.RetentionUntil(365).String())
}

func TestIsSensitive(t *testing.T) {
	t.Parallel()
	assert.True(t, IsSensitive(OwnerClaim, false))
	assert.True(t, IsSensitive(OwnerParty, true))
	assert.False(t, IsSensitive(OwnerPolicy, false))
}

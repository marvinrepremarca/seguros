package audit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validEntry() Entry {
	return Entry{
		Action: "featuremgmt.flag.updated", ResourceType: "featuremgmt.flag", ResourceID: "module.claims",
		Outcome: OutcomeSuccess, Details: map[string]string{"previous": "false", "new": "true"},
	}
}

func TestEntryValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Entry)
		ok     bool
	}{
		{"válida", func(*Entry) {}, true},
		{"sin details", func(e *Entry) { e.Details = nil }, true},
		{"acción de dos segmentos", func(e *Entry) { e.Action = "identity.login" }, false},
		{"acción con mayúsculas", func(e *Entry) { e.Action = "Identity.session.login" }, false},
		{"acción con espacios", func(e *Entry) { e.Action = "identity.session.log in" }, false},
		{"resourceType vacío", func(e *Entry) { e.ResourceType = "" }, false},
		{"resourceId largo", func(e *Entry) { e.ResourceID = strings.Repeat("x", 201) }, false},
		{"outcome inválido", func(e *Entry) { e.Outcome = "ok" }, false},
		{"clave de detail inválida", func(e *Entry) { e.Details = map[string]string{"e-mail": "x"} }, false},
		{"valor de detail largo", func(e *Entry) { e.Details = map[string]string{"reason": strings.Repeat("x", 201)} }, false},
		{"demasiados details", func(e *Entry) {
			e.Details = map[string]string{}
			for i := range 21 {
				e.Details["k"+string(rune('a'+i))] = "v"
			}
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := validEntry()
			tt.mutate(&e)
			err := e.validate()
			if tt.ok {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrInvalidEntry)
		})
	}
}

func TestNonNil(t *testing.T) {
	t.Parallel()
	assert.NotNil(t, nonNil(nil))
	assert.Equal(t, map[string]string{"a": "b"}, nonNil(map[string]string{"a": "b"}))
}

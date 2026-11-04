package kernel_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

func TestNewTransferReason(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, code, note string
		ok               bool
	}{
		{"motivo sin nota", kernel.TransferEmployeeDeparture, "", true},
		{"motivo con nota", kernel.TransferClientRequest, "Pidió cambio", true},
		{"otro con nota", kernel.TransferOther, "Fusión de equipos", true},
		{"otro sin nota", kernel.TransferOther, "  ", false},
		{"código desconocido", "retiro", "", false},
		{"nota corta", kernel.TransferRoleChange, "x", false},
		{"nota larga", kernel.TransferRoleChange, strings.Repeat("ñ", 201), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, ok := kernel.NewTransferReason(c.code, c.note)
			assert.Equal(t, c.ok, ok)
		})
	}
}

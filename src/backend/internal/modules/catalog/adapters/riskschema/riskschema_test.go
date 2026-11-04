package riskschema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/catalog/adapters/riskschema"
	"github.com/marvinrepremarca/seguros/internal/modules/catalog/contracts"
)

func TestValidate(t *testing.T) {
	t.Parallel()
	r, err := riskschema.Load()
	require.NoError(t, err)
	cases := []struct {
		name, key, data string
		err             error
	}{
		{"auto válido", "auto", `{"plate":"ABC123","brand":"Mazda","model":"3","year":2024}`, nil},
		{"placa de moto válida", "auto", `{"plate":"ABC12D","brand":"Honda","model":"CB","year":2020}`, nil},
		{"placa inválida", "auto", `{"plate":"ab-123","brand":"Mazda","model":"3","year":2024}`, contracts.ErrInvalidRiskData},
		{"falta requerido", "auto", `{"plate":"ABC123"}`, contracts.ErrInvalidRiskData},
		{"tipo incorrecto", "auto", `{"plate":"ABC123","brand":"M","model":"3","year":"2024"}`, contracts.ErrInvalidRiskData},
		{"vida válida", "life", `{"age":40,"occupation":"Ingeniera"}`, nil},
		{"vida rechaza datos de salud", "life", `{"age":40,"occupation":"Ingeniera","healthDeclaration":"diabetes"}`, contracts.ErrInvalidRiskData},
		{"hogar válido", "home", `{"address":"Calle 1","propertyType":"Apartamento","buildingValue":"100000000.50"}`, nil},
		{"hogar con valor inválido", "home", `{"address":"Calle 1","propertyType":"Apto","buildingValue":"cien"}`, contracts.ErrInvalidRiskData},
		{"empresa válida", "business", `{"activity":"Comercio","employees":12}`, nil},
		{"general válido", "generic", `{"description":"Riesgo de cumplimiento"}`, nil},
		{"esquema desconocido", "nave", `{}`, contracts.ErrUnknownRiskSchema},
		{"json inválido", "generic", `{`, contracts.ErrInvalidRiskData},
		{"no es objeto", "generic", `[]`, contracts.ErrInvalidRiskData},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := r.Validate(c.key, []byte(c.data))
			if c.err != nil {
				require.ErrorIs(t, err, c.err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestAllSchemasAreListedWithLabels(t *testing.T) {
	t.Parallel()
	r, err := riskschema.Load()
	require.NoError(t, err)
	list := r.List()
	require.Len(t, list, 5)
	for _, s := range list {
		assert.NotEmpty(t, s.Label, s.Key)
		v, ok := r.Current(s.Key)
		assert.True(t, ok)
		assert.Equal(t, s.Version, v)
		assert.True(t, r.Known(s.Key))
	}
	_, ok := r.Current("nave")
	assert.False(t, ok)
}

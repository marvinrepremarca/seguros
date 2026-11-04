// Package contracttest valida respuestas HTTP contra los contratos OpenAPI (rules/testing.md §Contrato).
package contracttest

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/stretchr/testify/require"
)

// Checker valida respuestas contra uno o varios contratos.
type Checker struct {
	routers []routers.Router
}

// New construye el validador.
func New(t *testing.T, specs ...func() (*openapi3.T, error)) *Checker {
	t.Helper()
	c := &Checker{}
	for _, get := range specs {
		spec, err := get()
		require.NoError(t, err)
		spec.Servers = nil // el contrato no fija host: se valida solo la ruta
		router, err := gorillamux.NewRouter(spec)
		require.NoError(t, err)
		c.routers = append(c.routers, router)
	}
	return c
}

// Check falla la prueba si la respuesta no cumple el contrato de su operación: estado declarado y
// cuerpo válido. Los 5xx quedan fuera: son fallas que el contrato no enumera, y el cuerpo genérico ya
// lo prueba httpx.
func (c *Checker) Check(t *testing.T, req *http.Request, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code >= http.StatusInternalServerError {
		return
	}
	for _, router := range c.routers {
		route, params, err := router.FindRoute(req)
		if err != nil {
			continue
		}
		in := &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route}
		err = openapi3filter.ValidateResponse(req.Context(), &openapi3filter.ResponseValidationInput{
			RequestValidationInput: in, Status: rec.Code, Header: rec.Header(), Body: nopCloser{bytes.NewReader(rec.Body.Bytes())},
			Options: &openapi3filter.Options{IncludeResponseStatus: true},
		})
		require.NoError(t, err, "%s %s: respuesta %d fuera de contrato", req.Method, req.URL.Path, rec.Code)
		return
	}
}

type nopCloser struct{ *bytes.Reader }

func (nopCloser) Close() error { return nil }

package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
)

func TestClientIP(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.7:51000"
	req.Header.Set("X-Real-IP", "203.0.113.9")
	assert.Equal(t, "10.0.0.7", httpx.ClientIP(req, ""), "sin cabecera de confianza se ignora X-Real-IP")
	assert.Equal(t, "203.0.113.9", httpx.ClientIP(req, "X-Real-IP"))
	req.Header.Set("X-Real-IP", "no-es-ip")
	assert.Equal(t, "10.0.0.7", httpx.ClientIP(req, "X-Real-IP"), "valor inválido → RemoteAddr")
}

func TestRateLimitByIPOnlyLimitsPrefix(t *testing.T) {
	t.Parallel()
	h := httpx.RateLimitByIP("/bff/auth/", 2, time.Minute, "")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	call := func(path string) int {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		req.RemoteAddr = "198.51.100.1:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	assert.Equal(t, http.StatusNoContent, call("/bff/auth/login"))
	assert.Equal(t, http.StatusNoContent, call("/bff/auth/login"))
	assert.Equal(t, http.StatusTooManyRequests, call("/bff/auth/login"))
	for range 5 {
		assert.Equal(t, http.StatusNoContent, call("/bff/me"), "fuera del prefijo no hay límite")
	}
}

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRateLimitedRoutes(t *testing.T) {
	t.Parallel()
	req := func(method, target string) *http.Request {
		return httptest.NewRequestWithContext(t.Context(), method, target, nil)
	}
	assert.True(t, isAgentChange(req(http.MethodPost, "/api/v1/policies/portfolio-transfers")))
	assert.True(t, isAgentChange(req(http.MethodPost, "/api/v1/policies/policies/1/agent")))
	assert.True(t, isAgentChange(req(http.MethodPost, "/api/v1/parties/parties/1/responsible-agent")))
	assert.False(t, isAgentChange(req(http.MethodGet, "/api/v1/policies/policies/1")))
	assert.True(t, isNameSearch(req(http.MethodGet, "/api/v1/parties/parties?q=ana")))
	assert.False(t, isNameSearch(req(http.MethodGet, "/api/v1/parties/parties")))
	assert.True(t, isAgentList(req(http.MethodGet, "/api/v1/policies/agents")))
}

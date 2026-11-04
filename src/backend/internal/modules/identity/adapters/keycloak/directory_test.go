package keycloak_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/identity/adapters/keycloak"
	"github.com/marvinrepremarca/seguros/internal/platform/config"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

func newServer(t *testing.T, users func(first int) (int, any)) *keycloak.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /realms/r/protocol/openid-connect/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "token_type": "Bearer", "expires_in": 60}))
	})
	mux.HandleFunc("GET /admin/realms/r/roles/SalesAgent/users", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		first, err := strconv.Atoi(r.URL.Query().Get("first"))
		assert.NoError(t, err)
		status, body := users(first)
		w.WriteHeader(status)
		assert.NoError(t, json.NewEncoder(w).Encode(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := keycloak.New(config.Auth{Issuer: srv.URL + "/realms/r", DirectoryClientID: "d", DirectoryClientSecret: config.NewSecret("s")}, kernel.SystemClock{})
	require.NoError(t, err)
	return c
}

func TestActiveAgentSubjectsPaginatesAndSkipsDisabled(t *testing.T) {
	t.Parallel()
	c := newServer(t, func(first int) (int, any) {
		if first == 0 {
			page := make([]map[string]any, 0, 200)
			for i := range 200 {
				page = append(page, map[string]any{"id": fmt.Sprintf("u%d", i), "enabled": i != 0})
			}
			return http.StatusOK, page
		}
		return http.StatusOK, []map[string]any{{"id": "ultimo", "enabled": true}}
	})
	got, err := c.ActiveAgentSubjects(t.Context())
	require.NoError(t, err)
	assert.Len(t, got, 200)
	assert.True(t, got["ultimo"], "lee la segunda página")
	assert.False(t, got["u0"], "un usuario deshabilitado ya no es asesor activo")
}

func TestActiveAgentSubjectsFailsOnError(t *testing.T) {
	t.Parallel()
	c := newServer(t, func(int) (int, any) { return http.StatusForbidden, map[string]string{"error": "x"} })
	_, err := c.ActiveAgentSubjects(t.Context())
	require.ErrorContains(t, err, "estado 403")
}

func TestNewRejectsURLWithoutRealm(t *testing.T) {
	t.Parallel()
	_, err := keycloak.New(config.Auth{Issuer: "https://id.example.test/otro"}, kernel.SystemClock{})
	require.Error(t, err)
}

func TestActiveAgentSubjectsCachesBriefly(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /realms/r/protocol/openid-connect/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "token_type": "Bearer", "expires_in": 3600}))
	})
	mux.HandleFunc("GET /admin/realms/r/roles/SalesAgent/users", func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		assert.NoError(t, json.NewEncoder(w).Encode([]map[string]any{{"id": "a", "enabled": true}}))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	clock := kernel.NewFakeClock(time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC))
	c, err := keycloak.New(config.Auth{Issuer: srv.URL + "/realms/r", DirectoryClientID: "d", DirectoryClientSecret: config.NewSecret("s")}, clock)
	require.NoError(t, err)

	for range 3 {
		_, err := c.ActiveAgentSubjects(t.Context())
		require.NoError(t, err)
	}
	assert.Equal(t, int32(1), calls.Load())
	clock.Advance(31 * time.Second)
	_, err = c.ActiveAgentSubjects(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int32(2), calls.Load())
}

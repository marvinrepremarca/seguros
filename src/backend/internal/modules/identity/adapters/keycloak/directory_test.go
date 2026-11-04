package keycloak_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/identity/adapters/keycloak"
	"github.com/marvinrepremarca/seguros/internal/platform/config"
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
	c, err := keycloak.New(config.Auth{Issuer: srv.URL + "/realms/r", DirectoryClientID: "d", DirectoryClientSecret: config.NewSecret("s")})
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
	_, err := keycloak.New(config.Auth{Issuer: "https://id.example.test/otro"})
	require.Error(t, err)
}

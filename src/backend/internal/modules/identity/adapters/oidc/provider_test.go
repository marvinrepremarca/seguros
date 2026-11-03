package oidc_test

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/identity/adapters/oidc"
	"github.com/marvinrepremarca/seguros/internal/platform/config"
	"github.com/marvinrepremarca/seguros/test/oidctest"
)

const (
	clientID = "seguros-bff"
	public   = "https://app.example.test"
	verifier = "verifier-verifier-verifier-verifier-verifier-123"
)

// realNow: go-oidc valida exp/iat contra el reloj real del proceso.
func realNow() time.Time { return time.Now() } //nolint:forbidigo // ver comentario

func setup(t *testing.T) (*oidctest.Server, *oidc.Provider) {
	t.Helper()
	clientSecret := oidctest.RandomString()
	op, err := oidctest.Start(clientID, clientSecret, realNow)
	require.NoError(t, err)
	t.Cleanup(op.Close)
	p, err := oidc.New(context.Background(), config.Auth{
		Issuer: op.URL, ClientID: clientID, ClientSecret: config.NewSecret(clientSecret), PublicURL: public,
	}, oidc.NewHTTPClient())
	require.NoError(t, err)
	return op, p
}

func claims() oidctest.Claims {
	return oidctest.Claims{
		Subject: "kc-sub-1", Email: "ana@example.test", Name: "Ana", Roles: []string{"TenantAdmin"},
		Organizations: []string{"agencia-x"}, AMR: []string{"pwd", "otp"},
	}
}

func TestAuthCodeURLUsesPKCEAndScopes(t *testing.T) {
	t.Parallel()
	_, p := setup(t)
	u, err := url.Parse(p.AuthCodeURL("st", "nn", verifier))
	require.NoError(t, err)
	q := u.Query()
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.NotEmpty(t, q.Get("code_challenge"))
	assert.NotContains(t, u.RawQuery, "verifier-verifier", "el verificador nunca viaja en la URL")
	assert.Equal(t, "st", q.Get("state"))
	assert.Equal(t, "nn", q.Get("nonce"))
	assert.Equal(t, public+oidc.CallbackPath, q.Get("redirect_uri"))
	assert.Contains(t, q.Get("scope"), "openid")
	assert.Contains(t, q.Get("scope"), "organization")
}

func TestExchange(t *testing.T) {
	t.Parallel()
	with := func(f func(*oidctest.Claims)) func() oidctest.Claims {
		return func() oidctest.Claims { c := claims(); f(&c); return c }
	}
	tests := []struct {
		name     string
		claims   func() oidctest.Claims
		nonce    string
		verifier string
		wantErr  bool
		wantOrgs []string
	}{
		{"válido", claims, "n1", verifier, false, []string{"agencia-x"}},
		{"organizaciones como objeto", with(func(c *oidctest.Claims) {
			c.Organizations = map[string]any{"agencia-y": map[string]string{"id": "1"}, "agencia-x": map[string]string{"id": "2"}}
		}), "n1", verifier, false, []string{"agencia-x", "agencia-y"}},
		{"sin organización", with(func(c *oidctest.Claims) { c.Organizations = nil }), "n1", verifier, false, nil},
		{"nonce distinto", claims, "otro", verifier, true, nil},
		{"verificador PKCE distinto", claims, "n1", verifier + "x", true, nil},
		{"audiencia ajena", with(func(c *oidctest.Claims) { c.Overrides = map[string]any{"aud": "otro-cliente"} }), "n1", verifier, true, nil},
		{"emisor ajeno", with(func(c *oidctest.Claims) { c.Overrides = map[string]any{"iss": "https://evil.example"} }), "n1", verifier, true, nil},
		{"token vencido", with(func(c *oidctest.Claims) {
			c.Overrides = map[string]any{"exp": realNow().Add(-time.Hour).Unix()}
		}), "n1", verifier, true, nil},
		{"organización mal formada", with(func(c *oidctest.Claims) { c.Organizations = 42 }), "n1", verifier, true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			op, p := setup(t)
			cb, err := op.Authorize(p.AuthCodeURL("st", "n1", verifier), tt.claims())
			require.NoError(t, err)

			id, err := p.Exchange(context.Background(), cb.Get("code"), tt.verifier, tt.nonce)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "kc-sub-1", id.Subject)
			assert.Equal(t, "ana@example.test", id.Email)
			assert.Equal(t, "Ana", id.Name)
			assert.Equal(t, []string{"TenantAdmin"}, id.Roles)
			assert.Equal(t, []string{"pwd", "otp"}, id.AuthMethods)
			assert.Equal(t, tt.wantOrgs, id.Organizations)
			assert.NotEmpty(t, id.RawIDToken)
		})
	}
}

func TestCodeCanOnlyBeExchangedOnce(t *testing.T) {
	t.Parallel()
	op, p := setup(t)
	cb, err := op.Authorize(p.AuthCodeURL("st", "n1", verifier), claims())
	require.NoError(t, err)
	_, err = p.Exchange(context.Background(), cb.Get("code"), verifier, "n1")
	require.NoError(t, err)
	_, err = p.Exchange(context.Background(), cb.Get("code"), verifier, "n1")
	require.Error(t, err)
}

func TestLogoutURL(t *testing.T) {
	t.Parallel()
	op, p := setup(t)
	u, err := url.Parse(p.LogoutURL("raw-id-token"))
	require.NoError(t, err)
	assert.Equal(t, op.URL+"/logout", u.Scheme+"://"+u.Host+u.Path)
	assert.Equal(t, clientID, u.Query().Get("client_id"))
	assert.Equal(t, public+"/", u.Query().Get("post_logout_redirect_uri"))
	assert.Equal(t, "raw-id-token", u.Query().Get("id_token_hint"))
}

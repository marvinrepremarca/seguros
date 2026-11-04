// Package oidc es el cliente del proveedor de identidad (Keycloak): Authorization Code + PKCE
// (S256) con validación del ID token por go-oidc (firma RS256 fija, iss, aud, exp y nonce).
// No implementa criptografía ni validación de JWT propia (rules/security.md §Autenticación).
package oidc

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/marvinrepremarca/seguros/internal/modules/identity/app"
	"github.com/marvinrepremarca/seguros/internal/platform/config"
)

// CallbackPath es la ruta del BFF registrada como redirect URI en el proveedor.
const CallbackPath = "/bff/auth/callback"

// ErrInvalidToken indica una respuesta del proveedor que no supera la validación.
var ErrInvalidToken = errors.New("oidc: token inválido")

// Identity son los claims verificados del ID token.
type Identity = app.VerifiedIdentity

// Provider es el cliente OIDC del BFF.
type Provider struct {
	oauth         oauth2.Config
	verifier      *gooidc.IDTokenVerifier
	httpClient    *http.Client
	endSessionURL string
	clientID      string
	publicURL     string
}

// New descubre el proveedor. Con DiscoveryURL (solo desarrollo) el backend lo consulta por una URL
// interna, pero sigue exigiendo que los tokens traigan iss = Issuer.
func New(ctx context.Context, cfg config.Auth, httpClient *http.Client) (*Provider, error) {
	ctx = gooidc.ClientContext(ctx, httpClient)
	discovery := cfg.Issuer
	if cfg.DiscoveryURL != "" {
		ctx = gooidc.InsecureIssuerURLContext(ctx, cfg.Issuer)
		discovery = cfg.DiscoveryURL
	}
	provider, err := gooidc.NewProvider(ctx, discovery)
	if err != nil {
		return nil, fmt.Errorf("oidc: descubriendo proveedor: %w", err)
	}
	var meta struct {
		EndSession string `json:"end_session_endpoint"`
	}
	if err := provider.Claims(&meta); err != nil {
		return nil, fmt.Errorf("oidc: leyendo metadatos: %w", err)
	}
	return &Provider{
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret.Reveal(),
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.PublicURL + CallbackPath,
			Scopes:       []string{gooidc.ScopeOpenID, "profile", "email", "organization"},
		},
		verifier: provider.Verifier(&gooidc.Config{
			ClientID:             cfg.ClientID,
			SupportedSigningAlgs: []string{gooidc.RS256},
		}),
		httpClient:    httpClient,
		endSessionURL: meta.EndSession,
		clientID:      cfg.ClientID,
		publicURL:     cfg.PublicURL,
	}, nil
}

// NewHTTPClient crea el cliente HTTP hacia el proveedor (timeouts acotados).
func NewHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// AuthCodeURL construye la URL de autorización con state, nonce y PKCE S256.
func (p *Provider) AuthCodeURL(state, nonce, verifier string) string {
	return p.oauth.AuthCodeURL(state, gooidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
}

// Exchange canjea el código (con el verificador PKCE) y valida el ID token y su nonce.
func (p *Provider) Exchange(ctx context.Context, code, verifier, nonce string) (Identity, error) {
	ctx = gooidc.ClientContext(ctx, p.httpClient)
	tok, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Identity{}, fmt.Errorf("oidc: canjeando código: %w", err)
	}
	if tok.RefreshToken == "" {
		return Identity{}, fmt.Errorf("%w: sin refresh_token", ErrInvalidToken)
	}
	id, nonceClaim, err := p.identityOf(ctx, tok)
	if err != nil {
		return Identity{}, err
	}
	if subtle.ConstantTimeCompare([]byte(nonceClaim), []byte(nonce)) != 1 || nonce == "" {
		return Identity{}, fmt.Errorf("%w: nonce", ErrInvalidToken)
	}
	return id, nil
}

// Refresh confirma la sesión con el proveedor y devuelve la identidad vigente. Una sesión revocada,
// un usuario deshabilitado o un refresh token vencido devuelven ErrSessionRevoked; cualquier otro
// error es transitorio.
func (p *Provider) Refresh(ctx context.Context, refreshToken string) (Identity, error) {
	ctx = gooidc.ClientContext(ctx, p.httpClient)
	tok, err := p.oauth.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken}).Token()
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && re.ErrorCode == "invalid_grant" {
			return Identity{}, app.ErrSessionRevoked
		}
		return Identity{}, fmt.Errorf("oidc: renovando sesión: %w", err)
	}
	id, _, err := p.identityOf(ctx, tok)
	return id, err
}

// identityOf valida el ID token de una respuesta del proveedor y extrae sus claims.
func (p *Provider) identityOf(ctx context.Context, tok *oauth2.Token) (Identity, string, error) {
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return Identity{}, "", fmt.Errorf("%w: sin id_token", ErrInvalidToken)
	}
	idt, err := p.verifier.Verify(ctx, raw)
	if err != nil {
		return Identity{}, "", fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	var c struct {
		Email        string          `json:"email"`
		Name         string          `json:"name"`
		Username     string          `json:"preferred_username"`
		Roles        []string        `json:"roles"`
		Organization json.RawMessage `json:"organization"`
		AMR          []string        `json:"amr"`
	}
	if err := idt.Claims(&c); err != nil {
		return Identity{}, "", fmt.Errorf("%w: claims: %w", ErrInvalidToken, err)
	}
	orgs, err := parseOrganizations(c.Organization)
	if err != nil {
		return Identity{}, "", err
	}
	name := c.Name
	if name == "" {
		name = c.Username
	}
	return Identity{
		Subject: idt.Subject, Email: c.Email, Name: name, Roles: c.Roles,
		Organizations: orgs, AuthMethods: c.AMR, RefreshToken: tok.RefreshToken,
	}, idt.Nonce, nil
}

// parseOrganizations acepta los dos formatos del mapper de Keycloak: lista de alias
// (["agencia-x"]) u objeto indexado por alias ({"agencia-x": {"id": "..."}}).
func parseOrganizations(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("%w: claim organization", ErrInvalidToken)
	}
	out := make([]string, 0, len(obj))
	for k := range obj {
		out = append(out, k)
	}
	slices.Sort(out)
	return out, nil
}

// LogoutURL devuelve la URL de cierre de sesión del proveedor (RP-initiated logout) o la raíz de
// la SPA si el proveedor no la publica.
func (p *Provider) LogoutURL(idTokenHint string) string {
	if p.endSessionURL == "" {
		return p.publicURL + "/"
	}
	q := url.Values{}
	q.Set("client_id", p.clientID)
	q.Set("post_logout_redirect_uri", p.publicURL+"/")
	if idTokenHint != "" {
		q.Set("id_token_hint", idTokenHint)
	}
	return p.endSessionURL + "?" + q.Encode()
}

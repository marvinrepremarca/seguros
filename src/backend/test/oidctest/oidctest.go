// Package oidctest es un proveedor OpenID Connect mínimo para pruebas: discovery, JWKS y token
// endpoint (Authorization Code + PKCE S256), con ID tokens RS256 firmados por una llave efímera.
// Verifica client_secret, redirect_uri y code_verifier como lo haría Keycloak.
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// Claims son los datos del usuario que el proveedor pone en el ID token.
type Claims struct {
	Subject       string
	Email         string
	Name          string
	Roles         []string
	Organizations any // []string o map[string]any (formatos de Keycloak)
	AMR           []string
	// Overrides permite alterar claims estándar para probar rechazos (iss, aud, exp, nonce).
	Overrides map[string]any
}

type grant struct {
	claims      Claims
	nonce       string
	challenge   string
	redirectURI string
}

// Server es el proveedor falso.
type Server struct {
	URL          string
	ClientID     string
	ClientSecret string

	srv   *httptest.Server
	key   *rsa.PrivateKey
	now   func() time.Time
	mu    sync.Mutex
	codes map[string]grant
}

// Start levanta el proveedor. now fija el reloj de emisión de tokens.
func Start(clientID, clientSecret string, now func() time.Time) (*Server, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("oidctest: generando llave: %w", err)
	}
	s := &Server{ClientID: clientID, ClientSecret: clientSecret, key: key, now: now, codes: map[string]grant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", s.discovery)
	mux.HandleFunc("GET /jwks", s.jwks)
	mux.HandleFunc("POST /token", s.token)
	s.srv = httptest.NewServer(mux)
	s.URL = s.srv.URL
	return s, nil
}

// Close detiene el proveedor.
func (s *Server) Close() { s.srv.Close() }

// Authorize simula que el usuario se autentica en el proveedor tras seguir authURL (la URL de
// autorización que generó el BFF) y devuelve la query del callback (code y state).
func (s *Server) Authorize(authURL string, c Claims) (url.Values, error) {
	u, err := url.Parse(authURL)
	if err != nil {
		return nil, fmt.Errorf("oidctest: URL de autorización: %w", err)
	}
	q := u.Query()
	if q.Get("response_type") != "code" || q.Get("client_id") != s.ClientID || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		return nil, fmt.Errorf("oidctest: solicitud de autorización inválida: %s", u.RawQuery)
	}
	code := RandomString()
	s.mu.Lock()
	s.codes[code] = grant{claims: c, nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirectURI: q.Get("redirect_uri")}
	s.mu.Unlock()
	return url.Values{"code": {code}, "state": {q.Get("state")}}, nil
}

func (s *Server) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                s.URL,
		"authorization_endpoint":                s.URL + "/auth",
		"token_endpoint":                        s.URL + "/token",
		"jwks_uri":                              s.URL + "/jwks",
		"end_session_endpoint":                  s.URL + "/logout",
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
	})
}

func (s *Server) jwks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &s.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if id != s.ClientID || subtle.ConstantTimeCompare([]byte(secret), []byte(s.ClientSecret)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
		return
	}
	s.mu.Lock()
	g, found := s.codes[r.PostForm.Get("code")]
	delete(s.codes, r.PostForm.Get("code")) // un código se canjea una sola vez
	s.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !found || r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("redirect_uri") != g.redirectURI ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	idToken, err := s.sign(g)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": RandomString(), "token_type": "Bearer", "expires_in": 300, "id_token": idToken,
	})
}

func (s *Server) sign(g grant) (string, error) {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: s.key}, (&jose.SignerOptions{}).WithHeader("kid", "k1").WithType("JWT"))
	if err != nil {
		return "", fmt.Errorf("oidctest: firmante: %w", err)
	}
	now := s.now()
	claims := map[string]any{
		"iss": s.URL, "sub": g.claims.Subject, "aud": s.ClientID, "nonce": g.nonce,
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
		"email": g.claims.Email, "name": g.claims.Name, "roles": g.claims.Roles,
	}
	if g.claims.Organizations != nil {
		claims["organization"] = g.claims.Organizations
	}
	if g.claims.AMR != nil {
		claims["amr"] = g.claims.AMR
	}
	for k, v := range g.claims.Overrides {
		claims[k] = v
	}
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		return "", fmt.Errorf("oidctest: firmando: %w", err)
	}
	return raw, nil
}

// RandomString devuelve 16 bytes aleatorios en base64url (códigos, secretos de prueba).
func RandomString() string {
	return rand.Text()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "encoding", http.StatusInternalServerError)
	}
}

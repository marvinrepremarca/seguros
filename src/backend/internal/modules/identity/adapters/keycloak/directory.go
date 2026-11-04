// Package keycloak consulta la API de administración con una cuenta de servicio de solo lectura.
package keycloak

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/config"
)

const (
	pageSize = 200
	maxPages = 50
)

// Client implementa app.ActiveAgents.
type Client struct {
	http      *http.Client
	adminBase string
}

// New deriva las URLs del realm y crea el cliente con credenciales de cliente.
func New(cfg config.Auth) (*Client, error) {
	realmURL := cfg.Issuer
	if cfg.DiscoveryURL != "" {
		realmURL = cfg.DiscoveryURL
	}
	base, realm, ok := strings.Cut(strings.TrimRight(realmURL, "/"), "/realms/")
	if !ok || realm == "" {
		return nil, errors.New("keycloak: la URL del proveedor no tiene /realms/<realm>")
	}
	credentials := clientcredentials.Config{
		ClientID: cfg.DirectoryClientID, ClientSecret: cfg.DirectoryClientSecret.Reveal(),
		TokenURL: base + "/realms/" + realm + "/protocol/openid-connect/token",
	}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Timeout: 10 * time.Second})
	httpClient := credentials.Client(ctx)
	httpClient.Timeout = 10 * time.Second
	return &Client{http: httpClient, adminBase: base + "/admin/realms/" + url.PathEscape(realm)}, nil
}

// ActiveAgentSubjects implementa app.ActiveAgents: usuarios habilitados con el rol de asesor.
func (c *Client) ActiveAgentSubjects(ctx context.Context) (map[string]bool, error) {
	out := map[string]bool{}
	for page := range maxPages {
		users, err := c.page(ctx, page*pageSize)
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			if u.Enabled {
				out[u.ID] = true
			}
		}
		if len(users) < pageSize {
			return out, nil
		}
	}
	return nil, errors.New("keycloak: demasiados usuarios con el rol de asesor")
}

type user struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

func (c *Client) page(ctx context.Context, first int) ([]user, error) {
	target := c.adminBase + "/roles/" + url.PathEscape(string(authz.RoleSalesAgent)) + "/users?first=" + strconv.Itoa(first) + "&max=" + strconv.Itoa(pageSize)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("keycloak: creando petición: %w", err)
	}
	resp, err := c.http.Do(req) // #nosec G704 -- destino fijo: la URL del proveedor viene de la configuración
	if err != nil {
		return nil, fmt.Errorf("keycloak: consultando asesores: %w", err)
	}
	defer func() {
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			return
		}
		if err := resp.Body.Close(); err != nil {
			return
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("keycloak: consultando asesores: estado %d", resp.StatusCode)
	}
	var users []user
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return nil, fmt.Errorf("keycloak: respuesta inválida: %w", err)
	}
	return users, nil
}

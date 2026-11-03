package httpx

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
)

// spaNoncePlaceholder es el valor de html.cspNonce en vite.config.ts: Vite lo escribe en el
// atributo nonce de cada <script>, <style> y <link> de index.html al compilar.
const spaNoncePlaceholder = "__CSP_NONCE__"

const spaNonceBytes = 16

// Prefijos que nunca devuelven la SPA: un error de la API sigue siendo problem+json.
//
//nolint:gochecknoglobals // lista inmutable
var spaReservedPrefixes = []string{"/api/", "/bff/", "/health/"}

// SPAHandler sirve la SPA compilada (src/frontend/dist) fuera de Vite, con CSP por petición.
// Solo responde GET y HEAD; las rutas del cliente reciben index.html y los archivos estáticos
// inexistentes reciben 404 (no index.html, para no confundir un chunk perdido con una página).
type SPAHandler struct {
	files  fs.FS
	index  string
	logger *slog.Logger
}

// NewSPAHandler valida el build al arrancar: sin index.html con el marcador del nonce la CSP
// bloquearía toda la aplicación, así que el proceso falla en lugar de servir una SPA rota.
func NewSPAHandler(files fs.FS, logger *slog.Logger) (*SPAHandler, error) {
	raw, err := fs.ReadFile(files, "index.html")
	if err != nil {
		return nil, fmt.Errorf("httpx: leyendo index.html de la SPA: %w", err)
	}
	index := string(raw)
	if !strings.Contains(index, `nonce="`+spaNoncePlaceholder+`"`) {
		return nil, errors.New("httpx: index.html sin marcador de nonce (revisa html.cspNonce en vite.config.ts)")
	}
	return &SPAHandler{files: files, index: index, logger: logger}, nil
}

// ServeHTTP implementa http.Handler.
func (s *SPAHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	notFound := func() {
		WriteProblem(w, r, http.StatusNotFound, CodeNotFound, titleForStatus(http.StatusNotFound))
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		notFound()
		return
	}
	for _, p := range spaReservedPrefixes {
		if strings.HasPrefix(r.URL.Path, p) {
			notFound()
			return
		}
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	switch {
	case name == "" || name == "index.html":
		s.serveIndex(w, r)
	case hasDotSegment(name):
		notFound()
	case s.isFile(name):
		s.serveAsset(w, r, name)
	case strings.HasPrefix(name, "assets/") || path.Ext(name) != "":
		notFound()
	default:
		s.serveIndex(w, r)
	}
}

func (s *SPAHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	nonce, err := newSPANonce()
	if err != nil {
		WriteError(w, r, s.logger, err)
		return
	}
	h := w.Header()
	h.Set("Content-Security-Policy", spaCSP(nonce))
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := w.Write([]byte(strings.ReplaceAll(s.index, spaNoncePlaceholder, nonce))); err != nil {
		s.logger.DebugContext(r.Context(), "escribiendo index.html", "error", err)
	}
}

func (s *SPAHandler) serveAsset(w http.ResponseWriter, r *http.Request, name string) {
	cache := "no-cache"
	if strings.HasPrefix(name, "assets/") {
		cache = "public, max-age=31536000, immutable" // nombres con hash de contenido (Vite)
	}
	w.Header().Set("Cache-Control", cache)
	http.ServeFileFS(w, r, s.files, name)
}

func (s *SPAHandler) isFile(name string) bool {
	if !fs.ValidPath(name) {
		return false
	}
	info, err := fs.Stat(s.files, name)
	return err == nil && info.Mode().IsRegular()
}

func hasDotSegment(name string) bool {
	for segment := range strings.SplitSeq(name, "/") {
		if strings.HasPrefix(segment, ".") {
			return true
		}
	}
	return false
}

func newSPANonce() (string, error) {
	b := make([]byte, spaNonceBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("httpx: generando nonce: %w", err)
	}
	return base64.RawStdEncoding.EncodeToString(b), nil
}

// spaCSP arma la política de la SPA (ARCHITECTURE.md §2). 'strict-dynamic' permite los chunks que
// importa el script con nonce; Trusted Types bloquea los sumideros de DOM XSS. Cualquier librería
// que necesite una política propia (ej. DOMPurify) debe declararla aquí con su nombre.
func spaCSP(nonce string) string {
	return strings.Join([]string{
		"default-src 'none'",
		"script-src 'nonce-" + nonce + "' 'strict-dynamic'",
		"style-src 'self' 'nonce-" + nonce + "'",
		"img-src 'self' data:",
		"font-src 'self'",
		"connect-src 'self'",
		"manifest-src 'self'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
		"require-trusted-types-for 'script'",
		"trusted-types 'none'",
	}, "; ")
}

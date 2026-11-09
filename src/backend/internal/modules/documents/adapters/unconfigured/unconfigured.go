// Package unconfigured reemplaza almacenamiento y antivirus cuando el entorno no los configuró: la API
// arranca y las operaciones de archivos responden 503.
package unconfigured

import (
	"context"
	"io"
	"time"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// ErrUnavailable indica que el almacenamiento de documentos no está configurado.
var ErrUnavailable = kernel.NewError(kernel.KindUnavailable, "documents.storage.unconfigured", "el almacenamiento de documentos no está configurado")

// Storage implementa app.Storage y app.Scanner sin hacer nada.
type Storage struct{}

// Put implementa app.Storage.
func (Storage) Put(context.Context, string, []byte, string) error { return ErrUnavailable }

// Open implementa app.Storage.
func (Storage) Open(context.Context, string) (io.ReadCloser, error) { return nil, ErrUnavailable }

// Remove implementa app.Storage.
func (Storage) Remove(context.Context, string) error { return ErrUnavailable }

// PresignGet implementa app.Storage.
func (Storage) PresignGet(context.Context, string, string, string, time.Duration) (string, error) {
	return "", ErrUnavailable
}

// Scan implementa app.Scanner.
func (Storage) Scan(context.Context, io.Reader) (bool, error) { return false, ErrUnavailable }

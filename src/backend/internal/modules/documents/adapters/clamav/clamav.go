// Package clamav revisa archivos con clamd mediante el protocolo INSTREAM sobre TCP.
package clamav

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

const (
	chunkSize = 64 * 1024
	timeout   = 2 * time.Minute
)

// Scanner habla con un clamd.
type Scanner struct{ Addr string }

// Scan implementa app.Scanner. Cualquier respuesta distinta de OK o FOUND es un error: el archivo
// sigue bloqueado.
func (s Scanner) Scan(ctx context.Context, content io.Reader) (infected bool, err error) {
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", s.Addr)
	if err != nil {
		return false, fmt.Errorf("clamav: conectando: %w", err)
	}
	defer func() { err = errors.Join(err, conn.Close()) }()
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(timeout) //nolint:forbidigo // plazo de E/S del socket, no regla de negocio
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return false, fmt.Errorf("clamav: plazo: %w", err)
	}
	if _, err := conn.Write([]byte("zINSTREAM\x00")); err != nil {
		return false, fmt.Errorf("clamav: comando: %w", err)
	}
	buf := make([]byte, chunkSize)
	for {
		n, readErr := content.Read(buf)
		if n > 0 {
			var size [4]byte
			binary.BigEndian.PutUint32(size[:], uint32(n)) //nolint:gosec // n ≤ chunkSize
			if _, err := conn.Write(size[:]); err != nil {
				return false, fmt.Errorf("clamav: enviando: %w", err)
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				return false, fmt.Errorf("clamav: enviando: %w", err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return false, fmt.Errorf("clamav: leyendo archivo: %w", readErr)
		}
	}
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return false, fmt.Errorf("clamav: cerrando envío: %w", err)
	}
	reply, err := io.ReadAll(io.LimitReader(conn, 1024))
	if err != nil {
		return false, fmt.Errorf("clamav: respuesta: %w", err)
	}
	text := strings.TrimRight(string(reply), "\x00\n ")
	switch {
	case strings.HasSuffix(text, " OK"):
		return false, nil
	case strings.HasSuffix(text, " FOUND"):
		return true, nil
	default:
		return false, errors.New("clamav: respuesta inesperada")
	}
}

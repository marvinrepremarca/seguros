package smtp

import (
	"bufio"
	"context"
	"net"
	"net/textproto"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/modules/notifications/domain"
)

// fakeServer habla lo mínimo de SMTP, sin STARTTLS, y entrega lo recibido por el canal.
func fakeServer(t *testing.T) (string, <-chan string) {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, ln.Close()) })
	out := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck // fin de la prueba
		tp := textproto.NewConn(conn)
		_ = tp.PrintfLine("220 ok") //nolint:errcheck // la prueba valida el resultado del cliente
		r := bufio.NewReader(conn)
		var data strings.Builder
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			switch {
			case inData && line == ".\r\n":
				inData = false
				out <- data.String()
				_ = tp.PrintfLine("250 ok") //nolint:errcheck // idem
			case inData:
				data.WriteString(line)
			case strings.HasPrefix(line, "DATA"):
				inData = true
				_ = tp.PrintfLine("354 go") //nolint:errcheck // idem
			case strings.HasPrefix(line, "QUIT"):
				_ = tp.PrintfLine("221 bye") //nolint:errcheck // idem
				return
			default:
				_ = tp.PrintfLine("250 ok") //nolint:errcheck // idem
			}
		}
	}()
	return ln.Addr().String(), out
}

func TestSend(t *testing.T) {
	t.Parallel()
	addr, out := fakeServer(t)
	s := Sender{C: Config{Addr: addr, From: "avisos@seguros.test"}}
	require.NoError(t, s.Send(context.Background(), "ana@example.com", domain.Email{Subject: "Su póliza", Body: "Hola.\nFin."}))
	msg := <-out
	assert.Contains(t, msg, "To: <ana@example.com>")
	assert.Contains(t, msg, "=?utf-8?q?Su_p=C3=B3liza?=")
	assert.Contains(t, msg, "Hola.\r\nFin.")
}

func TestRequiresTLSWhenConfigured(t *testing.T) {
	t.Parallel()
	addr, _ := fakeServer(t)
	s := Sender{C: Config{Addr: addr, From: "avisos@seguros.test", RequireTLS: true}}
	err := s.Send(context.Background(), "ana@example.com", domain.Email{Subject: "x", Body: "y"})
	require.ErrorContains(t, err, "STARTTLS")
}

func TestRejectsHeaderInjection(t *testing.T) {
	t.Parallel()
	s := Sender{C: Config{Addr: "127.0.0.1:1", From: "avisos@seguros.test"}}
	require.Error(t, s.Send(context.Background(), "ana@example.com\r\nBcc: x@y.z", domain.Email{Subject: "x"}))
	require.Error(t, s.Send(context.Background(), "ana@example.com", domain.Email{Subject: "x\r\nBcc: y"}))
}

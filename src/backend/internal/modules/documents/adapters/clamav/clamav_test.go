package clamav

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClamd lee el protocolo INSTREAM real y responde lo indicado.
func fakeClamd(t *testing.T, reply string, got *bytes.Buffer) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, ln.Close()) })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck // fin de la prueba
		cmd := make([]byte, len("zINSTREAM\x00"))
		if _, err := io.ReadFull(conn, cmd); err != nil {
			return
		}
		for {
			var size [4]byte
			if _, err := io.ReadFull(conn, size[:]); err != nil {
				return
			}
			n := binary.BigEndian.Uint32(size[:])
			if n == 0 {
				break
			}
			if _, err := io.CopyN(got, conn, int64(n)); err != nil {
				return
			}
		}
		_, _ = conn.Write([]byte(reply)) //nolint:errcheck // la prueba valida lo que recibe el cliente
	}()
	return ln.Addr().String()
}

func TestScan(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, reply string
		infected    bool
		fails       bool
	}{
		{"limpio", "stream: OK\x00", false, false},
		{"virus", "stream: Eicar-Signature FOUND\x00", true, false},
		{"error de clamd", "INSTREAM size limit exceeded. ERROR\x00", false, true},
		{"respuesta vacía", "", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var got bytes.Buffer
			content := strings.Repeat("x", chunkSize+10)
			infected, err := Scanner{Addr: fakeClamd(t, c.reply, &got)}.Scan(context.Background(), strings.NewReader(content))
			if c.fails {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.infected, infected)
			assert.Equal(t, content, got.String())
		})
	}
}

func TestScanWithoutServerFails(t *testing.T) {
	t.Parallel()
	_, err := Scanner{Addr: "127.0.0.1:1"}.Scan(context.Background(), strings.NewReader("x"))
	require.Error(t, err)
}

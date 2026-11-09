// Package smtp entrega correos por un servidor SMTP configurable.
package smtp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	netsmtp "net/smtp"
	"strings"
	"time"

	"github.com/marvinrepremarca/seguros/internal/modules/notifications/domain"
)

const timeout = 30 * time.Second

// Config son los datos del servidor.
type Config struct {
	Addr, From, User, Password string
	// RequireTLS exige STARTTLS.
	RequireTLS bool
}

// Sender implementa app.Sender.
type Sender struct{ C Config }

// Send implementa app.Sender. net/smtp se niega a enviar credenciales PLAIN sin TLS salvo a localhost.
func (s Sender) Send(ctx context.Context, to string, e domain.Email) (err error) {
	rcpt, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("smtp: destinatario: %w", err)
	}
	from, err := mail.ParseAddress(s.C.From)
	if err != nil {
		return fmt.Errorf("smtp: remitente: %w", err)
	}
	if strings.ContainsAny(e.Subject, "\r\n") {
		return errors.New("smtp: salto de línea en el asunto")
	}
	host, _, err := net.SplitHostPort(s.C.Addr)
	if err != nil {
		return fmt.Errorf("smtp: dirección: %w", err)
	}
	conn, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", s.C.Addr)
	if err != nil {
		return fmt.Errorf("smtp: conectando: %w", err)
	}
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil { //nolint:forbidigo // plazo de E/S del socket
		return errors.Join(fmt.Errorf("smtp: plazo: %w", err), conn.Close())
	}
	c, err := netsmtp.NewClient(conn, host)
	if err != nil {
		return errors.Join(fmt.Errorf("smtp: cliente: %w", err), conn.Close())
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, c.Close())
		}
	}()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("smtp: tls: %w", err)
		}
	} else if s.C.RequireTLS {
		return errors.New("smtp: el servidor no ofrece STARTTLS")
	}
	if s.C.User != "" {
		if err := c.Auth(netsmtp.PlainAuth("", s.C.User, s.C.Password, host)); err != nil {
			return fmt.Errorf("smtp: autenticación: %w", err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("smtp: remitente: %w", err)
	}
	if err := c.Rcpt(rcpt.Address); err != nil {
		return fmt.Errorf("smtp: destinatario: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp: datos: %w", err)
	}
	var b strings.Builder
	b.WriteString("From: " + from.String() + "\r\n")
	b.WriteString("To: " + rcpt.String() + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", e.Subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(strings.ReplaceAll(e.Body, "\n", "\r\n"))
	if _, err := w.Write([]byte(b.String())); err != nil {
		return fmt.Errorf("smtp: escribiendo: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: cerrando mensaje: %w", err)
	}
	if err := c.Quit(); err != nil {
		return fmt.Errorf("smtp: cierre: %w", err)
	}
	return nil
}

package core

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"time"
)

// Mailer verschickt eine Text-Mail. Im CGI-Modus synchron innerhalb der Anfrage.
type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}

// ErrMailNotConfigured: SMTP-Host oder Absender fehlen in den Settings.
var ErrMailNotConfigured = errors.New("mail: SMTP nicht eingerichtet")

const smtpTimeout = 10 * time.Second

// SMTPMailer liest die Zugangsdaten bei jedem Versand aus den Settings
// (mail.smtp.*) und verlangt STARTTLS – ohne TLS wird nichts gesendet,
// schon gar kein Passwort.
type SMTPMailer struct {
	Settings  Settings
	TLSConfig *tls.Config // nil = Systemzertifikate; Tests setzen RootCAs
}

func (m SMTPMailer) Send(ctx context.Context, to, subject, body string) error {
	cfg := map[string]string{}
	for _, k := range []string{KeySMTPHost, KeySMTPPort, KeySMTPUser, KeySMTPPassword, KeyMailFrom} {
		v, err := m.Settings.Get(ctx, k)
		if err != nil {
			return err
		}
		cfg[k] = v
	}
	host, from := cfg[KeySMTPHost], cfg[KeyMailFrom]
	if host == "" || from == "" {
		return ErrMailNotConfigured
	}
	port := cfg[KeySMTPPort]
	if port == "" {
		port = "587"
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("mail: ungültiger Port")
	}
	fromAddr, err := mail.ParseAddress(from)
	if err != nil {
		return fmt.Errorf("mail: ungültiger Absender")
	}
	toAddr, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("mail: ungültiger Empfänger")
	}
	msg, err := buildMessage(fromAddr, toAddr, subject, body)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(smtpTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	dialer := net.Dialer{Deadline: deadline}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return fmt.Errorf("mail: verbinden: %w", err)
	}
	_ = conn.SetDeadline(deadline)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("mail: %w", err)
	}
	defer c.Close()

	if ok, _ := c.Extension("STARTTLS"); !ok {
		return errors.New("mail: Server bietet kein STARTTLS an")
	}
	tcfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if m.TLSConfig != nil {
		tcfg = m.TLSConfig.Clone()
		if tcfg.ServerName == "" {
			tcfg.ServerName = host
		}
	}
	if err := c.StartTLS(tcfg); err != nil {
		return fmt.Errorf("mail: STARTTLS: %w", err)
	}
	if user := cfg[KeySMTPUser]; user != "" {
		if err := c.Auth(smtp.PlainAuth("", user, cfg[KeySMTPPassword], host)); err != nil {
			return errors.New("mail: Anmeldung am SMTP-Server fehlgeschlagen") // Serverantwort kann Zugangsdaten spiegeln
		}
	}
	if err := c.Mail(fromAddr.Address); err != nil {
		return fmt.Errorf("mail: MAIL FROM: %w", err)
	}
	if err := c.Rcpt(toAddr.Address); err != nil {
		return fmt.Errorf("mail: RCPT TO: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("mail: DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("mail: DATA: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail: DATA: %w", err)
	}
	return c.Quit()
}

// buildMessage baut eine RFC-5322-Nachricht. Adressen kommen aus
// mail.ParseAddress (keine Header-Injektion), der Betreff wird kodiert, der
// Text quoted-printable (kein 8BITMIME nötig).
func buildMessage(from, to *mail.Address, subject, body string) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("From: " + from.String() + "\r\n")
	b.WriteString("To: " + to.String() + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	qp := quotedprintable.NewWriter(&b)
	if _, err := qp.Write([]byte(body)); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

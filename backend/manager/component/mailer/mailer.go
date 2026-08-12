// Package mailer sends transactional email (e.g. the signup verification
// email) over SMTP using the workspace SMTPSetting. The config is re-read from
// the setting table on every send, so admin changes take effect immediately
// without restart or invalidation wiring.
package mailer

import (
	"context"
	"crypto/tls"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/manager/store"
)

// Sender delivers email through the workspace SMTP configuration.
type Sender struct {
	store *store.Store
}

// NewSender returns a Sender backed by the workspace setting store.
func NewSender(s *store.Store) *Sender {
	return &Sender{store: s}
}

// Configured reports whether an SMTP server has been set up (host non-empty).
func (s *Sender) Configured(ctx context.Context) (bool, error) {
	cfg, err := s.store.GetSMTPSetting(ctx)
	if err != nil {
		return false, err
	}
	return cfg.GetHost() != "", nil
}

// Send delivers a multipart/alternative (plain text + HTML) email to a single
// recipient. from/to/subject must not contain header-injection newlines; Send
// rejects them.
func (s *Sender) Send(ctx context.Context, to, subject, textBody, htmlBody string) error {
	cfg, err := s.store.GetSMTPSetting(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to read SMTP setting")
	}
	if cfg.GetHost() == "" {
		return errors.New("SMTP is not configured")
	}
	if strings.ContainsAny(to, "\r\n") || strings.ContainsAny(cfg.GetFrom(), "\r\n") || strings.ContainsAny(subject, "\r\n") {
		return errors.New("email headers must not contain newlines")
	}

	port := int(cfg.GetPort())
	if port == 0 {
		port = 587
	}
	addr := net.JoinHostPort(cfg.GetHost(), strconv.Itoa(port))

	var conn net.Conn
	dial := net.Dialer{Timeout: 10 * time.Second}
	if cfg.GetUseTls() && port == 465 {
		conn, err = tls.DialWithDialer(&dial, "tcp", addr, &tls.Config{ServerName: cfg.GetHost(), MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dial.Dial("tcp", addr)
	}
	if err != nil {
		return errors.Wrapf(err, "failed to connect to SMTP server %s", addr)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	client, err := smtp.NewClient(conn, cfg.GetHost())
	if err != nil {
		return errors.Wrap(err, "failed to start SMTP session")
	}
	defer client.Close()

	if cfg.GetUseTls() && port != 465 {
		if err := client.StartTLS(&tls.Config{ServerName: cfg.GetHost(), MinVersion: tls.VersionTLS12}); err != nil {
			return errors.Wrap(err, "failed to start SMTP TLS")
		}
	}
	if cfg.GetUsername() != "" {
		auth := smtp.PlainAuth("", cfg.GetUsername(), cfg.GetPassword(), cfg.GetHost())
		if err := client.Auth(auth); err != nil {
			return errors.Wrap(err, "failed to authenticate with SMTP server")
		}
	}
	if err := client.Mail(cfg.GetFrom()); err != nil {
		return errors.Wrap(err, "failed to set SMTP sender")
	}
	if err := client.Rcpt(to); err != nil {
		return errors.Wrap(err, "failed to set SMTP recipient")
	}
	w, err := client.Data()
	if err != nil {
		return errors.Wrap(err, "failed to open SMTP data channel")
	}
	body := buildMessage(cfg.GetFrom(), to, subject, textBody, htmlBody)
	if _, err := w.Write(body); err != nil {
		return errors.Wrap(err, "failed to write email body")
	}
	if err := w.Close(); err != nil {
		return errors.Wrap(err, "failed to finish email body")
	}
	if err := client.Quit(); err != nil {
		return errors.Wrap(err, "failed to close SMTP session")
	}
	return nil
}

// SendVerificationEmail sends the signup verification email with the clickable
// verification link.
func (s *Sender) SendVerificationEmail(ctx context.Context, to, link string) error {
	subject := "Verify your email to finish signing up for Laelia"
	text := "Click the link below to verify your email and finish signing up:\n\n" + link +
		"\n\nIf you did not sign up, you can ignore this email."
	html := "<p>Click the link below to verify your email and finish signing up:</p>" +
		"<p><a href=\"" + link + "\">" + link + "</a></p>" +
		"<p>If you did not sign up, you can ignore this email.</p>"
	return s.Send(ctx, to, subject, text, html)
}

// buildMessage assembles a multipart/alternative message with a plain-text and
// an HTML part.
func buildMessage(from, to, subject, text, html string) []byte {
	var b strings.Builder
	write := func(parts ...string) {
		for _, p := range parts {
			_, _ = b.WriteString(p)
		}
	}
	write("From: ", mime.QEncoding.Encode("UTF-8", from), "\r\n")
	write("To: ", to, "\r\n")
	write("Subject: ", mime.QEncoding.Encode("UTF-8", subject), "\r\n")
	write("MIME-Version: 1.0\r\n")
	write("Content-Type: multipart/alternative; boundary=\"laelia-boundary\"\r\n")
	write("\r\n--laelia-boundary\r\n")
	write("Content-Type: text/plain; charset=utf-8\r\n\r\n")
	write(text)
	write("\r\n--laelia-boundary\r\n")
	write("Content-Type: text/html; charset=utf-8\r\n\r\n")
	write(html)
	write("\r\n--laelia-boundary--\r\n")
	return []byte(b.String())
}

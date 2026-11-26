package mailer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"time"
)

type ErrorKind string

const (
	ErrorKindPreAcceptanceRetryable ErrorKind = "pre_acceptance_retryable"
	ErrorKindPreAcceptancePermanent ErrorKind = "pre_acceptance_permanent"
	ErrorKindUncertain              ErrorKind = "uncertain"
)

type DeliveryError struct {
	Kind ErrorKind
	Err  error
}

func (e *DeliveryError) Error() string {
	return e.Err.Error()
}

func (e *DeliveryError) Unwrap() error {
	return e.Err
}

type SMTPConfig struct {
	Host      string
	Port      int
	Username  string
	Password  string
	FromName  string
	FromEmail string
	TLSMode   string // "starttls" | "tls" | "none"
	Timeout   time.Duration
}

type Attachment struct {
	Filename    string
	ContentType string
	Content     []byte
}

type Message struct {
	To          string
	Subject     string
	Body        string
	Attachments []Attachment
}

type SendResult struct {
	MessageID string
}

type Mailer interface {
	Send(ctx context.Context, cfg SMTPConfig, msg Message, onBeforeData func() error) (SendResult, error)
}

type SMTPMailer struct{}

func NewSMTPMailer() *SMTPMailer {
	return &SMTPMailer{}
}

func (m *SMTPMailer) Send(ctx context.Context, cfg SMTPConfig, msg Message, onBeforeData func() error) (SendResult, error) {
	if strings.TrimSpace(cfg.Host) == "" {
		return SendResult{}, &DeliveryError{Kind: ErrorKindPreAcceptancePermanent, Err: errors.New("SMTP host is not configured")}
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return SendResult{}, &DeliveryError{Kind: ErrorKindPreAcceptancePermanent, Err: errors.New("SMTP port is invalid")}
	}
	fromEmail := strings.TrimSpace(cfg.FromEmail)
	if fromEmail == "" {
		fromEmail = strings.TrimSpace(cfg.Username)
	}
	if fromEmail == "" {
		return SendResult{}, &DeliveryError{Kind: ErrorKindPreAcceptancePermanent, Err: errors.New("sender email address is not configured")}
	}
	toEmail := strings.TrimSpace(msg.To)
	if toEmail == "" {
		return SendResult{}, &DeliveryError{Kind: ErrorKindPreAcceptancePermanent, Err: errors.New("recipient email address is missing")}
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	addr := net.JoinHostPort(strings.TrimSpace(cfg.Host), strconv.Itoa(cfg.Port))
	tlsMode := strings.ToLower(strings.TrimSpace(cfg.TLSMode))
	if tlsMode == "" {
		tlsMode = "starttls"
	}

	dialer := &net.Dialer{Timeout: timeout}
	var conn net.Conn
	var err error
	if tlsMode == "tls" {
		tlsDialer := &tls.Dialer{
			NetDialer: dialer,
			Config:    &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12},
		}
		conn, err = tlsDialer.DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return SendResult{}, &DeliveryError{Kind: ErrorKindPreAcceptanceRetryable, Err: fmt.Errorf("connect SMTP %s: %w", addr, err)}
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return SendResult{}, &DeliveryError{Kind: ErrorKindPreAcceptanceRetryable, Err: fmt.Errorf("SMTP greeting failed: %w", err)}
	}
	defer func() {
		_ = client.Close()
	}()

	if tlsMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
				return SendResult{}, &DeliveryError{Kind: ErrorKindPreAcceptanceRetryable, Err: fmt.Errorf("SMTP STARTTLS failed: %w", err)}
			}
		} else {
			return SendResult{}, &DeliveryError{Kind: ErrorKindPreAcceptancePermanent, Err: errors.New("SMTP server does not support STARTTLS")}
		}
	}

	if strings.TrimSpace(cfg.Username) != "" {
		var auth smtp.Auth
		if tlsMode == "none" {
			auth = &allowPlainAuth{username: cfg.Username, password: cfg.Password}
		} else {
			auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
		}
		if err := client.Auth(auth); err != nil {
			return SendResult{}, classifyPreDataError("SMTP authentication failed", err)
		}
	}

	if err := client.Mail(fromEmail); err != nil {
		return SendResult{}, classifyPreDataError("SMTP MAIL FROM rejected", err)
	}
	if err := client.Rcpt(toEmail); err != nil {
		return SendResult{}, classifyPreDataError("SMTP RCPT TO rejected", err)
	}

	messageID := generateMessageID(fromEmail)
	rawMIME := BuildMIMEMessage(fromEmail, cfg.FromName, toEmail, msg.Subject, msg.Body, messageID, msg.Attachments)

	// Point of no return: record `sending` durably in SQLite BEFORE initiating DATA transfer.
	if onBeforeData != nil {
		if err := onBeforeData(); err != nil {
			return SendResult{}, &DeliveryError{Kind: ErrorKindPreAcceptanceRetryable, Err: fmt.Errorf("record pre-dispatch state: %w", err)}
		}
	}

	// Optional test hook for deterministic crash/restart verification mid-dispatch.
	if delayStr := strings.TrimSpace(os.Getenv("ROLERELAY_DISPATCH_DELAY_MS")); delayStr != "" {
		if ms, parseErr := strconv.Atoi(delayStr); parseErr == nil && ms > 0 {
			select {
			case <-ctx.Done():
				return SendResult{}, &DeliveryError{Kind: ErrorKindUncertain, Err: fmt.Errorf("dispatch interrupted during DATA stage: %w", ctx.Err())}
			case <-time.After(time.Duration(ms) * time.Millisecond):
			}
		}
	}

	dataWriter, err := client.Data()
	if err != nil {
		// DATA command itself was rejected before message body transmission.
		return SendResult{}, classifyPreDataError("SMTP DATA command rejected", err)
	}

	if _, err := dataWriter.Write(rawMIME); err != nil {
		return SendResult{}, &DeliveryError{Kind: ErrorKindUncertain, Err: fmt.Errorf("interrupted while transmitting email body: %w", err)}
	}
	if err := dataWriter.Close(); err != nil {
		// Explicit 5xx rejection after DATA means server rejected it; connection drop / EOF / timeout is UNCERTAIN!
		var tpErr *textproto.Error
		if errors.As(err, &tpErr) {
			if tpErr.Code >= 500 {
				return SendResult{}, &DeliveryError{Kind: ErrorKindPreAcceptancePermanent, Err: fmt.Errorf("SMTP server rejected message (%d): %s", tpErr.Code, tpErr.Msg)}
			}
			if tpErr.Code >= 400 {
				return SendResult{}, &DeliveryError{Kind: ErrorKindPreAcceptanceRetryable, Err: fmt.Errorf("SMTP server temporarily rejected message (%d): %s", tpErr.Code, tpErr.Msg)}
			}
		}
		return SendResult{}, &DeliveryError{Kind: ErrorKindUncertain, Err: fmt.Errorf("delivery uncertain after DATA transmission: %w", err)}
	}

	_ = client.Quit()
	return SendResult{MessageID: messageID}, nil
}

func BuildMIMEMessage(fromEmail, fromName, toEmail, subject, body, messageID string, attachments []Attachment) []byte {
	boundary := "rolerelay-" + randomHex(12)
	var buf bytes.Buffer

	fromHeader := fromEmail
	if strings.TrimSpace(fromName) != "" {
		fromHeader = fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("utf-8", strings.TrimSpace(fromName)), fromEmail)
	}

	buf.WriteString("From: " + fromHeader + "\r\n")
	buf.WriteString("To: " + toEmail + "\r\n")
	buf.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	buf.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n")
	buf.WriteString("Message-ID: " + messageID + "\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=%q\r\n\r\n", boundary))

	// Plain text part
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	buf.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	normalizedBody := strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")
	buf.WriteString(normalizedBody + "\r\n")

	// Attachments
	for _, att := range attachments {
		ct := strings.TrimSpace(att.ContentType)
		if ct == "" {
			ct = "application/pdf"
		}
		safeName := strings.NewReplacer(`"`, "", "\r", "", "\n", "").Replace(att.Filename)
		if safeName == "" {
			safeName = "cv.pdf"
		}
		buf.WriteString("--" + boundary + "\r\n")
		buf.WriteString(fmt.Sprintf("Content-Type: %s; name=%q\r\n", ct, safeName))
		buf.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=%q\r\n", safeName))
		buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
		writeBase64Lines(&buf, att.Content)
	}

	buf.WriteString("--" + boundary + "--\r\n")
	return buf.Bytes()
}

func writeBase64Lines(buf *bytes.Buffer, data []byte) {
	encoded := base64.StdEncoding.EncodeToString(data)
	for i := 0; i < len(encoded); i += 76 {
		end := min(i+76, len(encoded))
		buf.WriteString(encoded[i:end])
		buf.WriteString("\r\n")
	}
}

func classifyPreDataError(prefix string, err error) *DeliveryError {
	var tpErr *textproto.Error
	if errors.As(err, &tpErr) && tpErr.Code >= 500 {
		return &DeliveryError{
			Kind: ErrorKindPreAcceptancePermanent,
			Err:  fmt.Errorf("%s (%d): %s", prefix, tpErr.Code, tpErr.Msg),
		}
	}
	return &DeliveryError{
		Kind: ErrorKindPreAcceptanceRetryable,
		Err:  fmt.Errorf("%s: %w", prefix, err),
	}
}

func generateMessageID(fromEmail string) string {
	domain := "rolerelay.local"
	if parts := strings.Split(fromEmail, "@"); len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		domain = strings.TrimSpace(parts[1])
	}
	return fmt.Sprintf("<%d.%s@%s>", time.Now().UTC().UnixNano(), randomHex(6), domain)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// allowPlainAuth allows SMTP PLAIN authentication on local loopback test servers when tlsMode == "none".
type allowPlainAuth struct {
	username string
	password string
}

func (a *allowPlainAuth) Start(_ *smtp.ServerInfo) (string, []byte, error) {
	resp := []byte("\x00" + a.username + "\x00" + a.password)
	return "PLAIN", resp, nil
}

func (a *allowPlainAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("unexpected SMTP AUTH challenge")
	}
	return nil, nil
}

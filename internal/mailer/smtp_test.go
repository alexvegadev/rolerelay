package mailer

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

func TestBuildMIMEMessageWithPDFAttachments(t *testing.T) {
	pdfEN := []byte("%PDF-1.4\nEnglish CV binary payload")
	pdfES := []byte("%PDF-1.4\nSpanish CV binary payload")

	raw := BuildMIMEMessage(
		"ada@example.com",
		"Ada Lovelace",
		"jobs@acme.dev",
		"Application: Senior Go Engineer",
		"Hello team,\nPlease find my CVs attached.",
		"<test-id@example.com>",
		[]Attachment{
			{Filename: "ada-cv-en.pdf", ContentType: "application/pdf", Content: pdfEN},
			{Filename: "ada-cv-es.pdf", ContentType: "application/pdf", Content: pdfES},
		},
	)

	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("mail.ReadMessage failed: %v", err)
	}
	if parsed.Header.Get("To") != "jobs@acme.dev" {
		t.Fatalf("unexpected To header: %q", parsed.Header.Get("To"))
	}

	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/mixed" {
		t.Fatalf("expected multipart/mixed, got %q err=%v", mediaType, err)
	}

	mr := multipart.NewReader(parsed.Body, params["boundary"])
	var bodyText string
	attachments := map[string][]byte{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextPart failed: %v", err)
		}
		data, err := io.ReadAll(part)
		if err != nil {
			t.Fatal(err)
		}
		if part.FileName() == "" {
			bodyText = string(data)
		} else {
			decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(data), "\r\n", ""))
			if err != nil {
				t.Fatalf("decode base64 attachment %s: %v", part.FileName(), err)
			}
			attachments[part.FileName()] = decoded
		}
	}

	if !strings.Contains(bodyText, "Please find my CVs attached.") {
		t.Fatalf("unexpected body text: %q", bodyText)
	}
	if !bytes.Equal(attachments["ada-cv-en.pdf"], pdfEN) {
		t.Fatalf("EN attachment bytes mismatch")
	}
	if !bytes.Equal(attachments["ada-cv-es.pdf"], pdfES) {
		t.Fatalf("ES attachment bytes mismatch")
	}
}

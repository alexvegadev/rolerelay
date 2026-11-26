package pdfextract

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func buildTestPDF(lines []string) []byte {
	var stream strings.Builder
	if len(lines) > 0 {
		stream.WriteString("BT\n/F1 12 Tf\n72 720 Td\n")
		for i, line := range lines {
			if i > 0 {
				stream.WriteString("0 -18 Td\n")
			}
			escaped := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(line)
			stream.WriteString("(")
			stream.WriteString(escaped)
			stream.WriteString(") Tj\n")
		}
		stream.WriteString("ET\n")
	}
	content := stream.String()

	objects := []string{
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n",
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n",
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>\nendobj\n",
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n",
		fmt.Sprintf("5 0 obj\n<< /Length %d >>\nstream\n%sendstream\nendobj\n", len(content), content),
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, obj := range objects {
		offsets[i+1] = buf.Len()
		buf.WriteString(obj)
	}
	xrefOffset := buf.Len()
	buf.WriteString(fmt.Sprintf("xref\n0 %d\n", len(objects)+1))
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= len(objects); i++ {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", offsets[i]))
	}
	buf.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xrefOffset))
	return buf.Bytes()
}

func TestExtractValidTextPDF(t *testing.T) {
	pdfBytes := buildTestPDF([]string{
		"Maria Garcia - Senior Software Engineer",
		"Go, TypeScript, SQLite, Distributed Systems",
	})

	res, err := Extract(pdfBytes)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if res.PageCount != 1 {
		t.Fatalf("expected 1 page, got %d", res.PageCount)
	}
	if !strings.Contains(res.Text, "Maria Garcia") || !strings.Contains(res.Text, "SQLite") {
		t.Fatalf("expected extracted text to contain resume content, got %q", res.Text)
	}
}

func TestExtractEmptyTextPDFReturnsClearError(t *testing.T) {
	// Simulates a scanned/image-only PDF that has a valid page structure but zero extractable text.
	pdfBytes := buildTestPDF(nil)

	_, err := Extract(pdfBytes)
	if !errors.Is(err, ErrNoExtractableText) {
		t.Fatalf("expected ErrNoExtractableText, got %v", err)
	}
}

func TestExtractInvalidFileReturnsInvalidPDF(t *testing.T) {
	_, err := Extract([]byte("not a pdf file"))
	if !errors.Is(err, ErrInvalidPDF) {
		t.Fatalf("expected ErrInvalidPDF, got %v", err)
	}
}

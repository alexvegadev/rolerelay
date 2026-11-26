package pdfextract

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/dslipak/pdf"
)

const MaxPDFSizeBytes = 20 * 1024 * 1024 // 20 MiB

var (
	ErrEmptyFile         = errors.New("the uploaded file is empty")
	ErrFileTooLarge      = errors.New("the uploaded PDF exceeds the 20 MB limit")
	ErrInvalidPDF        = errors.New("the uploaded file is not a valid PDF document")
	ErrNoExtractableText = errors.New("this PDF has no extractable text; upload a text-based PDF or run OCR on scanned pages first")
)

type Result struct {
	Text      string
	PageCount int
}

func Extract(data []byte) (res Result, err error) {
	if len(data) == 0 {
		return Result{}, ErrEmptyFile
	}
	if len(data) > MaxPDFSizeBytes {
		return Result{}, ErrFileTooLarge
	}

	headerIdx := bytes.Index(data[:min(len(data), 1024)], []byte("%PDF-"))
	if headerIdx < 0 {
		return Result{}, ErrInvalidPDF
	}

	defer func() {
		if r := recover(); r != nil {
			res = Result{}
			err = fmt.Errorf("%w: parser error", ErrInvalidPDF)
		}
	}()

	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrInvalidPDF, err)
	}

	numPages := reader.NumPage()
	if numPages <= 0 {
		return Result{}, ErrNoExtractableText
	}

	var pages []string
	for i := 1; i <= numPages; i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		pageText, pageErr := extractPageText(page)
		if pageErr != nil {
			continue
		}
		pageText = normalizeWhitespace(pageText)
		if pageText != "" {
			pages = append(pages, pageText)
		}
	}

	combined := strings.TrimSpace(strings.Join(pages, "\n\n"))
	if !hasReadableText(combined) {
		return Result{}, ErrNoExtractableText
	}

	return Result{
		Text:      combined,
		PageCount: numPages,
	}, nil
}

func extractPageText(page pdf.Page) (string, error) {
	text, err := page.GetPlainText(nil)
	if err == nil && strings.TrimSpace(text) != "" {
		return text, nil
	}

	rows, rowErr := page.GetTextByRow()
	if rowErr != nil {
		if err != nil {
			return "", err
		}
		return "", rowErr
	}

	var b strings.Builder
	for _, row := range rows {
		for idx, word := range row.Content {
			if idx > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(word.S)
		}
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func normalizeWhitespace(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		cleaned = append(cleaned, strings.Join(fields, " "))
	}
	return strings.Join(cleaned, "\n")
}

func hasReadableText(s string) bool {
	alnum := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			alnum++
			if alnum >= 3 {
				return true
			}
		}
	}
	return false
}

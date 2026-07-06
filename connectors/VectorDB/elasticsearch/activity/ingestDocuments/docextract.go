package ingestDocuments

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
)

// ExtractTextFromBytes extracts plain text from a byte slice given its format.
func ExtractTextFromBytes(data []byte, format string) (string, error) {
	switch strings.ToLower(format) {
	case "pdf":
		return extractPDF(data)
	case "docx":
		return extractDOCX(data)
	default:
		return string(data), nil
	}
}

func extractPDF(data []byte) (string, error) {
	r := bytes.NewReader(data)
	pr, err := pdf.NewReader(r, int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("pdf.NewReader: %w", err)
	}
	var sb strings.Builder
	for i := 1; i <= pr.NumPage(); i++ {
		page := pr.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		sb.WriteString(text)
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

func extractDOCX(data []byte) (string, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("docx zip.NewReader: %w", err)
	}
	for _, f := range r.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("docx open document.xml: %w", err)
		}
		defer rc.Close()
		buf := new(bytes.Buffer)
		if _, err := buf.ReadFrom(rc); err != nil {
			return "", fmt.Errorf("docx read document.xml: %w", err)
		}
		xml := buf.String()
		// Strip XML tags
		var text strings.Builder
		inTag := false
		for _, ch := range xml {
			if ch == '<' {
				inTag = true
			} else if ch == '>' {
				inTag = false
				text.WriteRune(' ')
			} else if !inTag {
				text.WriteRune(ch)
			}
		}
		return strings.TrimSpace(text.String()), nil
	}
	return "", fmt.Errorf("docx: word/document.xml not found")
}

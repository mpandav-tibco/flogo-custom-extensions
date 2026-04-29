package ingestDocuments

// docextract.go — Binary document text extraction for IngestDocuments activity.
//
// Supported formats:
//   .pdf  — github.com/ledongthuc/pdf  (MIT, pure Go, CGo-free)
//   .docx — stdlib archive/zip + encoding/xml  (no extra dependency)
//   .txt / .md — raw UTF-8 passthrough
//
// Usage:
//   text, err := ExtractTextFromBytes(data, "report.pdf")

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
)

// ExtractTextFromBytes extracts plain text from raw file bytes, using the
// filename extension to select the correct parser.
func ExtractTextFromBytes(data []byte, filename string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".pdf":
		return extractPDF(data)
	case ".docx":
		return extractDOCX(data)
	case ".txt", ".md", ".text", ".markdown":
		return strings.TrimSpace(string(data)), nil
	default:
		// Graceful fallback: if the content looks like UTF-8 text, return it as-is.
		if looksLikeText(data) {
			return strings.TrimSpace(string(data)), nil
		}
		return "", fmt.Errorf("unsupported file type %q — supported: .pdf, .docx, .txt, .md", ext)
	}
}

// extractPDF extracts plain text from all pages of a PDF using ledongthuc/pdf.
// It tolerates partially-unreadable pages (e.g. scanned images) by skipping them.
func extractPDF(data []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("pdf: open failed: %w", err)
	}
	var sb strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err != nil {
			continue // skip unreadable pages (e.g. image-only)
		}
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(text)
	}
	return strings.TrimSpace(sb.String()), nil
}

// extractDOCX extracts text from a .docx file (Office Open XML format).
// A .docx is a ZIP archive; the document body lives in word/document.xml.
// Text runs (<w:t>) are extracted and paragraphs (<w:p>) are separated by newlines.
// No external dependencies — uses only stdlib archive/zip and encoding/xml.
func extractDOCX(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("docx: not a valid zip archive: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("docx: cannot open word/document.xml: %w", err)
		}
		defer rc.Close()
		return parseWordXML(rc)
	}
	return "", fmt.Errorf("docx: word/document.xml not found — is this a valid .docx file?")
}

// parseWordXML streams word/document.xml and extracts text from <w:t> elements.
// Each <w:p> paragraph boundary inserts a newline.
func parseWordXML(r io.Reader) (string, error) {
	const wNS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	var sb strings.Builder
	inText := false
	dec := xml.NewDecoder(r)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("docx: XML parse error: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				// Paragraph boundary — add newline between paragraphs.
				if sb.Len() > 0 {
					sb.WriteByte('\n')
				}
			case "t":
				// Text run — only collect chars inside <w:t>.
				if t.Name.Space == wNS || t.Name.Space == "" {
					inText = true
				}
			case "br", "cr":
				// Line break inside a paragraph.
				sb.WriteByte('\n')
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inText = false
			}
		case xml.CharData:
			if inText {
				sb.Write(t)
			}
		}
	}
	return strings.TrimSpace(sb.String()), nil
}

// fileContentToBytes converts the `content` field of a files[] item to []byte.
//
// Flogo passes []byte values through its JSON data mapper which encodes them as
// base64 strings. This function handles all forms that may arrive at the activity:
//   - []byte       — direct Go value (programmatic use / unit tests)
//   - string       — base64-encoded (Flogo JSON mapper) or plain UTF-8 text
//   - []interface{} — Flogo wraps [][]byte as []interface{} when the trigger
//     stores multiple files under the same field name; we take [0]
func fileContentToBytes(content interface{}) ([]byte, error) {
	switch v := content.(type) {
	case []byte:
		return v, nil
	case string:
		// Try standard base64 first (Flogo JSON-encodes []byte as base64).
		if b, err := base64.StdEncoding.DecodeString(v); err == nil && len(b) > 0 {
			return b, nil
		}
		// Try URL-safe base64 (some Flogo versions / HTTP clients use this).
		if b, err := base64.URLEncoding.DecodeString(v); err == nil && len(b) > 0 {
			return b, nil
		}
		// Treat as raw UTF-8 text (e.g. plain .txt content).
		return []byte(v), nil
	case []interface{}:
		// Outer array from trigger: [][]byte → []interface{}{[]byte, []byte, ...}
		// Take the first element.
		if len(v) == 0 {
			return nil, fmt.Errorf("content array is empty")
		}
		return fileContentToBytes(v[0])
	case [][]byte:
		// Flogo multipart trigger delivers file fields as [][]byte
		// (outer = multiple files with the same field name, inner = bytes).
		// When the whole slice is mapped to fileContent, take the first file.
		if len(v) == 0 {
			return nil, fmt.Errorf("content array is empty")
		}
		return v[0], nil
	default:
		return nil, fmt.Errorf("unexpected content type %T — expected base64 string or []byte", content)
	}
}

// looksLikeText returns true if the byte slice appears to be valid UTF-8 text
// (less than 1% null bytes). Used as a fallback for unknown file extensions.
func looksLikeText(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	nulls := 0
	for _, b := range data {
		if b == 0 {
			nulls++
		}
	}
	return float64(nulls)/float64(len(data)) < 0.01
}

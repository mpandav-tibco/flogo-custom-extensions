package ingestDocuments

// docextract.go — Binary document text extraction for IngestDocuments activity.
//
// Supported formats:
//   .pdf  — github.com/ledongthuc/pdf  (MIT, pure Go, CGo-free)
//   .docx — stdlib archive/zip + encoding/xml  (no extra dependency)
//   .txt / .md — raw UTF-8 passthrough

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

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
	case ".doc":
		return "", fmt.Errorf("unsupported file type %q: legacy binary .doc format cannot be read — convert to .docx or .pdf first", ext)
	default:
		if looksLikeText(data) {
			return strings.TrimSpace(string(data)), nil
		}
		return "", fmt.Errorf("unsupported file type %q — supported: .pdf, .docx, .txt, .md", ext)
	}
}

func extractPDF(data []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("pdf: open failed: %w", err)
	}
	var pages []string
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		pageText := extractPageRows(p)
		if strings.TrimSpace(pageText) != "" {
			pages = append(pages, pageText)
		}
	}
	if len(pages) == 0 {
		return "", nil
	}
	pages = stripPageHeadersFooters(pages)
	combined := strings.Join(pages, "\n\n")
	return postProcessPDFText(combined), nil
}

func extractPageRows(p pdf.Page) string {
	rows, err := p.GetTextByRow()
	if err != nil || len(rows) == 0 {
		text, _ := p.GetPlainText(nil)
		return text
	}
	var lines []string
	for _, row := range rows {
		var sb strings.Builder
		hasPrev := false
		var prevX, prevW, prevFontSize float64
		for _, t := range row.Content {
			s := strings.TrimSpace(t.S)
			if s == "" {
				continue
			}
			if !hasPrev {
				sb.WriteString(s)
				hasPrev = true
			} else {
				prevEnd := prevX + prevW
				if prevW == 0 {
					prevEnd = prevX + float64(len([]rune(sb.String())))*prevFontSize*0.5
				}
				gap := t.X - prevEnd
				spaceWidth := prevFontSize * 0.25
				if spaceWidth <= 0 {
					spaceWidth = 3.0
				}
				if gap > spaceWidth {
					sb.WriteString(" ")
				}
				sb.WriteString(s)
			}
			prevX = t.X
			prevW = t.W
			prevFontSize = t.FontSize
		}
		if line := sb.String(); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

var pdfHeadingRe = regexp.MustCompile(`^(\d+\.)*\d+\s{1,4}[A-Z]|^(Chapter|Section|Appendix|Part)\s+[\dA-Z]`)
var pdfAllCapsRe = regexp.MustCompile(`^[A-Z][A-Z0-9\s®\-–:,.'()]+$`)
var pdfPageNumRe = regexp.MustCompile(`^\d+$|^\d+\s*\||\|\s*\d+$`)
var pdfHyphenBreakRe = regexp.MustCompile(`([a-zA-Z])-\n([a-z])`)
var pdfBlankCollapseRe = regexp.MustCompile(`\n{3,}`)

func stripPageHeadersFooters(pages []string) []string {
	lineFreq := make(map[string]int)
	for _, page := range pages {
		lines := strings.Split(strings.TrimSpace(page), "\n")
		n := len(lines)
		if n == 0 {
			continue
		}
		boundary := min(3, n)
		seen := make(map[string]bool)
		candidates := append(lines[:boundary:boundary], lines[max(0, n-boundary):]...)
		for _, l := range candidates {
			t := strings.TrimSpace(l)
			if len(t) >= 4 && !seen[t] {
				lineFreq[t]++
				seen[t] = true
			}
		}
	}
	threshold := max(2, len(pages)*30/100)
	boilerplate := make(map[string]bool)
	for line, cnt := range lineFreq {
		if cnt >= threshold {
			boilerplate[line] = true
		}
	}
	if len(boilerplate) == 0 {
		return pages
	}
	cleaned := make([]string, len(pages))
	for i, page := range pages {
		var kept []string
		for _, l := range strings.Split(page, "\n") {
			if !boilerplate[strings.TrimSpace(l)] {
				kept = append(kept, l)
			}
		}
		cleaned[i] = strings.Join(kept, "\n")
	}
	return cleaned
}

func postProcessPDFText(text string) string {
	text = pdfHyphenBreakRe.ReplaceAllString(text, "$1$2")

	rawLines := strings.Split(text, "\n")
	out := make([]string, 0, len(rawLines))
	for i, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if isLikelyPDFHeading(trimmed, rawLines, i) {
			out = append(out, "## "+trimmed)
		} else {
			out = append(out, line)
		}
	}

	result := pdfBlankCollapseRe.ReplaceAllString(strings.Join(out, "\n"), "\n\n")
	return strings.TrimSpace(result)
}

func isLikelyPDFHeading(line string, allLines []string, idx int) bool {
	if len(line) == 0 || len(line) > 100 || strings.HasPrefix(line, "#") {
		return false
	}
	if pdfHeadingRe.MatchString(line) {
		return true
	}
	if strings.ToUpper(line) == line && len(line) <= 60 &&
		pdfAllCapsRe.MatchString(line) && !pdfPageNumRe.MatchString(line) {
		return true
	}
	prevBlank := idx == 0 || strings.TrimSpace(allLines[idx-1]) == ""
	nextBlank := idx >= len(allLines)-1 || strings.TrimSpace(allLines[idx+1]) == ""
	if prevBlank && nextBlank && isTitleCaseLine(line) && len(line) <= 80 {
		return true
	}
	return false
}

func isTitleCaseLine(s string) bool {
	small := map[string]bool{
		"a": true, "an": true, "the": true, "and": true, "or": true,
		"but": true, "in": true, "on": true, "at": true, "to": true,
		"for": true, "of": true, "with": true, "by": true,
	}
	words := strings.Fields(s)
	if len(words) < 2 || len(words) > 12 {
		return false
	}
	for j, w := range words {
		r := []rune(w)
		if len(r) == 0 {
			continue
		}
		if j > 0 && small[strings.ToLower(w)] {
			continue
		}
		if !unicode.IsUpper(r[0]) {
			return false
		}
	}
	return true
}

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
				if sb.Len() > 0 {
					sb.WriteByte('\n')
				}
			case "t":
				if t.Name.Space == wNS || t.Name.Space == "" {
					inText = true
				}
			case "br", "cr":
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

func fileContentToBytes(content interface{}) ([]byte, error) {
	switch v := content.(type) {
	case []byte:
		return v, nil
	case string:
		if b, err := base64.StdEncoding.DecodeString(v); err == nil && len(b) > 0 {
			return b, nil
		}
		if b, err := base64.URLEncoding.DecodeString(v); err == nil && len(b) > 0 {
			return b, nil
		}
		return []byte(v), nil
	case []interface{}:
		if len(v) == 0 {
			return nil, fmt.Errorf("content array is empty")
		}
		return fileContentToBytes(v[0])
	case [][]byte:
		if len(v) == 0 {
			return nil, fmt.Errorf("content array is empty")
		}
		return v[0], nil
	default:
		return nil, fmt.Errorf("unexpected content type %T — expected base64 string or []byte", content)
	}
}

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

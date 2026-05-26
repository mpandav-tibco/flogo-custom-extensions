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
		// Legacy binary Word format (.doc) is not extractable without a
		// specialised parser. Please convert to .docx (File → Save As in Word)
		// or export as PDF before ingesting.
		return "", fmt.Errorf("unsupported file type %q: legacy binary .doc format cannot be read — convert to .docx or .pdf first", ext)
	default:
		// Graceful fallback: if the content looks like UTF-8 text, return it as-is.
		if looksLikeText(data) {
			return strings.TrimSpace(string(data)), nil
		}
		return "", fmt.Errorf("unsupported file type %q — supported: .pdf, .docx, .txt, .md", ext)
	}
}

// extractPDF extracts and reconstructs text from a PDF.
//
// Level 1 — line reconstruction: uses GetTextByRow() which groups text objects
// by Y-coordinate so words on the same visual line are joined, fixing the
// word-per-line output produced by GetPlainText() on professionally-typeset PDFs.
//
// Level 2 — structural clean-up:
//   - Strips repeating page headers/footers (e.g. "TIBCO Flogo® User Guide 352 |").
//   - Re-joins words split by typographic hyphens at end-of-line.
//   - Emits ## markers before detected section headings so the heading chunk
//     strategy can split on them.
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
	// Level 2: strip repeating page headers/footers.
	pages = stripPageHeadersFooters(pages)
	combined := strings.Join(pages, "\n\n")
	// Level 2: hyphen rejoining, heading detection, whitespace normalisation.
	return postProcessPDFText(combined), nil
}

// extractPageRows converts one PDF page into a string using GetTextByRow so
// that text objects on the same horizontal band are joined as a single line.
// Falls back to GetPlainText if GetTextByRow returns an error or empty result.
//
// Level 1.5 — gap-based word joining: instead of blindly joining every text
// object with a space, we compare the X-gap between consecutive elements to a
// font-size-derived space-width threshold.  Characters that belong to the same
// word (kerned or individually-positioned glyphs) are concatenated directly;
// only genuine inter-word gaps produce a space.  This fixes the "D e s i g n"
// character-per-character artefact seen in professionally-typeset PDFs where
// each glyph is positioned individually.
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
				// Estimate end of previous element.
				prevEnd := prevX + prevW
				if prevW == 0 {
					// Fall back: estimate width from rune count and font size.
					prevEnd = prevX + float64(len([]rune(sb.String())))*prevFontSize*0.5
				}
				gap := t.X - prevEnd
				// Typical space width ≈ 0.25× font size (in PDF points).
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

// ── Level 2 helpers ───────────────────────────────────────────────────────────

// pdfHeadingRe matches common numbered section titles and chapter/appendix markers.
var pdfHeadingRe = regexp.MustCompile(`^(\d+\.)*\d+\s{1,4}[A-Z]|^(Chapter|Section|Appendix|Part)\s+[\dA-Z]`)

// pdfAllCapsRe matches lines composed entirely of upper-case letters, digits,
// spaces, and common punctuation — used to catch ALL-CAPS section headings.
var pdfAllCapsRe = regexp.MustCompile(`^[A-Z][A-Z0-9\s®\-–:,.'()]+$`)

// pdfPageNumRe matches bare page-number patterns ("352", "352 |", "| 352").
var pdfPageNumRe = regexp.MustCompile(`^\d+$|^\d+\s*\||\|\s*\d+$`)

// pdfHyphenBreakRe matches a letter-hyphen at end-of-line followed by a
// lowercase letter — the typographic soft-hyphen line-break pattern.
var pdfHyphenBreakRe = regexp.MustCompile(`([a-zA-Z])-\n([a-z])`)

// pdfBlankCollapseRe collapses three or more consecutive blank lines to two.
var pdfBlankCollapseRe = regexp.MustCompile(`\n{3,}`)

// stripPageHeadersFooters detects lines that appear verbatim in the first or
// last 3 lines of ≥30% of pages (minimum 2 pages) and removes them from every
// page.  This eliminates repeated "TIBCO Flogo® User Guide 352 |" banners.
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

// postProcessPDFText applies Level 2 transformations to the combined text:
//  1. Re-joins words split by typographic end-of-line hyphens.
//  2. Emits ## markers before detected section headings.
//  3. Collapses excessive blank lines.
func postProcessPDFText(text string) string {
	// 1. Rejoin soft hyphens: "configu-\nration" → "configuration"
	text = pdfHyphenBreakRe.ReplaceAllString(text, "$1$2")

	// 2. Heading detection — line by line.
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

	// 3. Collapse blank lines.
	result := pdfBlankCollapseRe.ReplaceAllString(strings.Join(out, "\n"), "\n\n")
	return strings.TrimSpace(result)
}

// isLikelyPDFHeading returns true when a line looks like a section heading.
func isLikelyPDFHeading(line string, allLines []string, idx int) bool {
	if len(line) == 0 || len(line) > 100 || strings.HasPrefix(line, "#") {
		return false
	}
	// Numbered: "1.2 Title…", "Chapter 3 …"
	if pdfHeadingRe.MatchString(line) {
		return true
	}
	// ALL-CAPS line ≤ 60 chars that is not a bare page number.
	if strings.ToUpper(line) == line && len(line) <= 60 &&
		pdfAllCapsRe.MatchString(line) && !pdfPageNumRe.MatchString(line) {
		return true
	}
	// Short title-case line isolated by blank lines on both sides.
	prevBlank := idx == 0 || strings.TrimSpace(allLines[idx-1]) == ""
	nextBlank := idx >= len(allLines)-1 || strings.TrimSpace(allLines[idx+1]) == ""
	if prevBlank && nextBlank && isTitleCaseLine(line) && len(line) <= 80 {
		return true
	}
	return false
}

// isTitleCaseLine returns true when every significant word starts with an
// upper-case letter (articles/prepositions are exempt after the first word).
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

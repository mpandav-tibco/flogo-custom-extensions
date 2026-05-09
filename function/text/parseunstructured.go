package textfn

// parseunstructured.go — text.parseUnstructured() Flogo function
//
// Converts the JSON response from the Unstructured.io REST API
// (POST /general/v0/general) into clean plain text ready for vector embedding.
//
// The Unstructured.io API returns a JSON array of elements, each with:
//   - "type"     : element kind (Title, NarrativeText, Text, Table, ListItem, …)
//   - "text"     : raw extracted text
//   - "metadata" : dict; for Table elements, "text_as_html" contains an HTML table
//
// Table elements are special: instead of the flat whitespace-joined cell text
// produced by Tika, Unstructured provides a full HTML table with <th>/<td>.
// This function parses that HTML to reconstruct rows as labelled prose:
//
//   Header1: val1 | Header2: val2 | Header3: val3
//
// For multi-environment tables (DEV / SIT / UAT / PROD columns) this preserves
// the column context that allows the LLM to answer environment-specific questions.
//
// Usage in Flogo mapper:
//
//	=text.parseUnstructured($activity[UnstructuredExtract].responseBody.data)

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/project-flogo/core/data"
	"github.com/project-flogo/core/data/coerce"
	"github.com/project-flogo/core/data/expression/function"
	"github.com/project-flogo/core/support/log"
)

func init() {
	_ = function.Register(&fnParseUnstructured{})
}

// unstructuredElement is one item in the Unstructured.io JSON response array.
type unstructuredElement struct {
	Type     string                 `json:"type"`
	Text     string                 `json:"text"`
	Metadata map[string]interface{} `json:"metadata"`
}

// --- HTML table parsing helpers (regex-based, no external dependencies) ---

// reHTMLTag strips any HTML tag including its attributes.
var reHTMLTag = regexp.MustCompile(`<[^>]+>`)

// reHTMLEnt decodes the small set of entities Unstructured emits.
var reHTMLEnt = regexp.MustCompile(`&(amp|lt|gt|quot|apos|nbsp|#[0-9]{1,5}|#x[0-9a-fA-F]{1,5});`)

// reTR matches one <tr>…</tr> block (case-insensitive, dot-all).
var reTR = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)

// reTH matches one <th>…</th> block.
var reTH = regexp.MustCompile(`(?is)<th[^>]*>(.*?)</th>`)

// reTD matches one <td>…</td> block.
var reTD = regexp.MustCompile(`(?is)<td[^>]*>(.*?)</td>`)

// reTHEAD matches the <thead>…</thead> section.
var reTHEAD = regexp.MustCompile(`(?is)<thead[^>]*>(.*?)</thead>`)

// reTBODY matches the <tbody>…</tbody> section.
var reTBODY = regexp.MustCompile(`(?is)<tbody[^>]*>(.*?)</tbody>`)

// cellText strips HTML tags and decodes entities from a table cell's inner HTML.
func cellText(inner string) string {
	// Replace <br> variants with a space before stripping all tags
	inner = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>`).ReplaceAllString(inner, " ")
	inner = reHTMLTag.ReplaceAllString(inner, "")
	inner = reHTMLEnt.ReplaceAllStringFunc(inner, func(m string) string {
		raw := m[1 : len(m)-1] // strip leading & and trailing ;
		switch strings.ToLower(raw) {
		case "amp":
			return "&"
		case "lt":
			return "<"
		case "gt":
			return ">"
		case "quot":
			return "\""
		case "apos":
			return "'"
		case "nbsp":
			return " "
		}
		// Numeric/hex entity — just drop it (noise in OCR output)
		return ""
	})
	return strings.TrimSpace(inner)
}

// extractCells returns the text of all <th> or <td> cells in an HTML row fragment.
func extractCells(rowInner string, useTH bool) []string {
	re := reTD
	if useTH {
		re = reTH
	}
	matches := re.FindAllStringSubmatch(rowInner, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) > 1 {
			out = append(out, cellText(m[1]))
		}
	}
	return out
}

// tableHTMLToText converts an Unstructured.io HTML table to labelled plain text.
//
// Strategy:
//  1. Extract header labels from <thead> — use the LAST <tr> row in thead because
//     multi-row headers have the most specific labels in the bottom row (e.g.
//     "Development", "Quality/Staging", "Production" under a broader "Environment").
//  2. For each <tbody> <tr>, pair non-empty cells with their header labels.
//     Format: "Label1: val1 | Label2: val2 | ..."
//  3. If no headers exist, join cells with " | ".
//  4. Return "" if the table yields no useful content.
func tableHTMLToText(html string) string {
	if html == "" {
		return ""
	}

	// --- Extract headers ---
	var headers []string
	if m := reTHEAD.FindStringSubmatch(html); len(m) > 1 {
		theadHTML := m[1]
		// Collect all <tr> rows in thead; take the last one as most specific
		trMatches := reTR.FindAllStringSubmatch(theadHTML, -1)
		for _, trm := range trMatches {
			if len(trm) < 2 {
				continue
			}
			cells := extractCells(trm[1], true) // <th>
			if len(cells) > 0 {
				headers = cells // overwrite — last row wins
			}
		}
	}

	// --- Extract body rows ---
	tbodyHTML := html
	if m := reTBODY.FindStringSubmatch(html); len(m) > 1 {
		tbodyHTML = m[1]
	}

	trMatches := reTR.FindAllStringSubmatch(tbodyHTML, -1)
	if len(trMatches) == 0 {
		// No tbody rows — fall back to thead data as a single row
		if len(headers) > 0 {
			return strings.Join(headers, " | ")
		}
		return ""
	}

	var rowLines []string
	for _, trm := range trMatches {
		if len(trm) < 2 {
			continue
		}
		cells := extractCells(trm[1], false) // <td>
		if len(cells) == 0 {
			continue
		}

		var parts []string
		for i, cell := range cells {
			if cell == "" {
				continue
			}
			if i < len(headers) && headers[i] != "" {
				parts = append(parts, headers[i]+": "+cell)
			} else {
				parts = append(parts, cell)
			}
		}
		if len(parts) > 0 {
			rowLines = append(rowLines, strings.Join(parts, " | "))
		}
	}

	return strings.Join(rowLines, "\n")
}

// --- Flogo function registration ---

type fnParseUnstructured struct{}

func (fnParseUnstructured) Name() string        { return "parseUnstructured" }
func (fnParseUnstructured) GetCategory() string { return "text" }

func (fnParseUnstructured) Sig() (paramTypes []data.Type, isVariadic bool) {
	return []data.Type{data.TypeString}, false
}

// skipElementType lists element types that add no semantic content for RAG.
var skipElementType = map[string]bool{
	"Footer":    true,
	"Header":    true,
	"PageBreak": true,
	"Image":     true,
}

// Eval parses the Unstructured.io JSON response body and returns clean plain text.
//
// Tables are converted to labelled row text preserving column context.
// All other elements contribute their plain "text" field.
// Footer/Header/Image/PageBreak elements are omitted.
func (fnParseUnstructured) Eval(params ...interface{}) (interface{}, error) {
	raw, err := coerce.ToString(params[0])
	if err != nil {
		return nil, fmt.Errorf("text.parseUnstructured: parameter must be a string, got %T", params[0])
	}
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}

	var elements []unstructuredElement
	if err := json.Unmarshal([]byte(raw), &elements); err != nil {
		return nil, fmt.Errorf("text.parseUnstructured: invalid JSON from Unstructured.io API: %w", err)
	}

	log.RootLogger().Debugf("text.parseUnstructured: parsing %d elements", len(elements))

	var parts []string
	for _, el := range elements {
		if skipElementType[el.Type] {
			continue
		}

		var text string
		if el.Type == "Table" {
			// Prefer HTML reconstruction to preserve column context
			if htmlVal, ok := el.Metadata["text_as_html"].(string); ok && htmlVal != "" {
				text = tableHTMLToText(htmlVal)
			}
			// Fall back to plain text if HTML yielded nothing
			if text == "" {
				text = el.Text
			}
		} else {
			text = el.Text
		}

		text = strings.TrimSpace(text)
		if text != "" {
			parts = append(parts, text)
		}
	}

	result := strings.Join(parts, "\n\n")
	result = strings.TrimSpace(result)

	log.RootLogger().Debugf("text.parseUnstructured: output length=%d chars", len(result))
	return result, nil
}

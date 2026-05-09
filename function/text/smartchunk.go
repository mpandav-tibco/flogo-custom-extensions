package textfn

// smartchunk.go — text.smartChunk() Flogo function
//
// Converts the JSON response from the Unstructured.io REST API into a single
// string of pre-formed, structure-aware chunks separated by blank lines (\n\n).
// The downstream Flogo VectorDB ingestDocuments activity, when configured with
// chunkStrategy="paragraph", splits on \n\n and therefore preserves each chunk
// produced here 1:1 in the vector store.
//
// Why this exists
// ───────────────
// The naive approach (parseUnstructured → paragraph chunker) produces orphaned
// table-cell fragments such as "nonpii | dss" with no surrounding section
// context. These embed poorly and cause "not grounded" hallucination flags
// even when the answer is technically present in the chunk.
//
// smartChunk addresses this by:
//
//  1. **Section tracking** — every chunk is prefixed with the most recent
//     Title element so embeddings carry hierarchical context.
//
//  2. **Tables are atomic** — a Table element never splits across chunks.
//     Tables are emitted in pipe-delimited row form so retrievers can match
//     on full row context (e.g. "Kafka cluster: PIVOTAL | FRANKFURT | …").
//
//  3. **Form-field digest** — 2-column key/value tables are also flattened
//     to a "Field: Value" digest paragraph, which embeds well for queries
//     like "What is the receiver domain type?".
//
//  4. **Prose grouping** — narrative paragraphs accumulate up to ~1500 chars
//     under the same section before being flushed as one chunk.
//
//  5. **Hard size cap** — chunks longer than 6000 chars are split at row /
//     paragraph boundaries to stay below the embedding model context window
//     (the downstream activity also enforces an 8000-char cap as a safety net).
//
// Usage in Flogo mapper:
//
//	=text.smartChunk($activity[UnstructuredExtract].responseBody.data,
//	                 $flow.multipartFormData.filename)

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/project-flogo/core/data"
	"github.com/project-flogo/core/data/coerce"
	"github.com/project-flogo/core/data/expression/function"
	"github.com/project-flogo/core/support/log"
)

func init() {
	_ = function.Register(&fnSmartChunk{})
}

// ── Tunables ────────────────────────────────────────────────────────────────

const (
	// proseTargetSize is the soft cap for accumulated narrative text before a
	// new chunk is flushed. Aligned with typical embedding model windows.
	proseTargetSize = 1500

	// chunkHardCap is the maximum chunk size we emit before the downstream
	// activity's 8000-char safety net kicks in. Keeping our cap lower means
	// our nice formatting (section headers, table rows) survives intact.
	chunkHardCap = 6000

	// chunkSeparator is the delimiter the downstream paragraph chunker splits
	// on. Two newlines (one blank line) is the convention.
	chunkSeparator = "\n\n"

	// maxCellChars caps the rendered length of any single cell value. Very
	// long descriptive cells dilute the row embedding away from the actual
	// answer and rarely contain extra retrievable facts.
	maxCellChars = 200
)

// ── Column heuristics ───────────────────────────────────────────────────────

// reDescriptionHeader matches column headers that typically contain template
// guidance / instruction text rather than retrievable facts. These columns
// pollute embeddings when concatenated into row chunks.
var reDescriptionHeader = regexp.MustCompile(`(?i)^\s*(description|comments?|notes?|guidance|details|remarks?|example|explanation|definition|instructions?|help)\s*$`)

// reDescriptionContent matches phrases that appear in template instruction
// text. Used as a fallback when the header itself doesn't match.
var reDescriptionContent = regexp.MustCompile(`(?i)\b(please|describe|optional|provide an?|should|must|e\.?g\.?|i\.?e\.?|for example|list them|add details|fill in|enter the|specify)\b`)

// reEnvHeader matches environment-tier column headers commonly used in
// multi-environment configuration tables (DEV / SIT / UAT / PRD / QA / GA).
var reEnvHeader = regexp.MustCompile(`(?i)^\s*(dev(elopment)?|sit|uat|prd|prod(uction)?|qa|ga|stg|staging|pre|preprod|test|integration)\s*$`)

// isDescriptionHeader returns true when the header text is a known
// description/instruction column label.
func isDescriptionHeader(h string) bool {
	return reDescriptionHeader.MatchString(h)
}

// isDescriptionColumn returns true when a column appears to contain template
// instruction text rather than facts. The decision uses (a) the header label,
// or (b) heuristics on the column's body cells: long average length AND
// frequent occurrence of guidance phrasing.
func isDescriptionColumn(header string, columnCells []string) bool {
	if isDescriptionHeader(header) {
		return true
	}
	if len(columnCells) < 3 {
		// Too few rows to make a confident heuristic call — keep the column.
		return false
	}
	totalChars := 0
	guidanceHits := 0
	nonEmpty := 0
	for _, c := range columnCells {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		nonEmpty++
		totalChars += len(c)
		if reDescriptionContent.MatchString(c) {
			guidanceHits++
		}
	}
	if nonEmpty == 0 {
		return false
	}
	avgLen := totalChars / nonEmpty
	// Guidance signal: long cells (> 80 chars on average) AND ≥ 40% of cells
	// contain instruction phrasing. Both must hold to avoid false positives
	// on legitimate prose-y data columns (e.g. error message tables).
	return avgLen > 80 && guidanceHits*5 >= nonEmpty*2
}

// isEnvHeader returns true when the header text matches an environment tier.
func isEnvHeader(h string) bool {
	return reEnvHeader.MatchString(h)
}

// columnAt returns all cell values at index col across rows, padding short
// rows with empty strings. Used for column-level heuristics.
func columnAt(rows [][]string, col int) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if col < len(r) {
			out = append(out, r[col])
		} else {
			out = append(out, "")
		}
	}
	return out
}

// dropColumns returns headers and rows with the specified column indices
// removed. The drop list need not be sorted.
func dropColumns(headers []string, rows [][]string, drop map[int]bool) ([]string, [][]string) {
	if len(drop) == 0 {
		return headers, rows
	}
	keep := func(slice []string) []string {
		out := make([]string, 0, len(slice))
		for i, v := range slice {
			if !drop[i] {
				out = append(out, v)
			}
		}
		return out
	}
	newHeaders := keep(headers)
	newRows := make([][]string, 0, len(rows))
	for _, r := range rows {
		newRows = append(newRows, keep(r))
	}
	return newHeaders, newRows
}

// truncateCell trims a single cell value to maxCellChars runes, appending an
// ellipsis when truncation occurs. Operates on runes (not bytes) to avoid
// breaking multi-byte characters.
func truncateCell(s string) string {
	r := []rune(s)
	if len(r) <= maxCellChars {
		return s
	}
	return string(r[:maxCellChars]) + "…"
}

// isEnvironmentMatrix returns true when ≥3 of the column headers (excluding
// the first / "property" column) match an environment tier label. These
// tables benefit from being decomposed into per-environment atomic chunks.
func isEnvironmentMatrix(headers []string) bool {
	if len(headers) < 3 {
		return false
	}
	envCols := 0
	// Skip the first column — it conventionally holds the property label.
	for i := 1; i < len(headers); i++ {
		if isEnvHeader(headers[i]) {
			envCols++
		}
	}
	return envCols >= 2
}

// renderEnvMatrixAtoms decomposes an environment-matrix table into one chunk
// per (property, environment) pair. Each atomic chunk is laser-focused for
// retrieval: "Kafka cluster (DEV): PIVOTAL" embeds far better than a row
// containing all four environments.
//
// The first column of each row is the property name; remaining columns whose
// headers are environment tiers contribute one atom each. Cells with empty
// or placeholder values ("-", "n/a") are skipped.
func renderEnvMatrixAtoms(prefix string, headers []string, rows [][]string) []string {
	var chunks []string
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		property := strings.TrimSpace(row[0])
		if property == "" {
			continue
		}
		for i := 1; i < len(row) && i < len(headers); i++ {
			env := strings.TrimSpace(headers[i])
			if !isEnvHeader(env) {
				continue
			}
			val := strings.TrimSpace(row[i])
			if val == "" || val == "-" || strings.EqualFold(val, "n/a") || strings.EqualFold(val, "na") {
				continue
			}
			val = truncateCell(val)
			body := fmt.Sprintf("%s (%s): %s", property, strings.ToUpper(env), val)
			c := joinChunkParts(prefix, body)
			if c != "" {
				chunks = append(chunks, c)
			}
		}
	}
	return chunks
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// docTitleFromFilename strips the directory path and extension from a filename
// to produce a short, human-readable document title for chunk prefixes.
func docTitleFromFilename(name string) string {
	if name == "" {
		return ""
	}
	base := filepath.Base(name)
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return strings.TrimSpace(base)
}

// formatHeader returns the standard "[Document: …] [Section: …]" prefix.
// Either argument may be empty; empty fields are omitted from the prefix.
func formatHeader(docTitle, section string) string {
	docTitle = strings.TrimSpace(docTitle)
	section = strings.TrimSpace(section)
	switch {
	case docTitle != "" && section != "":
		return fmt.Sprintf("[Document: %s] [Section: %s]", docTitle, section)
	case docTitle != "":
		return fmt.Sprintf("[Document: %s]", docTitle)
	case section != "":
		return fmt.Sprintf("[Section: %s]", section)
	default:
		return ""
	}
}

// joinChunkParts joins a header line with the chunk body, handling empty
// headers cleanly (no leading blank line) and trimming trailing whitespace.
func joinChunkParts(header, body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	if header == "" {
		return body
	}
	return header + "\n" + body
}

// ── Table-shape detection ───────────────────────────────────────────────────

// extractRows returns a slice of row-cell-arrays from <tbody>; falls back to
// any direct <tr> children of the table when no explicit <tbody> exists.
func extractRows(html string) [][]string {
	tbody := html
	if m := reTBODY.FindStringSubmatch(html); len(m) > 1 {
		tbody = m[1]
	}
	trMatches := reTR.FindAllStringSubmatch(tbody, -1)
	rows := make([][]string, 0, len(trMatches))
	for _, trm := range trMatches {
		if len(trm) < 2 {
			continue
		}
		cells := extractCells(trm[1], false)
		if len(cells) == 0 {
			continue
		}
		rows = append(rows, cells)
	}
	return rows
}

// extractHeaders mirrors the logic in tableHTMLToText: take the LAST <tr> in
// <thead> as the most-specific header row, falling back to nil if absent.
func extractHeaders(html string) []string {
	m := reTHEAD.FindStringSubmatch(html)
	if len(m) <= 1 {
		return nil
	}
	thead := m[1]
	trMatches := reTR.FindAllStringSubmatch(thead, -1)
	var headers []string
	for _, trm := range trMatches {
		if len(trm) < 2 {
			continue
		}
		cells := extractCells(trm[1], true)
		if len(cells) > 0 {
			headers = cells
		}
	}
	return headers
}

// formatTableRow renders one row as "Header1: val1 | Header2: val2 | …",
// dropping empty cells and capping each cell to maxCellChars runes. When
// headers is nil/short the cells are joined with " | " as a fallback.
func formatTableRow(headers, cells []string) string {
	var parts []string
	for i, c := range cells {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		c = truncateCell(c)
		if i < len(headers) && strings.TrimSpace(headers[i]) != "" {
			parts = append(parts, strings.TrimSpace(headers[i])+": "+c)
		} else {
			parts = append(parts, c)
		}
	}
	return strings.Join(parts, " | ")
}

// renderTableChunks produces one or more chunk-bodies for a Table element.
//
//	header (string) — descriptive caption for the table (section / nearby Title)
//	rows  ([]string) — formatted body rows (already labelled with headers)
//
// The output is split into multiple chunks if the cumulative body would
// exceed chunkHardCap, with each continuation chunk repeating the headers
// line so retrieval still hits.
func renderTableChunks(prefix, headerLine string, rows []string) []string {
	if len(rows) == 0 {
		return nil
	}
	bodyHeader := strings.TrimSpace(headerLine)

	var chunks []string
	var buf strings.Builder

	flush := func(continuation bool) {
		body := strings.TrimSpace(buf.String())
		if body == "" {
			return
		}
		var hdr string
		if continuation {
			hdr = "Table (continued):"
		} else {
			hdr = "Table:"
		}
		// Compose: [prefix]\nTable: / Table (continued):\n<bodyHeader>\n<rows>
		var parts []string
		if prefix != "" {
			parts = append(parts, prefix)
		}
		parts = append(parts, hdr)
		if bodyHeader != "" {
			parts = append(parts, "Columns: "+bodyHeader)
		}
		parts = append(parts, body)
		chunks = append(chunks, strings.Join(parts, "\n"))
		buf.Reset()
	}

	first := true
	for _, row := range rows {
		row = strings.TrimSpace(row)
		if row == "" {
			continue
		}
		// +1 for the joining newline; +bodyHeader prefix counted once per chunk
		projected := buf.Len() + len(row) + 1
		if !first {
			projected += len(prefix) + len(bodyHeader) + 32 // headers + "Table:" overhead
		}
		if buf.Len() > 0 && projected > chunkHardCap {
			flush(!first)
			first = false
		}
		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(row)
	}
	flush(!first)
	return chunks
}

// renderFormDigest produces a "Form fields:" digest chunk for 2-column
// key/value tables, which is what most form-style PDFs (interface specs)
// contain. Returns "" if the table is not 2-column or yields no usable rows.
//
// Phase 1.5 note: when the caller has already dropped description columns
// (via dropColumns) a 3-column form table reduces to 2 columns and this
// function picks it up automatically.
func renderFormDigest(prefix string, rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	// Verify ≥75% of rows are exactly 2 cells; otherwise this is not a
	// key/value table and we should let the row renderer handle it.
	twoCol := 0
	for _, r := range rows {
		if len(r) == 2 {
			twoCol++
		}
	}
	if twoCol*4 < len(rows)*3 {
		return ""
	}

	var lines []string
	if prefix != "" {
		lines = append(lines, prefix)
	}
	lines = append(lines, "Form fields:")
	for _, r := range rows {
		if len(r) < 2 {
			continue
		}
		k := strings.TrimSpace(r[0])
		v := strings.TrimSpace(r[1])
		if k == "" || v == "" {
			continue
		}
		// Truncate runaway values to keep the digest scannable.
		// Rune-safe truncation at 400 chars.
		if rv := []rune(v); len(rv) > 400 {
			v = string(rv[:400]) + "…"
		}
		lines = append(lines, "- "+k+": "+v)
	}
	if len(lines) <= 2 {
		// Only header lines, no actual fields — skip.
		return ""
	}
	out := strings.Join(lines, "\n")
	if len(out) > chunkHardCap {
		out = out[:chunkHardCap]
	}
	return out
}

// ── Flogo function registration ────────────────────────────────────────────

type fnSmartChunk struct{}

func (fnSmartChunk) Name() string        { return "smartChunk" }
func (fnSmartChunk) GetCategory() string { return "text" }

func (fnSmartChunk) Sig() (paramTypes []data.Type, isVariadic bool) {
	return []data.Type{data.TypeString, data.TypeString}, false
}

// Eval converts the Unstructured.io JSON response into structure-aware chunks
// joined by blank lines. The second argument is the source filename, used to
// build a "[Document: …]" prefix on every chunk.
func (fnSmartChunk) Eval(params ...interface{}) (interface{}, error) {
	if len(params) < 1 {
		return nil, fmt.Errorf("text.smartChunk: requires (jsonResponse, filename)")
	}
	raw, err := coerce.ToString(params[0])
	if err != nil {
		return nil, fmt.Errorf("text.smartChunk: jsonResponse must be a string, got %T", params[0])
	}
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}

	filename := ""
	if len(params) >= 2 {
		filename, _ = coerce.ToString(params[1])
	}
	docTitle := docTitleFromFilename(filename)

	var elements []unstructuredElement
	if err := json.Unmarshal([]byte(raw), &elements); err != nil {
		return nil, fmt.Errorf("text.smartChunk: invalid JSON from Unstructured.io API: %w", err)
	}

	log.RootLogger().Debugf("text.smartChunk: parsing %d elements (doc=%q)", len(elements), docTitle)

	var chunks []string

	// Section state: most recent Title element.
	currentSection := ""

	// Prose buffer: accumulated narrative text under the current section.
	var prose strings.Builder
	flushProse := func() {
		body := strings.TrimSpace(prose.String())
		prose.Reset()
		if body == "" {
			return
		}
		header := formatHeader(docTitle, currentSection)
		c := joinChunkParts(header, body)
		if c != "" {
			chunks = append(chunks, c)
		}
	}

	addProse := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		// Flush before appending if this paragraph would push us over the cap.
		if prose.Len() > 0 && prose.Len()+len(text)+1 > proseTargetSize {
			flushProse()
		}
		if prose.Len() > 0 {
			prose.WriteByte('\n')
		}
		prose.WriteString(text)
	}

	for _, el := range elements {
		if skipElementType[el.Type] {
			continue
		}

		text := strings.TrimSpace(el.Text)

		switch el.Type {
		case "Title":
			// Section change: flush prose under the previous section first.
			flushProse()
			if text != "" {
				currentSection = text
			}
			// Titles also count as searchable content — include as a tiny chunk
			// so a query like "where is the Kafka section?" can hit the heading.
			if text != "" {
				header := formatHeader(docTitle, "")
				c := joinChunkParts(header, "Section heading: "+text)
				if c != "" {
					chunks = append(chunks, c)
				}
			}

		case "Table":
			// Tables break the prose stream — flush whatever's pending first.
			flushProse()

			html, _ := el.Metadata["text_as_html"].(string)
			prefix := formatHeader(docTitle, currentSection)

			if html != "" {
				headers := extractHeaders(html)
				rawRows := extractRows(html)

				// Phase 1.5: drop description / instruction columns. These
				// add 100-500 chars of template guidance per row that
				// pollute the embedding without contributing facts.
				if len(headers) > 0 {
					dropSet := make(map[int]bool)
					for i, h := range headers {
						col := columnAt(rawRows, i)
						if isDescriptionColumn(h, col) {
							dropSet[i] = true
						}
					}
					if len(dropSet) > 0 {
						headers, rawRows = dropColumns(headers, rawRows, dropSet)
					}
				}

				// Build header line "H1 | H2 | H3" for context (post-drop).
				var headerLine string
				if len(headers) > 0 {
					headerLine = strings.Join(headers, " | ")
				}

				// Phase 1.5: environment-matrix decomposition. When ≥3
				// columns are environment tiers, emit one atomic chunk per
				// (property, environment) pair — high precision retrieval.
				if isEnvironmentMatrix(headers) {
					atoms := renderEnvMatrixAtoms(prefix, headers, rawRows)
					chunks = append(chunks, atoms...)
					// Skip row rendering for matrix tables — atoms are
					// authoritative; row chunks would be redundant noise.
					continue
				}

				// 1) Form-field digest (if 2-column key/value).
				if digest := renderFormDigest(prefix, rawRows); digest != "" {
					chunks = append(chunks, digest)
				}

				// 2) Row-by-row formatted body (preserves column context).
				rowLines := make([]string, 0, len(rawRows))
				for _, r := range rawRows {
					line := formatTableRow(headers, r)
					if line != "" {
						rowLines = append(rowLines, line)
					}
				}
				tableChunks := renderTableChunks(prefix, headerLine, rowLines)
				chunks = append(chunks, tableChunks...)

				// If we managed to render any structured form, skip the raw text.
				if len(rowLines) > 0 || len(rawRows) > 0 {
					continue
				}
			}

			// HTML missing or unparseable — fall back to the plain text.
			if text != "" {
				body := "Table:\n" + text
				if len(body) > chunkHardCap {
					body = body[:chunkHardCap]
				}
				c := joinChunkParts(prefix, body)
				if c != "" {
					chunks = append(chunks, c)
				}
			}

		case "ListItem":
			// Bullets stay with their surrounding prose.
			addProse("• " + text)

		default:
			// NarrativeText, Text, Address, EmailAddress, etc.
			addProse(text)
		}
	}
	flushProse()

	// De-duplicate exact repeats (some PDFs emit duplicate Title elements).
	chunks = dedupeChunks(chunks)

	out := strings.Join(chunks, chunkSeparator)
	log.RootLogger().Debugf("text.smartChunk: emitted %d chunks, total %d chars", len(chunks), len(out))
	return out, nil
}

// dedupeChunks drops exact duplicate chunks while preserving order.
func dedupeChunks(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, c := range in {
		key := strings.TrimSpace(c)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, c)
	}
	return out
}

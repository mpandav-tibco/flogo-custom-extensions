package textfn

// cleantext.go — text.cleanText() Flogo function
//
// Removes Apache Tika text-extraction artifacts that degrade embedding quality:
//
//  1. HTML entities  — &amp; → &, &nbsp; → space, &#NNN; null/control → removed
//     (Tika leaks HTML entities when extracting MHTML/PDF with embedded HTML)
//  2. [image: ...] alt-text — MHTML/Word checkbox images (e.g. [image: (tick)])
//  3. Null + control characters — \x00–\x08, \x0B–\x1F (except \t, \n)
//  4. Line-level whitespace trim — strips leading/trailing spaces per line
//  5. Fragment joining — short orphaned lines (≤20 chars) separated only by blank
//     lines are joined with a space; fixes "BC\n\n7.5.0" → "BC 7.5.0" caused by
//     Tika extracting multi-cell table rows as separate paragraphs
//  6. Excessive blank lines — 3+ consecutive newlines → single blank line
//
// These are format-agnostic transformations applied to the plain-text string
// returned by Tika regardless of original file type (PDF, DOC, DOCX, etc.).
// Empty strings (JPEG/MOV — no text in Tika output) are returned unchanged.
//
// Usage in Flogo mapper:
//
//	=text.cleanText($activity[TikaExtract].responseBody.data)

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/project-flogo/core/data"
	"github.com/project-flogo/core/data/coerce"
	"github.com/project-flogo/core/data/expression/function"
	"github.com/project-flogo/core/support/log"
)

func init() {
	_ = function.Register(&fnCleanText{})
}

// reImageAlt matches Tika image alt-text tokens: [image: (tick)], [image: (cross)], etc.
var reImageAlt = regexp.MustCompile(`\[image:[^\]]*\]`)

// reExcessiveBlank matches 3 or more consecutive newlines (blank lines)
var reExcessiveBlank = regexp.MustCompile(`\n{3,}`)

// reHtmlNamedEntity matches common HTML named entities Tika leaks into plain text
var reHtmlNamedEntity = regexp.MustCompile(`&(amp|lt|gt|quot|apos|nbsp);`)

// reHtmlNumericEntity matches numeric HTML entities &#NNN; or &#xHHH;
var reHtmlNumericEntity = regexp.MustCompile(`&#[xX]?[0-9a-fA-F]+;`)

// fragmentMaxLen is the maximum byte length for a line to be considered a
// table-cell fragment eligible for joining with an adjacent fragment.
const fragmentMaxLen = 20

type fnCleanText struct{}

func (fnCleanText) Name() string        { return "cleanText" }
func (fnCleanText) GetCategory() string { return "text" }

func (fnCleanText) Sig() (paramTypes []data.Type, isVariadic bool) {
	return []data.Type{data.TypeString}, false
}

// Eval removes Tika extraction artifacts from a plain-text string.
// Steps are applied in order — see package comment for details.
func (fnCleanText) Eval(params ...interface{}) (interface{}, error) {
	text, err := coerce.ToString(params[0])
	if err != nil {
		return nil, fmt.Errorf("text.cleanText: parameter must be a string, got %T", params[0])
	}

	if text == "" {
		return "", nil
	}

	log.RootLogger().Debugf("text.cleanText: input length=%d chars", len(text))

	// Step 1: decode HTML named entities (&amp; &nbsp; &lt; &gt; etc.)
	text = reHtmlNamedEntity.ReplaceAllStringFunc(text, func(m string) string {
		switch m {
		case "&amp;":
			return "&"
		case "&lt;":
			return "<"
		case "&gt;":
			return ">"
		case "&quot;":
			return "\""
		case "&apos;":
			return "'"
		case "&nbsp;":
			return " "
		}
		return m
	})

	// Step 2: decode/remove numeric HTML entities
	// Printable ASCII/Unicode codepoints are kept; null and control chars removed.
	text = reHtmlNumericEntity.ReplaceAllStringFunc(text, func(m string) string {
		// strip &# and ; to get the raw value — not needed for embedding quality,
		// just remove the entity entirely to avoid noise like "&#0;"
		return ""
	})

	// Step 3: remove null bytes and non-printable control characters
	// Keep: \t (0x09), \n (0x0A), \r (0x0D), and all printable runes
	text = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return r
		}
		if unicode.IsControl(r) {
			return -1 // drop
		}
		return r
	}, text)

	// Step 4: remove [image: ...] alt-text tokens
	text = reImageAlt.ReplaceAllString(text, "")

	// Step 5: trim leading/trailing whitespace from every line
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}

	// Step 6: join orphaned short fragments separated by a single blank line.
	// Table cells extracted positionally by Tika often appear as:
	//   "BC\n\n7.5.0\n\nBW\n\n6.8.1"  →  "BC 7.5.0\nBW 6.8.1"
	// Rule: if line[i] and line[i+2] are both non-blank and ≤ fragmentMaxLen
	// bytes, and line[i+1] is blank, merge them into one line.
	joined := make([]string, 0, len(lines))
	i := 0
	for i < len(lines) {
		cur := lines[i]
		// Look ahead: current short, next blank, after-next short → join
		if len(cur) > 0 && len(cur) <= fragmentMaxLen &&
			i+2 < len(lines) && lines[i+1] == "" &&
			len(lines[i+2]) > 0 && len(lines[i+2]) <= fragmentMaxLen {
			joined = append(joined, cur+" "+lines[i+2])
			i += 3 // consumed cur, blank, next-fragment
			continue
		}
		joined = append(joined, cur)
		i++
	}

	text = strings.Join(joined, "\n")

	// Step 7: collapse 3+ consecutive blank lines into one blank line
	text = reExcessiveBlank.ReplaceAllString(text, "\n\n")

	// Step 8: trim overall result
	text = strings.TrimSpace(text)

	log.RootLogger().Debugf("text.cleanText: output length=%d chars", len(text))
	return text, nil
}

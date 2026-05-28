package ingestDocuments

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── postProcessPDFText ────────────────────────────────────────────────────────

func TestPostProcessPDFText_HyphenRejoin(t *testing.T) {
	input := "The configu-\nration system handles all set-\ntings gracefully."
	got := postProcessPDFText(input)
	assert.Contains(t, got, "configuration", "soft hyphen should be rejoined")
	assert.Contains(t, got, "settings", "soft hyphen should be rejoined")
	assert.NotContains(t, got, "configu-", "hyphen-break should be removed")
}

func TestPostProcessPDFText_BlankLineCollapse(t *testing.T) {
	input := "First\n\n\n\n\nSecond"
	got := postProcessPDFText(input)
	assert.NotContains(t, got, "\n\n\n", "3+ blank lines should collapse to 2")
	assert.Contains(t, got, "First")
	assert.Contains(t, got, "Second")
}

// ── isLikelyPDFHeading ────────────────────────────────────────────────────────

func TestIsLikelyPDFHeading_NumberedSection(t *testing.T) {
	lines := []string{"1.2 Installation Guide", "some body text"}
	assert.True(t, isLikelyPDFHeading("1.2 Installation Guide", lines, 0))
	assert.True(t, isLikelyPDFHeading("Chapter 3 Configuration", lines, 0))
	assert.True(t, isLikelyPDFHeading("Appendix A Reference", lines, 0))
}

func TestIsLikelyPDFHeading_AllCaps(t *testing.T) {
	lines := []string{"GETTING STARTED", "body text follows"}
	assert.True(t, isLikelyPDFHeading("GETTING STARTED", lines, 0))
	assert.True(t, isLikelyPDFHeading("CONFIGURATION OPTIONS", lines, 0))
	// Page number should NOT be treated as heading
	assert.False(t, isLikelyPDFHeading("352", lines, 0))
	assert.False(t, isLikelyPDFHeading("352 |", lines, 0))
}

func TestIsLikelyPDFHeading_TitleCaseIsolated(t *testing.T) {
	lines := []string{"", "Getting Started With Flogo", ""}
	assert.True(t, isLikelyPDFHeading("Getting Started With Flogo", lines, 1),
		"title-case line between blank lines should be a heading")
}

func TestIsLikelyPDFHeading_NegativeCases(t *testing.T) {
	lines := []string{"This is a normal paragraph sentence that goes on for a while.", "next"}
	// Too long
	assert.False(t, isLikelyPDFHeading(strings.Repeat("X", 101), lines, 0))
	// Regular prose — not title-case isolated
	assert.False(t, isLikelyPDFHeading("This is a normal paragraph sentence.", lines, 0))
	// Already has ##
	assert.False(t, isLikelyPDFHeading("## Already Marked", lines, 0))
	// Empty
	assert.False(t, isLikelyPDFHeading("", lines, 0))
}

// ── isTitleCaseLine ───────────────────────────────────────────────────────────

func TestIsTitleCaseLine(t *testing.T) {
	assert.True(t, isTitleCaseLine("Getting Started With Flogo"))
	assert.True(t, isTitleCaseLine("Advanced Configuration Options"))
	// Articles after first word are exempt
	assert.True(t, isTitleCaseLine("Design of the System"))
	// Lower-case first word → false
	assert.False(t, isTitleCaseLine("getting started"))
	// Single word → false (needs ≥2 words)
	assert.False(t, isTitleCaseLine("Introduction"))
	// Too many words (13 > 12)
	assert.False(t, isTitleCaseLine("One Two Three Four Five Six Seven Eight Nine Ten Eleven Twelve Thirteen"))
}

// ── stripPageHeadersFooters ───────────────────────────────────────────────────

func TestStripPageHeadersFooters_RemovesRepeatingLines(t *testing.T) {
	header := "TIBCO Flogo® User Guide"
	pages := make([]string, 10)
	for i := range pages {
		pages[i] = header + "\n352 |\nActual content for page " + string(rune('A'+i))
	}
	cleaned := stripPageHeadersFooters(pages)
	for i, p := range cleaned {
		assert.NotContains(t, p, header, "page %d: boilerplate header should be stripped", i)
		assert.Contains(t, p, "Actual content", "page %d: real content should remain", i)
	}
}

func TestStripPageHeadersFooters_KeepsUniqueLines(t *testing.T) {
	pages := []string{
		"Introduction\nWelcome to the guide.",
		"Chapter 1\nThis is the first chapter.",
		"Chapter 2\nThis is the second chapter.",
	}
	cleaned := stripPageHeadersFooters(pages)
	// Only 3 pages — nothing should be stripped (threshold = max(2, 0) = 2,
	// no line appears in first/last 3 of ≥2 pages here).
	for i, p := range cleaned {
		assert.Equal(t, pages[i], p, "unique lines should not be stripped")
	}
}

// ── ExtractTextFromBytes — text passthrough ───────────────────────────────────

func TestExtractTextFromBytes_TXT(t *testing.T) {
	input := "Hello, World!\nSecond line."
	got, err := ExtractTextFromBytes([]byte(input), "readme.txt")
	require.NoError(t, err)
	assert.Equal(t, input, got)
}

func TestExtractTextFromBytes_MD(t *testing.T) {
	input := "## Heading\n\nSome paragraph text."
	got, err := ExtractTextFromBytes([]byte(input), "notes.md")
	require.NoError(t, err)
	assert.Equal(t, input, got)
}

func TestExtractTextFromBytes_UnsupportedBinary(t *testing.T) {
	binary := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05}
	_, err := ExtractTextFromBytes(binary, "archive.zip")
	assert.Error(t, err, "binary file with unknown extension should error")
}

func TestExtractTextFromBytes_LegacyDoc(t *testing.T) {
	_, err := ExtractTextFromBytes([]byte("dummy"), "report.doc")
	assert.Error(t, err, ".doc should return an unsupported error")
	assert.Contains(t, err.Error(), ".doc")
}

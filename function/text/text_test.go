package textfn

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanText(t *testing.T) {
	fn := &fnCleanText{}

	// --- original behaviours ---

	t.Run("removes image alt-text", func(t *testing.T) {
		out, err := fn.Eval("[image: (tick)]\nBC 7.5.0\n[image: (cross)]")
		require.NoError(t, err)
		assert.Equal(t, "BC 7.5.0", out)
	})
	t.Run("collapses excessive blank lines", func(t *testing.T) {
		out, err := fn.Eval("line1\n\n\n\n\nline2")
		require.NoError(t, err)
		assert.Equal(t, "line1\n\nline2", out)
	})
	t.Run("trims line whitespace", func(t *testing.T) {
		out, err := fn.Eval("   BC 7.5.0   \n   Compatible   ")
		require.NoError(t, err)
		assert.Equal(t, "BC 7.5.0\nCompatible", out)
	})
	t.Run("empty string returns empty", func(t *testing.T) {
		out, err := fn.Eval("")
		require.NoError(t, err)
		assert.Equal(t, "", out)
	})
	t.Run("clean text passes through unchanged", func(t *testing.T) {
		out, err := fn.Eval("BC 7.5.0 is compatible with RV 5.0")
		require.NoError(t, err)
		assert.Equal(t, "BC 7.5.0 is compatible with RV 5.0", out)
	})

	// --- HTML entity decoding ---

	t.Run("decodes &amp;", func(t *testing.T) {
		out, err := fn.Eval("BC &amp; Protocols")
		require.NoError(t, err)
		assert.Equal(t, "BC & Protocols", out)
	})
	t.Run("decodes &nbsp; to space", func(t *testing.T) {
		out, err := fn.Eval("BC&nbsp;7.5.0")
		require.NoError(t, err)
		assert.Equal(t, "BC 7.5.0", out)
	})
	t.Run("removes numeric HTML entity &#0;", func(t *testing.T) {
		out, err := fn.Eval("BC&#0;7.5.0")
		require.NoError(t, err)
		assert.Equal(t, "BC7.5.0", out)
	})
	t.Run("decodes &lt; and &gt;", func(t *testing.T) {
		out, err := fn.Eval("version &lt;= 6.0.0")
		require.NoError(t, err)
		assert.Equal(t, "version <= 6.0.0", out)
	})

	// --- control character removal ---

	t.Run("removes null bytes", func(t *testing.T) {
		out, err := fn.Eval("BC\x007.5.0")
		require.NoError(t, err)
		assert.Equal(t, "BC7.5.0", out)
	})

	// --- fragment joining (table cell split-across-lines fix) ---

	t.Run("joins split version BC\\n\\n7.5.0", func(t *testing.T) {
		// Tika extracts "BC" and "7.5.0" as separate paragraphs from table cells
		out, err := fn.Eval("BC\n\n7.5.0")
		require.NoError(t, err)
		assert.Equal(t, "BC 7.5.0", out)
	})
	t.Run("joins multiple split version pairs", func(t *testing.T) {
		out, err := fn.Eval("BC\n\n7.5.0\nBW\n\n6.8.1")
		require.NoError(t, err)
		assert.Equal(t, "BC 7.5.0\nBW 6.8.1", out)
	})
	t.Run("does not join long lines", func(t *testing.T) {
		// Lines over fragmentMaxLen (20 chars) must NOT be joined — they are full sentences
		long := "BC Compatibility with RV"
		out, err := fn.Eval(long + "\n\nsome other long sentence here")
		require.NoError(t, err)
		assert.Equal(t, long+"\n\nsome other long sentence here", out)
	})
	t.Run("real BC doc pattern: image+split versions", func(t *testing.T) {
		input := "                BC 7.5.0\n[image: (tick)]\n\n\n\n\n                BC\n\n7.4.0\n[image: (tick)]"
		out, err := fn.Eval(input)
		require.NoError(t, err)
		// image removed, whitespace trimmed, split version joined, blanks collapsed
		assert.Equal(t, "BC 7.5.0\n\nBC 7.4.0", out)
	})

	// --- URL and email noise removal ---

	t.Run("removes standalone URL lines", func(t *testing.T) {
		// Lines > 20 chars so fragment joiner does not collapse them
		input := "API Authentication Details\nhttps://confluence.tools.3stripes.net/display/SomePage\nDetailed system requirements"
		out, err := fn.Eval(input)
		require.NoError(t, err)
		assert.Equal(t, "API Authentication Details\n\nDetailed system requirements", out)
	})
	t.Run("keeps inline URL with surrounding text", func(t *testing.T) {
		input := "Dev URL: https://cpai-productapi.azurewebsites.net/api/adidasdev/client"
		out, err := fn.Eval(input)
		require.NoError(t, err)
		assert.Equal(t, "Dev URL: https://cpai-productapi.azurewebsites.net/api/adidasdev/client", out)
	})
	t.Run("removes mailto standalone URL", func(t *testing.T) {
		// Lines > 20 chars so fragment joiner does not collapse them
		input := "For support, contact us:\nmailto:support@adidas-group.com\nPlease raise an ASPEN ticket"
		out, err := fn.Eval(input)
		require.NoError(t, err)
		assert.Equal(t, "For support, contact us:\n\nPlease raise an ASPEN ticket", out)
	})
	t.Run("replaces email addresses with placeholder", func(t *testing.T) {
		input := "Contact Tome@adidas.com for support"
		out, err := fn.Eval(input)
		require.NoError(t, err)
		assert.Equal(t, "Contact [email] for support", out)
	})
	t.Run("removes multiple standalone URL lines", func(t *testing.T) {
		// Long surrounding lines prevent fragment joining
		input := "Change Log Documentation\nhttps://confluence.tools.3stripes.net/display/~Mario\nhttps://jira.tools.3stripes.net/browse/T4MGT-17486\nDetailed integration diagram"
		out, err := fn.Eval(input)
		require.NoError(t, err)
		assert.Equal(t, "Change Log Documentation\n\nDetailed integration diagram", out)
	})
	t.Run("replaces partial email without TLD", func(t *testing.T) {
		// Tika wraps "Rathore@adidas.com" as "Rathore@adidas" + ".com" on next line
		input := "Contact Rathore@adidas for details"
		out, err := fn.Eval(input)
		require.NoError(t, err)
		assert.Equal(t, "Contact [email] for details", out)
	})
	t.Run("removes URL path continuation lines", func(t *testing.T) {
		// Tika wraps a long Confluence URL; the https:// line is stripped, leaving path fragments.
		// Two fragment lines removed → 3 consecutive newlines → collapsed to one blank line.
		input := "Change Log\nnet/display/~pen-siri\n/display/~NiteshKumarRathore\nInterface Owner"
		out, err := fn.Eval(input)
		require.NoError(t, err)
		assert.Equal(t, "Change Log\n\nInterface Owner", out)
	})
	t.Run("removes bare TLD line", func(t *testing.T) {
		// Tika splits "something.com" across two lines; URL stripping removes the first leaving just ".com"
		input := "API endpoint description\n.com\nMore documentation text here"
		out, err := fn.Eval(input)
		require.NoError(t, err)
		assert.Equal(t, "API endpoint description\n\nMore documentation text here", out)
	})
	t.Run("removes bare TLD word without dot prefix", func(t *testing.T) {
		// Email regex strips "user@adidas-group." (trailing dot consumed), leaving "com" on next line.
		// Fragment joiner would combine "[email]" + "com" into "[email] com" without this fix.
		input := "Functional Mailbox\nintegration-mailbox@adidas-group.\ncom\nMore content here with longer text"
		out, err := fn.Eval(input)
		require.NoError(t, err)
		assert.NotContains(t, out.(string), "com")
		assert.Contains(t, out.(string), "[email]")
	})
}

// ─── text.parseUnstructured tests ────────────────────────────────────────────

func TestParseUnstructured(t *testing.T) {
	fn := &fnParseUnstructured{}

	t.Run("empty string returns empty", func(t *testing.T) {
		out, err := fn.Eval("")
		require.NoError(t, err)
		assert.Equal(t, "", out)
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		_, err := fn.Eval("not json")
		assert.Error(t, err)
	})

	t.Run("plain text elements joined with blank lines", func(t *testing.T) {
		input := `[
			{"type":"Title","text":"My Spec","metadata":{}},
			{"type":"NarrativeText","text":"This is a paragraph.","metadata":{}}
		]`
		out, err := fn.Eval(input)
		require.NoError(t, err)
		assert.Equal(t, "My Spec\n\nThis is a paragraph.", out)
	})

	t.Run("short elements under minTextLen are filtered out", func(t *testing.T) {
		input := `[
			{"type":"UncategorizedText","text":"sender system","metadata":{}},
			{"type":"UncategorizedText","text":"eu-central-1","metadata":{}},
			{"type":"NarrativeText","text":"The non-technical name of the data","metadata":{}},
			{"type":"NarrativeText","text":"This is a substantive sentence that contains enough content to pass.","metadata":{}}
		]`
		out, err := fn.Eval(input)
		require.NoError(t, err)
		outStr := out.(string)
		// All non-empty elements are retained (no length filter)
		assert.Contains(t, outStr, "sender system")
		assert.Contains(t, outStr, "eu-central-1")
		assert.Contains(t, outStr, "non-technical name")
		assert.Contains(t, outStr, "substantive sentence")
	})

	t.Run("Footer and Image elements are skipped", func(t *testing.T) {
		input := `[
			{"type":"Title","text":"Section","metadata":{}},
			{"type":"Footer","text":"Page 1 of 10","metadata":{}},
			{"type":"Image","text":"","metadata":{}},
			{"type":"NarrativeText","text":"Content here.","metadata":{}}
		]`
		out, err := fn.Eval(input)
		require.NoError(t, err)
		assert.NotContains(t, out.(string), "Page 1 of 10")
		assert.Contains(t, out.(string), "Content here.")
	})

	t.Run("table with headers reconstructed as labelled rows", func(t *testing.T) {
		html := `<table><thead><tr><th>Interface Owner</th><th>Value</th></tr></thead>` +
			`<tbody><tr><td>Product Team</td><td>Risk &amp; Audit</td></tr>` +
			`<tr><td>Project Milestone</td><td>N/A</td></tr></tbody></table>`
		input := `[{"type":"Table","text":"Interface Owner Value","metadata":{"text_as_html":"` +
			strings.ReplaceAll(html, `"`, `\"`) + `"}}]`
		out, err := fn.Eval(input)
		require.NoError(t, err)
		outStr := out.(string)
		assert.Contains(t, outStr, "Interface Owner: Product Team")
		assert.Contains(t, outStr, "Value: Risk & Audit")
		assert.Contains(t, outStr, "Interface Owner: Project Milestone")
		assert.Contains(t, outStr, "Value: N/A")
	})

	t.Run("table without headers joins cells with pipe", func(t *testing.T) {
		html := `<table><tbody><tr><td>DEV</td><td>SIT</td><td>PROD</td></tr></tbody></table>`
		input := `[{"type":"Table","text":"DEV SIT PROD","metadata":{"text_as_html":"` +
			strings.ReplaceAll(html, `"`, `\"`) + `"}}]`
		out, err := fn.Eval(input)
		require.NoError(t, err)
		outStr := out.(string)
		assert.Contains(t, outStr, "DEV")
		assert.Contains(t, outStr, "SIT")
		assert.Contains(t, outStr, "PROD")
	})

	t.Run("table with multi-row header uses last header row", func(t *testing.T) {
		// Simulates DEV/SIT/UAT/PROD env table with 2-row header
		html := `<table><thead>` +
			`<tr><th></th><th colspan="3">Environment</th></tr>` +
			`<tr><th></th><th>Development</th><th>Quality/Staging</th><th>Production</th></tr>` +
			`</thead><tbody>` +
			`<tr><td>EMS Queue</td><td>dev.queue</td><td>sit.queue</td><td>prod.queue</td></tr>` +
			`</tbody></table>`
		input := `[{"type":"Table","text":"EMS Queue dev.queue sit.queue prod.queue","metadata":{"text_as_html":"` +
			strings.ReplaceAll(html, `"`, `\"`) + `"}}]`
		out, err := fn.Eval(input)
		require.NoError(t, err)
		outStr := out.(string)
		// Last header row: "", "Development", "Quality/Staging", "Production"
		assert.Contains(t, outStr, "Development: dev.queue")
		assert.Contains(t, outStr, "Quality/Staging: sit.queue")
		assert.Contains(t, outStr, "Production: prod.queue")
	})

	t.Run("table falls back to plain text when html is empty", func(t *testing.T) {
		input := `[{"type":"Table","text":"fallback text","metadata":{"text_as_html":""}}]`
		out, err := fn.Eval(input)
		require.NoError(t, err)
		assert.Equal(t, "fallback text", out)
	})

	t.Run("html entities in table cells decoded", func(t *testing.T) {
		html := `<table><tbody><tr><td>Risk &amp; Audit</td><td>&lt;field&gt;</td></tr></tbody></table>`
		input := `[{"type":"Table","text":"Risk Audit field","metadata":{"text_as_html":"` +
			strings.ReplaceAll(html, `"`, `\"`) + `"}}]`
		out, err := fn.Eval(input)
		require.NoError(t, err)
		outStr := out.(string)
		assert.Contains(t, outStr, "Risk & Audit")
		assert.Contains(t, outStr, "<field>")
	})
}

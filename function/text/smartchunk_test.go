package textfn

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSmartChunk_EmptyInput(t *testing.T) {
	fn := &fnSmartChunk{}
	out, err := fn.Eval("", "x.pdf")
	require.NoError(t, err)
	assert.Equal(t, "", out)
}

func TestSmartChunk_BadJSON(t *testing.T) {
	fn := &fnSmartChunk{}
	_, err := fn.Eval("not-json", "x.pdf")
	require.Error(t, err)
}

func TestSmartChunk_TitleAndProseGetSectionHeader(t *testing.T) {
	fn := &fnSmartChunk{}
	in := `[
		{"type":"Title","text":"Kafka Configuration","metadata":{}},
		{"type":"NarrativeText","text":"The producer publishes events to a topic.","metadata":{}}
	]`
	out, err := fn.Eval(in, "spec.pdf")
	require.NoError(t, err)
	assert.Contains(t, out, "[Document: spec]")
	assert.Contains(t, out, "[Section: Kafka Configuration]")
	assert.Contains(t, out, "The producer publishes events to a topic.")
}

func TestSmartChunk_TableRowsKeepColumnContext(t *testing.T) {
	fn := &fnSmartChunk{}
	// Non-environment headers so the generic row renderer runs (the env-matrix
	// path is exercised in TestSmartChunk_EnvironmentMatrixEmitsAtomicChunks).
	html := `<table>
		<thead><tr><th>Component</th><th>Owner</th><th>Status</th></tr></thead>
		<tbody>
			<tr><td>Kafka cluster</td><td>Platform</td><td>Active</td></tr>
			<tr><td>Topic User</td><td>Data</td><td>Active</td></tr>
		</tbody>
	</table>`
	in := `[
		{"type":"Title","text":"Component Inventory","metadata":{}},
		{"type":"Table","text":"Component Owner Status Kafka cluster Platform Active","metadata":{"text_as_html":` + jsonString(html) + `}}
	]`
	out, err := fn.Eval(in, "spec.pdf")
	require.NoError(t, err)
	// Each row line carries header labels — no orphan "Platform" cell.
	assert.Contains(t, out, "Component: Kafka cluster | Owner: Platform | Status: Active")
	assert.Contains(t, out, "Component: Topic User | Owner: Data | Status: Active")
	// Section context survives.
	assert.Contains(t, out, "[Section: Component Inventory]")
	// Columns line is added once per chunk.
	assert.Contains(t, out, "Columns: Component | Owner | Status")
}

func TestSmartChunk_FormFieldDigest(t *testing.T) {
	fn := &fnSmartChunk{}
	html := `<table>
		<tbody>
			<tr><td>Receiver domain type</td><td>External</td></tr>
			<tr><td>Sender system name</td><td>S/4HANA</td></tr>
			<tr><td>JIRA issue</td><td>INT-9469</td></tr>
		</tbody>
	</table>`
	in := `[
		{"type":"Title","text":"Interface 9469","metadata":{}},
		{"type":"Table","text":"junk","metadata":{"text_as_html":` + jsonString(html) + `}}
	]`
	out, err := fn.Eval(in, "fs.pdf")
	require.NoError(t, err)
	assert.Contains(t, out, "Form fields:")
	assert.Contains(t, out, "- Receiver domain type: External")
	assert.Contains(t, out, "- Sender system name: S/4HANA")
	assert.Contains(t, out, "- JIRA issue: INT-9469")
}

func TestSmartChunk_SkipsFooterHeaderImage(t *testing.T) {
	fn := &fnSmartChunk{}
	in := `[
		{"type":"Header","text":"Confidential","metadata":{}},
		{"type":"Footer","text":"Page 1 of 5","metadata":{}},
		{"type":"Image","text":"","metadata":{}},
		{"type":"PageBreak","text":"","metadata":{}},
		{"type":"NarrativeText","text":"Real content here.","metadata":{}}
	]`
	out, err := fn.Eval(in, "x.pdf")
	require.NoError(t, err)
	assert.Contains(t, out, "Real content here.")
	assert.NotContains(t, out, "Confidential")
	assert.NotContains(t, out, "Page 1 of 5")
}

func TestSmartChunk_ProseAccumulatesUntilTargetSize(t *testing.T) {
	fn := &fnSmartChunk{}
	// Two short paragraphs under the same title should be ONE chunk.
	in := `[
		{"type":"Title","text":"Sec","metadata":{}},
		{"type":"NarrativeText","text":"First short paragraph.","metadata":{}},
		{"type":"NarrativeText","text":"Second short paragraph.","metadata":{}}
	]`
	out, err := fn.Eval(in, "x.pdf")
	require.NoError(t, err)
	chunks := strings.Split(out.(string), "\n\n")
	// 1 heading mini-chunk + 1 combined prose chunk = 2 chunks total.
	assert.Len(t, chunks, 2)
	assert.Contains(t, chunks[1], "First short paragraph.")
	assert.Contains(t, chunks[1], "Second short paragraph.")
}

func TestSmartChunk_LongProseFlushesAtTarget(t *testing.T) {
	fn := &fnSmartChunk{}
	bigPara := strings.Repeat("word ", 400) // ~2000 chars, > proseTargetSize
	in := `[
		{"type":"Title","text":"Sec","metadata":{}},
		{"type":"NarrativeText","text":"` + bigPara + `","metadata":{}},
		{"type":"NarrativeText","text":"Trailing line.","metadata":{}}
	]`
	out, err := fn.Eval(in, "x.pdf")
	require.NoError(t, err)
	// Expect at least 2 prose chunks (excluding the heading mini-chunk).
	chunks := strings.Split(out.(string), "\n\n")
	assert.GreaterOrEqual(t, len(chunks), 2)
}

func TestSmartChunk_DedupesIdenticalChunks(t *testing.T) {
	fn := &fnSmartChunk{}
	in := `[
		{"type":"Title","text":"Same","metadata":{}},
		{"type":"Title","text":"Same","metadata":{}}
	]`
	out, err := fn.Eval(in, "x.pdf")
	require.NoError(t, err)
	// Only one heading mini-chunk should remain after dedup.
	count := strings.Count(out.(string), "Section heading: Same")
	assert.Equal(t, 1, count)
}

func TestSmartChunk_TableHardCapSplitsAcrossChunks(t *testing.T) {
	fn := &fnSmartChunk{}
	// Build a 200-row table whose row text alone exceeds chunkHardCap (6000).
	var rows strings.Builder
	for i := 0; i < 200; i++ {
		rows.WriteString("<tr><td>Row ")
		rows.WriteString(strings.Repeat("X", 50)) // padding to force size growth
		rows.WriteString("</td><td>val</td></tr>")
	}
	html := `<table><thead><tr><th>Key</th><th>Value</th></tr></thead><tbody>` + rows.String() + `</tbody></table>`
	in := `[
		{"type":"Title","text":"Big","metadata":{}},
		{"type":"Table","text":"big","metadata":{"text_as_html":` + jsonString(html) + `}}
	]`
	out, err := fn.Eval(in, "x.pdf")
	require.NoError(t, err)
	// The form-field digest path will catch this (2-column), so we should
	// still get at least one chunk that includes the digest header.
	assert.Contains(t, out, "Form fields:")
}

// jsonString returns a JSON string literal (with quotes and escapes) for the
// given input — used to embed HTML safely inside JSON test fixtures.
func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// ── Phase 1.5 tests ─────────────────────────────────────────────────────────

func TestSmartChunk_DropsDescriptionColumnByHeader(t *testing.T) {
	fn := &fnSmartChunk{}
	// 3-column form table: Field | Value | Description.
	html := `<table>
		<thead><tr><th>Field</th><th>Value</th><th>Description</th></tr></thead>
		<tbody>
			<tr><td>Receiver domain type</td><td>External</td><td>Please describe the domain type for the receiver system.</td></tr>
			<tr><td>JIRA issue</td><td>INT-9469</td><td>Provide an overview of the JIRA template issue link.</td></tr>
		</tbody>
	</table>`
	in := `[
		{"type":"Title","text":"Interface 9469","metadata":{}},
		{"type":"Table","text":"x","metadata":{"text_as_html":` + jsonString(html) + `}}
	]`
	out, err := fn.Eval(in, "fs.pdf")
	require.NoError(t, err)
	s := out.(string)

	// Description text MUST NOT appear anywhere in the chunks.
	assert.NotContains(t, s, "Please describe")
	assert.NotContains(t, s, "Provide an overview")
	// Form digest should now trigger because the 3-col reduces to 2-col.
	assert.Contains(t, s, "Form fields:")
	assert.Contains(t, s, "- Receiver domain type: External")
	assert.Contains(t, s, "- JIRA issue: INT-9469")
}

func TestSmartChunk_DropsDescriptionColumnByContent(t *testing.T) {
	fn := &fnSmartChunk{}
	// Header is innocuous ("Notes") but content is clearly guidance.
	html := `<table>
		<thead><tr><th>Field</th><th>Value</th><th>Notes</th></tr></thead>
		<tbody>
			<tr><td>Sender protocol</td><td>HTTPS</td><td>Please specify the protocol used by the sender system.</td></tr>
			<tr><td>Receiver protocol</td><td>HTTPS</td><td>Please specify the protocol used by the receiver system.</td></tr>
			<tr><td>Initial Load</td><td>No</td><td>Initial Load is needed or not — describe behaviour for backfill.</td></tr>
			<tr><td>Frequency</td><td>Realtime</td><td>Provide an overview of the message frequency e.g. realtime, daily.</td></tr>
		</tbody>
	</table>`
	in := `[
		{"type":"Title","text":"Form","metadata":{}},
		{"type":"Table","text":"x","metadata":{"text_as_html":` + jsonString(html) + `}}
	]`
	out, err := fn.Eval(in, "fs.pdf")
	require.NoError(t, err)
	s := out.(string)
	assert.NotContains(t, s, "Please specify")
	assert.NotContains(t, s, "Provide an overview")
	assert.NotContains(t, s, "needed or not")
	assert.Contains(t, s, "- Sender protocol: HTTPS")
	assert.Contains(t, s, "- Frequency: Realtime")
}

func TestSmartChunk_KeepsLegitimateValueColumn(t *testing.T) {
	fn := &fnSmartChunk{}
	// All three columns are real data — no header keywords, content is fact-y.
	html := `<table>
		<thead><tr><th>Component</th><th>Version</th><th>Status</th></tr></thead>
		<tbody>
			<tr><td>BWCE</td><td>2.7.0</td><td>Active</td></tr>
			<tr><td>EMS</td><td>10.4</td><td>Active</td></tr>
			<tr><td>Kafka</td><td>3.5</td><td>Deprecated</td></tr>
		</tbody>
	</table>`
	in := `[
		{"type":"Title","text":"Inventory","metadata":{}},
		{"type":"Table","text":"x","metadata":{"text_as_html":` + jsonString(html) + `}}
	]`
	out, err := fn.Eval(in, "x.pdf")
	require.NoError(t, err)
	s := out.(string)
	// All three columns must survive.
	assert.Contains(t, s, "Component: BWCE")
	assert.Contains(t, s, "Version: 2.7.0")
	assert.Contains(t, s, "Status: Active")
	assert.Contains(t, s, "Status: Deprecated")
}

func TestSmartChunk_EnvironmentMatrixEmitsAtomicChunks(t *testing.T) {
	fn := &fnSmartChunk{}
	html := `<table>
		<thead><tr><th>Property</th><th>DEV</th><th>SIT</th><th>UAT</th><th>PRD</th></tr></thead>
		<tbody>
			<tr><td>Kafka cluster</td><td>PIVOTAL</td><td>FRANKFURT</td><td>FRANKFURT</td><td>FRANKFURT</td></tr>
			<tr><td>Topic User</td><td>nonpii</td><td>dss</td><td>dss</td><td>dss</td></tr>
			<tr><td>Schema registry</td><td>https://dev.x</td><td>https://sit.x</td><td>https://uat.x</td><td>https://prd.x</td></tr>
		</tbody>
	</table>`
	in := `[
		{"type":"Title","text":"Environments","metadata":{}},
		{"type":"Table","text":"x","metadata":{"text_as_html":` + jsonString(html) + `}}
	]`
	out, err := fn.Eval(in, "spec.pdf")
	require.NoError(t, err)
	s := out.(string)
	// Each (property, env) pair must appear as its own focused chunk.
	assert.Contains(t, s, "Kafka cluster (DEV): PIVOTAL")
	assert.Contains(t, s, "Kafka cluster (PRD): FRANKFURT")
	assert.Contains(t, s, "Topic User (DEV): nonpii")
	assert.Contains(t, s, "Topic User (UAT): dss")
	assert.Contains(t, s, "Schema registry (SIT): https://sit.x")
	// Row-style chunks must NOT also be emitted (avoid duplication).
	assert.NotContains(t, s, "DEV: PIVOTAL | SIT: FRANKFURT")
}

func TestSmartChunk_EnvMatrixSkipsEmptyAndNAValues(t *testing.T) {
	fn := &fnSmartChunk{}
	html := `<table>
		<thead><tr><th>Property</th><th>DEV</th><th>SIT</th><th>PRD</th></tr></thead>
		<tbody>
			<tr><td>Optional flag</td><td>true</td><td>-</td><td>n/a</td></tr>
		</tbody>
	</table>`
	in := `[
		{"type":"Title","text":"Sec","metadata":{}},
		{"type":"Table","text":"x","metadata":{"text_as_html":` + jsonString(html) + `}}
	]`
	out, err := fn.Eval(in, "x.pdf")
	require.NoError(t, err)
	s := out.(string)
	assert.Contains(t, s, "Optional flag (DEV): true")
	assert.NotContains(t, s, "Optional flag (SIT)")
	assert.NotContains(t, s, "Optional flag (PRD)")
}

func TestSmartChunk_PerCellTruncation(t *testing.T) {
	fn := &fnSmartChunk{}
	long := strings.Repeat("X", 500) // > maxCellChars (200)
	html := `<table>
		<thead><tr><th>Component</th><th>Version</th><th>Status</th></tr></thead>
		<tbody>
			<tr><td>BWCE</td><td>` + long + `</td><td>Active</td></tr>
			<tr><td>EMS</td><td>` + long + `</td><td>Active</td></tr>
			<tr><td>K</td><td>` + long + `</td><td>Active</td></tr>
		</tbody>
	</table>`
	in := `[
		{"type":"Title","text":"Inv","metadata":{}},
		{"type":"Table","text":"x","metadata":{"text_as_html":` + jsonString(html) + `}}
	]`
	out, err := fn.Eval(in, "x.pdf")
	require.NoError(t, err)
	s := out.(string)
	// The 500-char value must have been truncated with an ellipsis.
	assert.Contains(t, s, "…")
	// And no run of >250 X's (200 + buffer) should survive.
	assert.NotContains(t, s, strings.Repeat("X", 250))
}

package textfn

import (
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
}

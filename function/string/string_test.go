package stringfn

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── padLeft ──────────────────────────────────────────────────────────────────

func TestPadLeft(t *testing.T) {
	fn := &fnPadLeft{}

	t.Run("zero-pad number", func(t *testing.T) {
		out, err := fn.Eval("7", 3, "0")
		require.NoError(t, err)
		assert.Equal(t, "007", out)
	})
	t.Run("space pad", func(t *testing.T) {
		out, err := fn.Eval("hi", 5, " ")
		require.NoError(t, err)
		assert.Equal(t, "   hi", out)
	})
	t.Run("already at size", func(t *testing.T) {
		out, err := fn.Eval("abc", 3, "0")
		require.NoError(t, err)
		assert.Equal(t, "abc", out)
	})
	t.Run("longer than size returns unchanged", func(t *testing.T) {
		out, err := fn.Eval("abcde", 3, "0")
		require.NoError(t, err)
		assert.Equal(t, "abcde", out)
	})
	t.Run("empty padChar defaults to space", func(t *testing.T) {
		out, err := fn.Eval("x", 3, "")
		require.NoError(t, err)
		assert.Equal(t, "  x", out)
	})
	t.Run("unicode string", func(t *testing.T) {
		out, err := fn.Eval("日本", 4, "_")
		require.NoError(t, err)
		assert.Equal(t, "__日本", out)
	})
}

// ─── padRight ─────────────────────────────────────────────────────────────────

func TestPadRight(t *testing.T) {
	fn := &fnPadRight{}

	t.Run("dash pad", func(t *testing.T) {
		out, err := fn.Eval("hi", 5, "-")
		require.NoError(t, err)
		assert.Equal(t, "hi---", out)
	})
	t.Run("already at size", func(t *testing.T) {
		out, err := fn.Eval("abc", 3, "x")
		require.NoError(t, err)
		assert.Equal(t, "abc", out)
	})
	t.Run("longer than size returns unchanged", func(t *testing.T) {
		out, err := fn.Eval("toolong", 3, "0")
		require.NoError(t, err)
		assert.Equal(t, "toolong", out)
	})
	t.Run("empty padChar defaults to space", func(t *testing.T) {
		out, err := fn.Eval("ok", 5, "")
		require.NoError(t, err)
		assert.Equal(t, "ok   ", out)
	})
}

// ─── mask ─────────────────────────────────────────────────────────────────────

func TestMask(t *testing.T) {
	fn := &fnMask{}

	t.Run("credit card - keep last 4", func(t *testing.T) {
		out, err := fn.Eval("4111-1111-1111-1234", 0, 4)
		require.NoError(t, err)
		assert.Equal(t, "***************1234", out)
	})
	t.Run("email - keep first 1 last 9", func(t *testing.T) {
		// "@acme.com" is 9 chars; keepLast=9 preserves the domain including @
		out, err := fn.Eval("john.doe@acme.com", 1, 9)
		require.NoError(t, err)
		assert.Equal(t, "j*******@acme.com", out)
	})
	t.Run("mask all", func(t *testing.T) {
		out, err := fn.Eval("secret", 0, 0)
		require.NoError(t, err)
		assert.Equal(t, "******", out)
	})
	t.Run("keepFirst+keepLast >= len returns unchanged", func(t *testing.T) {
		out, err := fn.Eval("abc", 2, 2)
		require.NoError(t, err)
		assert.Equal(t, "abc", out)
	})
	t.Run("keep first only", func(t *testing.T) {
		out, err := fn.Eval("password", 2, 0)
		require.NoError(t, err)
		assert.Equal(t, "pa******", out)
	})
	t.Run("keep last only", func(t *testing.T) {
		out, err := fn.Eval("password", 0, 3)
		require.NoError(t, err)
		assert.Equal(t, "*****ord", out)
	})
	t.Run("negative keepFirst", func(t *testing.T) {
		_, err := fn.Eval("abc", -1, 0)
		assert.Error(t, err)
	})
}

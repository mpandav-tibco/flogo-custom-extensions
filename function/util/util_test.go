package utilfn

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── coalesce ─────────────────────────────────────────────────────────────────

func TestCoalesce(t *testing.T) {
	fn := &fnCoalesce{}

	t.Run("returns first non-nil", func(t *testing.T) {
		out, err := fn.Eval(nil, nil, "hello")
		require.NoError(t, err)
		assert.Equal(t, "hello", out)
	})
	t.Run("skips empty string", func(t *testing.T) {
		out, err := fn.Eval("", "fallback")
		require.NoError(t, err)
		assert.Equal(t, "fallback", out)
	})
	t.Run("returns first non-empty", func(t *testing.T) {
		out, err := fn.Eval(nil, "", "first-valid", "second")
		require.NoError(t, err)
		assert.Equal(t, "first-valid", out)
	})
	t.Run("all nil returns nil", func(t *testing.T) {
		out, err := fn.Eval(nil, nil, nil)
		require.NoError(t, err)
		assert.Nil(t, out)
	})
	t.Run("non-string non-nil value", func(t *testing.T) {
		out, err := fn.Eval(nil, 42)
		require.NoError(t, err)
		assert.Equal(t, 42, out)
	})
	t.Run("single non-nil arg", func(t *testing.T) {
		out, err := fn.Eval("only")
		require.NoError(t, err)
		assert.Equal(t, "only", out)
	})
	t.Run("zero is valid (not skipped)", func(t *testing.T) {
		out, err := fn.Eval(nil, 0)
		require.NoError(t, err)
		assert.Equal(t, 0, out)
	})
}

// ─── sha256 ───────────────────────────────────────────────────────────────────

func TestSHA256(t *testing.T) {
	fn := &fnSHA256{}

	t.Run("well-known hash of 'hello'", func(t *testing.T) {
		out, err := fn.Eval("hello")
		require.NoError(t, err)
		assert.Equal(t, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", out)
	})
	t.Run("empty string", func(t *testing.T) {
		out, err := fn.Eval("")
		require.NoError(t, err)
		// SHA-256 of empty string
		assert.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", out)
	})
	t.Run("same input same output", func(t *testing.T) {
		out1, _ := fn.Eval("flogo")
		out2, _ := fn.Eval("flogo")
		assert.Equal(t, out1, out2)
	})
	t.Run("different inputs differ", func(t *testing.T) {
		out1, _ := fn.Eval("abc")
		out2, _ := fn.Eval("ABC")
		assert.NotEqual(t, out1, out2)
	})
	t.Run("output is 64 hex chars", func(t *testing.T) {
		out, err := fn.Eval("test")
		require.NoError(t, err)
		assert.Len(t, out, 64)
	})
}

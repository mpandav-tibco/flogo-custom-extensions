package arrayfn

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── min ──────────────────────────────────────────────────────────────────────

func TestMin(t *testing.T) {
	fn := &fnMin{}

	t.Run("integers", func(t *testing.T) {
		out, err := fn.Eval([]interface{}{3, 1, 4, 1, 5, 9})
		require.NoError(t, err)
		assert.Equal(t, 1.0, out)
	})
	t.Run("floats", func(t *testing.T) {
		out, err := fn.Eval([]interface{}{3.5, 1.1, 2.9})
		require.NoError(t, err)
		assert.Equal(t, 1.1, out)
	})
	t.Run("single element", func(t *testing.T) {
		out, err := fn.Eval([]interface{}{42})
		require.NoError(t, err)
		assert.Equal(t, 42.0, out)
	})
	t.Run("negative values", func(t *testing.T) {
		out, err := fn.Eval([]interface{}{-1, -5, -2})
		require.NoError(t, err)
		assert.Equal(t, -5.0, out)
	})
	t.Run("empty array", func(t *testing.T) {
		_, err := fn.Eval([]interface{}{})
		assert.Error(t, err)
	})
	t.Run("nil input", func(t *testing.T) {
		_, err := fn.Eval(nil)
		assert.Error(t, err)
	})
}

// ─── max ──────────────────────────────────────────────────────────────────────

func TestMax(t *testing.T) {
	fn := &fnMax{}

	t.Run("integers", func(t *testing.T) {
		out, err := fn.Eval([]interface{}{3, 1, 4, 1, 5, 9})
		require.NoError(t, err)
		assert.Equal(t, 9.0, out)
	})
	t.Run("floats", func(t *testing.T) {
		out, err := fn.Eval([]interface{}{3.5, 1.1, 2.9})
		require.NoError(t, err)
		assert.Equal(t, 3.5, out)
	})
	t.Run("negative values", func(t *testing.T) {
		out, err := fn.Eval([]interface{}{-1, -5, -2})
		require.NoError(t, err)
		assert.Equal(t, -1.0, out)
	})
}

// ─── avg ──────────────────────────────────────────────────────────────────────

func TestAvg(t *testing.T) {
	fn := &fnAvg{}

	t.Run("integers", func(t *testing.T) {
		out, err := fn.Eval([]interface{}{1, 2, 3, 4, 5})
		require.NoError(t, err)
		assert.Equal(t, 3.0, out)
	})
	t.Run("floats", func(t *testing.T) {
		out, err := fn.Eval([]interface{}{1.0, 2.0, 3.0})
		require.NoError(t, err)
		assert.InDelta(t, 2.0, out, 1e-9)
	})
	t.Run("single element", func(t *testing.T) {
		out, err := fn.Eval([]interface{}{7})
		require.NoError(t, err)
		assert.Equal(t, 7.0, out)
	})
}

// ─── unique ───────────────────────────────────────────────────────────────────

func TestUnique(t *testing.T) {
	fn := &fnUnique{}

	t.Run("removes duplicates", func(t *testing.T) {
		out, err := fn.Eval([]interface{}{1, 2, 2, 3, 1})
		require.NoError(t, err)
		assert.Equal(t, []interface{}{1, 2, 3}, out)
	})
	t.Run("preserves order", func(t *testing.T) {
		out, err := fn.Eval([]interface{}{"c", "a", "b", "a", "c"})
		require.NoError(t, err)
		assert.Equal(t, []interface{}{"c", "a", "b"}, out)
	})
	t.Run("no duplicates stays unchanged", func(t *testing.T) {
		out, err := fn.Eval([]interface{}{10, 20, 30})
		require.NoError(t, err)
		assert.Equal(t, []interface{}{10, 20, 30}, out)
	})
	t.Run("all duplicates", func(t *testing.T) {
		out, err := fn.Eval([]interface{}{"x", "x", "x"})
		require.NoError(t, err)
		assert.Equal(t, []interface{}{"x"}, out)
	})
	t.Run("empty array", func(t *testing.T) {
		out, err := fn.Eval([]interface{}{})
		require.NoError(t, err)
		assert.Equal(t, []interface{}{}, out)
	})
}

// ─── indexOf ──────────────────────────────────────────────────────────────────

func TestIndexOf(t *testing.T) {
	fn := &fnIndexOf{}

	t.Run("found string", func(t *testing.T) {
		out, err := fn.Eval([]interface{}{"a", "b", "c"}, "b")
		require.NoError(t, err)
		assert.Equal(t, 1, out)
	})
	t.Run("found integer", func(t *testing.T) {
		out, err := fn.Eval([]interface{}{10, 20, 30}, 20)
		require.NoError(t, err)
		assert.Equal(t, 1, out)
	})
	t.Run("not found returns -1", func(t *testing.T) {
		out, err := fn.Eval([]interface{}{"a", "b", "c"}, "z")
		require.NoError(t, err)
		assert.Equal(t, -1, out)
	})
	t.Run("first occurrence", func(t *testing.T) {
		out, err := fn.Eval([]interface{}{"a", "b", "a", "c"}, "a")
		require.NoError(t, err)
		assert.Equal(t, 0, out)
	})
	t.Run("empty array", func(t *testing.T) {
		out, err := fn.Eval([]interface{}{}, "x")
		require.NoError(t, err)
		assert.Equal(t, -1, out)
	})
}

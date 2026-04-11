package mathfn

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAbs(t *testing.T) {
	fn := &fnAbs{}

	t.Run("negative float", func(t *testing.T) {
		out, err := fn.Eval(-5.5)
		require.NoError(t, err)
		assert.Equal(t, 5.5, out)
	})
	t.Run("positive float", func(t *testing.T) {
		out, err := fn.Eval(3.14)
		require.NoError(t, err)
		assert.Equal(t, 3.14, out)
	})
	t.Run("zero", func(t *testing.T) {
		out, err := fn.Eval(0.0)
		require.NoError(t, err)
		assert.Equal(t, 0.0, out)
	})
	t.Run("integer input", func(t *testing.T) {
		out, err := fn.Eval(-7)
		require.NoError(t, err)
		assert.Equal(t, 7.0, out)
	})
	t.Run("string number input", func(t *testing.T) {
		out, err := fn.Eval("-3.0")
		require.NoError(t, err)
		assert.Equal(t, 3.0, out)
	})
	t.Run("invalid input", func(t *testing.T) {
		_, err := fn.Eval("not-a-number")
		assert.Error(t, err)
	})
}

func TestPow(t *testing.T) {
	fn := &fnPow{}

	t.Run("2 to the 10", func(t *testing.T) {
		out, err := fn.Eval(2.0, 10.0)
		require.NoError(t, err)
		assert.Equal(t, 1024.0, out)
	})
	t.Run("square root via fractional exp", func(t *testing.T) {
		out, err := fn.Eval(9.0, 0.5)
		require.NoError(t, err)
		assert.InDelta(t, 3.0, out, 1e-9)
	})
	t.Run("zero exponent", func(t *testing.T) {
		out, err := fn.Eval(5.0, 0.0)
		require.NoError(t, err)
		assert.Equal(t, 1.0, out)
	})
	t.Run("base zero", func(t *testing.T) {
		out, err := fn.Eval(0.0, 5.0)
		require.NoError(t, err)
		assert.Equal(t, 0.0, out)
	})
	t.Run("integer inputs", func(t *testing.T) {
		out, err := fn.Eval(3, 3)
		require.NoError(t, err)
		assert.Equal(t, 27.0, out)
	})
}

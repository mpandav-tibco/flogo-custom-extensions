package vectordb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// normalizeDistanceMetric
// ---------------------------------------------------------------------------

func TestNormalizeDistanceMetric(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", "cosine"},
		{"cosine", "cosine"},
		{"COSINE", "cosine"},
		{"Cosine", "cosine"},
		{"dot", "dot"},
		{"DOT", "dot"},
		{"euclidean", "euclidean"},
		{"EUCLIDEAN", "euclidean"},
		{"  cosine  ", "cosine"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizeDistanceMetric(tc.input))
		})
	}
}

// ---------------------------------------------------------------------------
// validateCollectionConfig
// ---------------------------------------------------------------------------

func TestValidateCollectionConfig_Valid(t *testing.T) {
	cfg := CollectionConfig{Name: "test", Dimensions: 128, DistanceMetric: "cosine"}
	assert.NoError(t, validateCollectionConfig(cfg))
}

func TestValidateCollectionConfig_EmptyName(t *testing.T) {
	cfg := CollectionConfig{Name: "", Dimensions: 128}
	err := validateCollectionConfig(cfg)
	require.Error(t, err)
	var ve *VDBError
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, ErrCodeInvalidCollectionName, ve.Code)
}

func TestValidateCollectionConfig_ZeroDimensions(t *testing.T) {
	cfg := CollectionConfig{Name: "test", Dimensions: 0}
	err := validateCollectionConfig(cfg)
	require.Error(t, err)
	var ve *VDBError
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, ErrCodeInvalidDimensions, ve.Code)
}

func TestValidateCollectionConfig_InvalidMetric(t *testing.T) {
	cfg := CollectionConfig{Name: "test", Dimensions: 128, DistanceMetric: "manhattan"}
	err := validateCollectionConfig(cfg)
	require.Error(t, err)
	var ve *VDBError
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, ErrCodeInvalidMetric, ve.Code)
}

// ---------------------------------------------------------------------------
// validateConnectionConfig (via NewClient invalid type)
// ---------------------------------------------------------------------------

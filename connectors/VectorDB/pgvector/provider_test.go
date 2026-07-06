//go:build !integration

package vectordb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- DSN building ---

func TestBuildPgvectorDSN(t *testing.T) {
	cfg := ConnectionConfig{
		DBType:   "pgvector",
		Host:     "localhost",
		Port:     5432,
		Username: "testuser",
		Password: "testpassword",
		DBName:   "vectordb",
		SSLMode:  "disable",
	}
	dsn := buildPgvectorDSN(cfg)
	assert.Contains(t, dsn, "host=localhost")
	assert.Contains(t, dsn, "port=5432")
	assert.Contains(t, dsn, "user=testuser")
	assert.Contains(t, dsn, "password=testpassword")
	assert.Contains(t, dsn, "dbname=vectordb")
	assert.Contains(t, dsn, "sslmode=disable")
}

// --- validateConnectionConfig ---

func TestValidateConnectionConfig_Defaults(t *testing.T) {
	cfg := ConnectionConfig{Host: "localhost"}
	require.NoError(t, validateConnectionConfig(&cfg))
	assert.Equal(t, "pgvector", cfg.DBType)
	assert.Equal(t, 5432, cfg.Port)
	assert.Equal(t, "postgres", cfg.DBName)
	assert.Equal(t, "disable", cfg.SSLMode)
	assert.Equal(t, 3, cfg.MaxRetries)
	assert.Equal(t, 500, cfg.RetryBackoffMs)
	assert.Equal(t, 30, cfg.TimeoutSeconds)
}

func TestValidateConnectionConfig_MissingHost(t *testing.T) {
	cfg := ConnectionConfig{}
	err := validateConnectionConfig(&cfg)
	require.Error(t, err)
	var vdbErr *VDBError
	require.ErrorAs(t, err, &vdbErr)
	assert.Equal(t, ErrCodeMissingHost, vdbErr.Code)
}

func TestValidateConnectionConfig_InvalidPort(t *testing.T) {
	cfg := ConnectionConfig{Host: "localhost", Port: 99999}
	err := validateConnectionConfig(&cfg)
	require.Error(t, err)
	var vdbErr *VDBError
	require.ErrorAs(t, err, &vdbErr)
	assert.Equal(t, ErrCodeInvalidPort, vdbErr.Code)
}

func TestValidateConnectionConfig_NegativeTimeout(t *testing.T) {
	cfg := ConnectionConfig{Host: "localhost", TimeoutSeconds: -1}
	err := validateConnectionConfig(&cfg)
	require.Error(t, err)
	var vdbErr *VDBError
	require.ErrorAs(t, err, &vdbErr)
	assert.Equal(t, ErrCodeInvalidTimeout, vdbErr.Code)
}

func TestValidateConnectionConfig_DBTypeAlwaysPgvector(t *testing.T) {
	cfg := ConnectionConfig{Host: "localhost", DBType: "anything"}
	require.NoError(t, validateConnectionConfig(&cfg))
	assert.Equal(t, "pgvector", cfg.DBType)
}

// --- DBType ---

func TestConnectionConfig_String_RedactsPassword(t *testing.T) {
	cfg := ConnectionConfig{
		DBType:   "pgvector",
		Host:     "localhost",
		Port:     5432,
		Password: "supersecret",
	}
	s := cfg.String()
	assert.NotContains(t, s, "supersecret")
	assert.Contains(t, s, "[redacted]")
}

// --- Table name sanitization ---

func TestPgvectorSafeTableName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"my-collection", "my_collection"},
		{"MyCollection", "mycollection"},
		{"collection 1", "collection_1"},
		{"123table", "t_123table"},
		{"valid_name_123", "valid_name_123"},
		{"CamelCaseTable", "camelcasetable"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, pgvectorSafeTableName(tc.input))
		})
	}
}

// --- Filter building ---

func TestBuildPgvectorFilter_Empty(t *testing.T) {
	clause, args := buildPgvectorFilter(nil, 1)
	assert.Equal(t, "", clause)
	assert.Nil(t, args)
}

func TestBuildPgvectorFilter_SimpleEquality(t *testing.T) {
	filters := map[string]interface{}{
		"category": "science",
	}
	clause, args := buildPgvectorFilter(filters, 1)
	assert.Contains(t, clause, "metadata->>'category'")
	assert.Contains(t, clause, "$1")
	assert.Equal(t, 1, len(args))
	assert.Equal(t, "science", args[0])
}

func TestBuildPgvectorFilter_EqOperator(t *testing.T) {
	filters := map[string]interface{}{
		"status": map[string]interface{}{"$eq": "active"},
	}
	clause, args := buildPgvectorFilter(filters, 1)
	assert.Contains(t, clause, "metadata->>'status' = $1")
	assert.Equal(t, 1, len(args))
	assert.Equal(t, "active", args[0])
}

func TestBuildPgvectorFilter_NumericGt(t *testing.T) {
	filters := map[string]interface{}{
		"year": map[string]interface{}{"$gt": float64(2020)},
	}
	clause, args := buildPgvectorFilter(filters, 1)
	assert.Contains(t, clause, "(metadata->>'year')::numeric > $1")
	require.Equal(t, 1, len(args))
}

func TestBuildPgvectorFilter_InOperator(t *testing.T) {
	filters := map[string]interface{}{
		"tags": map[string]interface{}{"$in": []interface{}{"a", "b", "c"}},
	}
	clause, args := buildPgvectorFilter(filters, 1)
	assert.Contains(t, clause, "metadata->>'tags' = ANY($1)")
	require.Equal(t, 1, len(args))
	strs, ok := args[0].([]string)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{"a", "b", "c"}, strs)
}

func TestBuildPgvectorFilter_MultipleConditions(t *testing.T) {
	filters := map[string]interface{}{
		"category": map[string]interface{}{"$eq": "tech"},
		"year":     map[string]interface{}{"$gte": float64(2022)},
	}
	clause, args := buildPgvectorFilter(filters, 1)
	assert.Contains(t, clause, "AND")
	assert.Equal(t, 2, len(args))
}

func TestBuildPgvectorFilter_ParamStartIdx(t *testing.T) {
	// When startIdx=2 (because $1 is the query vector), params should start at $2.
	filters := map[string]interface{}{
		"category": "tech",
	}
	clause, args := buildPgvectorFilter(filters, 2)
	assert.Contains(t, clause, "$2")
	assert.Equal(t, 1, len(args))
}

// --- Score computation ---

func TestComputeScore_Cosine(t *testing.T) {
	// cosine distance 0.1 → score = 1 - 0.1 = 0.9
	score := computeScore("cosine", 0.1)
	assert.InDelta(t, 0.9, score, 1e-9)
}

func TestComputeScore_Euclidean(t *testing.T) {
	// euclidean distance 1.0 → score = 1/(1+1) = 0.5
	score := computeScore("euclidean", 1.0)
	assert.InDelta(t, 0.5, score, 1e-9)
}

func TestComputeScore_Dot(t *testing.T) {
	// dot product pgvector returns negative inner product, negate it
	score := computeScore("dot", -0.8)
	assert.InDelta(t, 0.8, score, 1e-9)
}

// --- Batch size validation ---

func TestValidateBatchSize_WithinLimit(t *testing.T) {
	err := validateBatchSize(999, maxUpsertBatchPgvector, "pgvector")
	assert.NoError(t, err)
}

func TestValidateBatchSize_AtLimit(t *testing.T) {
	err := validateBatchSize(1000, maxUpsertBatchPgvector, "pgvector")
	assert.NoError(t, err)
}

func TestValidateBatchSize_ExceedsLimit(t *testing.T) {
	err := validateBatchSize(1001, maxUpsertBatchPgvector, "pgvector")
	require.Error(t, err)
	var vdbErr *VDBError
	require.ErrorAs(t, err, &vdbErr)
	assert.Equal(t, ErrCodeBatchTooLarge, vdbErr.Code)
}

// --- Distance operator helpers ---

func TestPgvectorDistanceOps(t *testing.T) {
	tests := []struct {
		metric      string
		wantSymbol  string
		wantScore   string
	}{
		{"cosine", "<=>", "cosine"},
		{"euclidean", "<->", "euclidean"},
		{"dot", "<#>", "dot"},
		{"", "<=>", "cosine"},   // default
		{"COSINE", "<=>", "cosine"}, // case insensitive
	}
	for _, tc := range tests {
		op, score := pgvectorDistanceOps(tc.metric)
		sym := pgvectorDistOpSymbol(op)
		assert.Equal(t, tc.wantSymbol, sym, "metric=%s", tc.metric)
		assert.Equal(t, tc.wantScore, score, "metric=%s", tc.metric)
	}
}

// --- Validation helpers ---

func TestValidateCollectionConfig_Valid(t *testing.T) {
	cfg := CollectionConfig{Name: "test", Dimensions: 1536, DistanceMetric: "cosine"}
	assert.NoError(t, validateCollectionConfig(cfg))
}

func TestValidateCollectionConfig_InvalidMetric(t *testing.T) {
	cfg := CollectionConfig{Name: "test", Dimensions: 1536, DistanceMetric: "manhattan"}
	err := validateCollectionConfig(cfg)
	require.Error(t, err)
	var vdbErr *VDBError
	require.ErrorAs(t, err, &vdbErr)
	assert.Equal(t, ErrCodeInvalidMetric, vdbErr.Code)
}

func TestValidateCollectionConfig_ZeroDimensions(t *testing.T) {
	cfg := CollectionConfig{Name: "test", Dimensions: 0}
	err := validateCollectionConfig(cfg)
	require.Error(t, err)
	var vdbErr *VDBError
	require.ErrorAs(t, err, &vdbErr)
	assert.Equal(t, ErrCodeInvalidDimensions, vdbErr.Code)
}

func TestValidateUpsertDocuments_EmptyList(t *testing.T) {
	err := validateUpsertDocuments([]Document{})
	require.Error(t, err)
	var vdbErr *VDBError
	require.ErrorAs(t, err, &vdbErr)
	assert.Equal(t, ErrCodeEmptyDocumentList, vdbErr.Code)
}

func TestValidateUpsertDocuments_MissingID(t *testing.T) {
	docs := []Document{{Vector: []float64{0.1, 0.2}}}
	err := validateUpsertDocuments(docs)
	require.Error(t, err)
	var vdbErr *VDBError
	require.ErrorAs(t, err, &vdbErr)
	assert.Equal(t, ErrCodeInvalidDocumentID, vdbErr.Code)
}

func TestValidateUpsertDocuments_DimensionMismatch(t *testing.T) {
	docs := []Document{
		{ID: "1", Vector: []float64{0.1, 0.2, 0.3}},
		{ID: "2", Vector: []float64{0.1, 0.2}},
	}
	err := validateUpsertDocuments(docs)
	require.Error(t, err)
	var vdbErr *VDBError
	require.ErrorAs(t, err, &vdbErr)
	assert.Equal(t, ErrCodeInvalidVector, vdbErr.Code)
}

// --- Float conversion ---

func TestToFloat32Slice(t *testing.T) {
	in := []float64{1.0, 2.0, 3.0}
	out := toFloat32Slice(in)
	require.Len(t, out, 3)
	assert.InDelta(t, float32(1.0), out[0], 1e-6)
	assert.InDelta(t, float32(2.0), out[1], 1e-6)
	assert.InDelta(t, float32(3.0), out[2], 1e-6)
}

func TestToFloat64Slice(t *testing.T) {
	in := []float32{1.0, 2.0, 3.0}
	out := toFloat64Slice(in)
	require.Len(t, out, 3)
	assert.InDelta(t, 1.0, out[0], 1e-6)
}

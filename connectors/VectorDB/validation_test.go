package vectordb

import (
	"testing"
)

// ---------------------------------------------------------------------------
// validateBatchSize
// ---------------------------------------------------------------------------

func TestValidateBatchSize(t *testing.T) {
	tests := []struct {
		name     string
		n        int
		limit    int
		provider string
		wantErr  bool
		wantCode string
	}{
		{"exactly at limit", 1000, 1000, "Qdrant", false, ""},
		{"one below limit", 999, 1000, "Qdrant", false, ""},
		{"one over limit", 1001, 1000, "Qdrant", true, ErrCodeBatchTooLarge},
		{"far over limit", 5000, 1000, "Weaviate", true, ErrCodeBatchTooLarge},
		{"zero docs", 0, 1000, "Qdrant", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBatchSize(tt.n, tt.limit, tt.provider)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateBatchSize(%d, %d, %q) error = %v, wantErr %v",
					tt.n, tt.limit, tt.provider, err, tt.wantErr)
			}
			if tt.wantErr {
				vdbErr, ok := err.(*VDBError)
				if !ok {
					t.Fatalf("expected *VDBError, got %T", err)
				}
				if vdbErr.Code != tt.wantCode {
					t.Errorf("error code = %q, want %q", vdbErr.Code, tt.wantCode)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// validateSearchRequest
// ---------------------------------------------------------------------------

func TestValidateSearchRequest(t *testing.T) {
	validVec := []float64{0.1, 0.2, 0.3}
	tests := []struct {
		name       string
		collection string
		vector     []float64
		topK       int
		wantErr    bool
		wantCode   string
	}{
		{"valid request", "mycol", validVec, 5, false, ""},
		{"empty collection name", "", validVec, 5, true, ErrCodeInvalidCollectionName},
		{"nil vector", "mycol", nil, 5, true, ErrCodeInvalidQueryVector},
		{"empty vector", "mycol", []float64{}, 5, true, ErrCodeInvalidQueryVector},
		{"zero topK", "mycol", validVec, 0, true, ErrCodeInvalidTopK},
		{"negative topK", "mycol", validVec, -1, true, ErrCodeInvalidTopK},
		{"topK=1 is valid", "mycol", validVec, 1, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSearchRequest(tt.collection, tt.vector, tt.topK)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateSearchRequest error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				vdbErr, ok := err.(*VDBError)
				if !ok {
					t.Fatalf("expected *VDBError, got %T", err)
				}
				if vdbErr.Code != tt.wantCode {
					t.Errorf("error code = %q, want %q", vdbErr.Code, tt.wantCode)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// validateUpsertDocuments
// ---------------------------------------------------------------------------

func TestValidateUpsertDocuments(t *testing.T) {
	vec3 := []float64{0.1, 0.2, 0.3}
	vec4 := []float64{0.1, 0.2, 0.3, 0.4}

	tests := []struct {
		name     string
		docs     []Document
		wantErr  bool
		wantCode string
	}{
		{
			name:    "valid single doc",
			docs:    []Document{{ID: "a", Vector: vec3}},
			wantErr: false,
		},
		{
			name:    "valid multiple docs same dimension",
			docs:    []Document{{ID: "a", Vector: vec3}, {ID: "b", Vector: vec3}},
			wantErr: false,
		},
		{
			name:     "empty list",
			docs:     []Document{},
			wantErr:  true,
			wantCode: ErrCodeEmptyDocumentList,
		},
		{
			name:     "nil list",
			docs:     nil,
			wantErr:  true,
			wantCode: ErrCodeEmptyDocumentList,
		},
		{
			name:     "empty doc ID",
			docs:     []Document{{ID: "", Vector: vec3}},
			wantErr:  true,
			wantCode: ErrCodeInvalidDocumentID,
		},
		{
			name:     "nil vector",
			docs:     []Document{{ID: "a", Vector: nil}},
			wantErr:  true,
			wantCode: ErrCodeInvalidVector,
		},
		{
			name:     "empty vector",
			docs:     []Document{{ID: "a", Vector: []float64{}}},
			wantErr:  true,
			wantCode: ErrCodeInvalidVector,
		},
		{
			name:     "dimension mismatch",
			docs:     []Document{{ID: "a", Vector: vec3}, {ID: "b", Vector: vec4}},
			wantErr:  true,
			wantCode: ErrCodeInvalidVector,
		},
		{
			name:     "second doc empty id",
			docs:     []Document{{ID: "a", Vector: vec3}, {ID: "", Vector: vec3}},
			wantErr:  true,
			wantCode: ErrCodeInvalidDocumentID,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateUpsertDocuments(tt.docs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateUpsertDocuments error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				vdbErr, ok := err.(*VDBError)
				if !ok {
					t.Fatalf("expected *VDBError, got %T", err)
				}
				if vdbErr.Code != tt.wantCode {
					t.Errorf("error code = %q, want %q", vdbErr.Code, tt.wantCode)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// validateCollectionConfig
// ---------------------------------------------------------------------------

func TestValidateCollectionConfig(t *testing.T) {
	tests := []struct {
		name     string
		cfg      CollectionConfig
		wantErr  bool
		wantCode string
	}{
		{"valid cosine", CollectionConfig{Name: "col", Dimensions: 128, DistanceMetric: "cosine"}, false, ""},
		{"valid dot", CollectionConfig{Name: "col", Dimensions: 128, DistanceMetric: "dot"}, false, ""},
		{"valid euclidean", CollectionConfig{Name: "col", Dimensions: 128, DistanceMetric: "euclidean"}, false, ""},
		{"valid mixed case", CollectionConfig{Name: "col", Dimensions: 128, DistanceMetric: "Cosine"}, false, ""},
		{"empty metric defaults cosine", CollectionConfig{Name: "col", Dimensions: 128, DistanceMetric: ""}, false, ""},
		{"empty name", CollectionConfig{Name: "", Dimensions: 128, DistanceMetric: "cosine"}, true, ErrCodeInvalidCollectionName},
		{"zero dimensions", CollectionConfig{Name: "col", Dimensions: 0, DistanceMetric: "cosine"}, true, ErrCodeInvalidDimensions},
		{"negative dimensions", CollectionConfig{Name: "col", Dimensions: -1, DistanceMetric: "cosine"}, true, ErrCodeInvalidDimensions},
		{"unknown metric", CollectionConfig{Name: "col", Dimensions: 128, DistanceMetric: "hamming"}, true, ErrCodeInvalidMetric},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCollectionConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateCollectionConfig error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				vdbErr, ok := err.(*VDBError)
				if !ok {
					t.Fatalf("expected *VDBError, got %T", err)
				}
				if vdbErr.Code != tt.wantCode {
					t.Errorf("error code = %q, want %q", vdbErr.Code, tt.wantCode)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// toFloat32Slice / toFloat64Slice
// ---------------------------------------------------------------------------

func TestToFloat32Slice(t *testing.T) {
	in := []float64{1.0, 2.5, -3.75, 0.0}
	out := toFloat32Slice(in)
	if len(out) != len(in) {
		t.Fatalf("length mismatch: got %d, want %d", len(out), len(in))
	}
	for i, v := range in {
		if out[i] != float32(v) {
			t.Errorf("[%d] got %v, want %v", i, out[i], float32(v))
		}
	}
}

func TestToFloat32SliceEmpty(t *testing.T) {
	out := toFloat32Slice(nil)
	if len(out) != 0 {
		t.Errorf("expected empty slice for nil input, got len=%d", len(out))
	}
}

func TestToFloat64Slice(t *testing.T) {
	in := []float32{1.0, 2.5, -3.75, 0.0}
	out := toFloat64Slice(in)
	if len(out) != len(in) {
		t.Fatalf("length mismatch: got %d, want %d", len(out), len(in))
	}
	for i, v := range in {
		if out[i] != float64(v) {
			t.Errorf("[%d] got %v, want %v", i, out[i], float64(v))
		}
	}
}

func TestToFloat64SliceEmpty(t *testing.T) {
	out := toFloat64Slice(nil)
	if len(out) != 0 {
		t.Errorf("expected empty slice for nil input, got len=%d", len(out))
	}
}

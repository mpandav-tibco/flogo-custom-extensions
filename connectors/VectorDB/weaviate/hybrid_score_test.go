package vectordb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaviate/weaviate/entities/models"
)

// Weaviate's GraphQL API returns the hybrid fusion score as a JSON string; it must not be dropped to 0.
func TestWeaviateGraphQLToResults_HybridScoreString(t *testing.T) {
	resp := &models.GraphQLResponse{Data: map[string]models.JSONObject{
		"Get": map[string]interface{}{
			"Docs": []interface{}{
				map[string]interface{}{"_docId": "SAP_IDOC_To_KAFKATopic", "_additional": map[string]interface{}{"id": "a", "score": "1"}},
				map[string]interface{}{"_docId": "KAFKATopic_To_SAP_IDOC", "_additional": map[string]interface{}{"id": "b", "score": "0.86898935"}},
				map[string]interface{}{"_docId": "numeric", "_additional": map[string]interface{}{"id": "c", "score": 0.5}},
				map[string]interface{}{"_docId": "vector", "_additional": map[string]interface{}{"id": "d", "distance": 0.25}},
			},
		},
	}}

	got := weaviateGraphQLToResults(resp, "Docs")

	require.Len(t, got, 4)
	assert.Equal(t, "SAP_IDOC_To_KAFKATopic", got[0].ID)
	assert.InDelta(t, 1.0, got[0].Score, 1e-9)
	assert.InDelta(t, 0.86898935, got[1].Score, 1e-9)
	assert.InDelta(t, 0.5, got[2].Score, 1e-9)
	assert.InDelta(t, 0.75, got[3].Score, 1e-9)
}

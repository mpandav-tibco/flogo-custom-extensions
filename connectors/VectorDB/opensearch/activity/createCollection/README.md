# Create Collection

Create a new vector collection (index) in the OpenSearch 2.x database. Collections must exist before documents can be upserted.

## Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **VectorDB Connection** | Yes | — | The opensearch-connector connection |

## Input

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `collectionName` | string | — | Name of the collection to create |
| `dimensions` | integer | `1536` | Vector dimension — must match the embedding model output (e.g. 1536 for `text-embedding-3-small`, 768 for `nomic-embed-text`) |
| `distanceMetric` | string | `cosine` | Similarity metric: `cosine` (cosinesimil), `euclidean` (l2), `dot` (innerproduct) |

Requires index created with `"index.knn": true`. Uses `knn_vector` field type (not `dense_vector` like Elasticsearch).

## Output

| Field | Type | Description |
|-------|------|-------------|
| `success` | boolean | `true` if the collection was created successfully |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

## Behavior

- If the collection already exists the activity returns `success=false` with an error message. Use `listCollections` or `CollectionExists` to check first.
- The collection must be deleted and recreated if you need to change the vector dimension or distance metric.

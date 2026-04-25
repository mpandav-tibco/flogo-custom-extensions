# Create Collection

Create a new vector collection (index) in the connected VectorDB. Collections must be created before documents can be upserted.

## Settings

| Setting | Required | Default | Description |
|---|---|---|---|
| **VectorDB Connection** | Yes | — | The VectorDB connector (Qdrant, Weaviate, Chroma, Milvus) |

## Input

| Field | Type | Default | Description |
|---|---|---|---|
| `collectionName` | string | — | Name of the collection to create |
| `dimensions` | integer | `1536` | Vector dimension. Must match the embedding model output (e.g. 1536 for `text-embedding-3-small`, 768 for `nomic-embed-text`) |
| `distanceMetric` | string | `cosine` | Similarity metric: `cosine`, `euclidean`, or `dot` |
| `onDisk` | boolean | `false` | Store vectors on disk instead of RAM (Qdrant only) |
| `replicationFactor` | integer | `1` | Number of replicas (Qdrant cluster only) |

## Output

| Field | Type | Description |
|---|---|---|
| `success` | boolean | `true` if the collection was created successfully |
| `duration` | string | Elapsed time (e.g. `"45ms"`) |
| `error` | string | Error message if `success` is `false`. Includes `"already exists"` if the collection already exists. |

## Notes

- **Idempotency**: If the collection already exists the activity returns `success=false` with an error message containing `"already exists"`. Use the [Collection Exists](#) check or wrap in error handling to make flows idempotent.
- **Dimension mismatch**: Attempting to upsert vectors with the wrong dimension will fail at upsert time, not at collection creation time (except Milvus, which validates at create time).

## Provider Notes

| Setting | Qdrant | Weaviate | Chroma | Milvus |
|---|---|---|---|---|
| `distanceMetric` | Cosine / Euclid / Dot | Cosine / L2 / Dot | Cosine / L2 / IP | L2 / IP / Cosine |
| `onDisk` | Supported | N/A | N/A | N/A |
| `replicationFactor` | Cluster only | N/A | N/A | N/A |

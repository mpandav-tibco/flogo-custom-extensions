# Get Document

Retrieve a single document by its ID, including its content, metadata payload, and optionally its stored embedding vector.

## Settings

| Setting | Required | Default | Description |
|---|---|---|---|
| **VectorDB Connection** | Yes | — | The VectorDB connector (Qdrant, Weaviate, Chroma, Milvus) |
| **Default Collection** | No | — | Fallback collection name when not provided at runtime |

## Input

| Field | Type | Description |
|---|---|---|
| `collectionName` | string | Collection to search in. Overrides Default Collection. |
| `documentId` | string | Unique document ID to retrieve |

## Output

| Field | Type | Description |
|---|---|---|
| `success` | boolean | `true` if the operation completed without error |
| `found` | boolean | `true` if the document exists; `false` if not found |
| `document` | object | The document (see schema below). `null` when `found=false`. |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

### Document Object Schema

| Field | Type | Description |
|---|---|---|
| `id` | string | Document ID |
| `content` | string | Source text content |
| `vector` | array\<number\> | Dense embedding vector |
| `payload` | object | Metadata key-value pairs |
| `score` | number | Similarity score (always `0` for direct get; populated in search results) |

## Notes

- When the document does not exist, `success=true` and `found=false` — this is not an error condition. Only invalid connection or provider errors set `success=false`.
- The vector field is always returned for direct gets (unlike search, which requires `withVectors=true`).

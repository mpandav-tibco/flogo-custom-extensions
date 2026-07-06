# Get Document

Retrieve a single document by its ID from Elasticsearch 8.x.

## Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **VectorDB Connection** | Yes | — | The elasticsearch-connector connection |

## Input

| Field | Type | Description |
|-------|------|-------------|
| `collectionName` | string | Collection containing the document |
| `documentId` | string | Unique document ID |

## Output

| Field | Type | Description |
|-------|------|-------------|
| `success` | boolean | `true` if the document was found |
| `document` | object | The retrieved document (see schema below) |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` or document not found |

### Document Schema

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Document ID |
| `content` | string | Stored text content |
| `payload` | object | Metadata key-value pairs |
| `vector` | array\<number\> | Embedding vector (only if `withVectors=true` at upsert time — provider-dependent) |

## Behavior

- Returns `ErrCodeDocumentNotFound` (wrapped in `error` field) if the ID does not exist.

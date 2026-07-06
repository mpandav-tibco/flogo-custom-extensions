# Upsert Documents

Insert or update documents with pre-computed embedding vectors in Azure AI Search.

Uses Azure AI Search batch index documents API. Metadata stored as JSON string in a single `Edm.String` field.

## Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **VectorDB Connection** | Yes | — | The azureaisearch-connector connection |

## Input

| Field | Type | Description |
|-------|------|-------------|
| `collectionName` | string | Target collection |
| `documents` | array\<object\> | Documents to upsert (see schema below) |

### Document Schema

```json
{
  "id": "doc-001",
  "content": "The document text",
  "vector": [0.1, 0.2, 0.3, ...],
  "payload": { "category": "tech", "year": 2024 }
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `id` | Yes | Unique document identifier |
| `content` | No | Text content (stored alongside the vector) |
| `vector` | Yes | Pre-computed dense embedding. Must match collection dimensions. |
| `payload` | No | Key-value metadata stored with the document |

## Output

| Field | Type | Description |
|-------|------|-------------|
| `success` | boolean | `true` if all documents were upserted |
| `upsertedCount` | integer | Number of documents upserted |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

## Behavior

- Use **Ingest Documents** instead if you want to auto-embed raw text in one step.
- The `vector` dimension must match the collection's configured dimension.

# Upsert Documents

Insert or update documents (pre-computed vectors + metadata) in a VectorDB collection. Use this activity when you already have the embedding vectors and want to store them directly, without generating embeddings.

## Settings

| Setting | Required | Default | Description |
|---|---|---|---|
| **VectorDB Connection** | Yes | — | The VectorDB connector (Qdrant, Weaviate, Chroma, Milvus) |
| **Default Collection** | No | — | Fallback collection name when not provided at runtime |

## Input

| Field | Type | Description |
|---|---|---|
| `collectionName` | string | Target collection. Overrides Default Collection. |
| `documents` | array\<object\> | Documents to upsert (see schema below) |

### Document Schema

```json
{
  "id": "unique-doc-id",
  "content": "Source text of the document",
  "vector": [0.12, -0.34, 0.56, ...],
  "payload": { "category": "tech", "year": 2024 }
}
```

| Field | Required | Description |
|---|---|---|
| `id` | Yes | Unique document identifier. Upserting with an existing ID updates the document. |
| `vector` | Yes | Pre-computed dense embedding vector. Length must match collection dimensions. |
| `content` | No | Source text stored alongside the vector |
| `payload` | No | Key-value metadata stored with the document |

## Output

| Field | Type | Description |
|---|---|---|
| `success` | boolean | `true` if all documents were upserted |
| `upsertedCount` | integer | Number of documents upserted |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

## Notes

- **Upsert semantics**: If a document with the same `id` already exists, it is fully replaced (vector, content, and payload). There is no partial-update / patch operation.
- **Batching**: All documents in the input array are sent in a single batch call to the provider. Large batches may need to be split in the flow.
- If you need to generate embeddings from raw text, use **Ingest Documents** instead.

## When to Use vs Ingest Documents

| Scenario | Use |
|---|---|
| You have raw text, no vectors yet | **Ingest Documents** |
| You already have computed vectors | **Upsert Documents** |
| You want embedding + store in one step | **Ingest Documents** |

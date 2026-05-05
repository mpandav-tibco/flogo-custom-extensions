# Scroll Documents

Paginate through all documents in a collection without a query vector. Ideal for batch processing, re-indexing, export, or iterating over a memory store.

## Settings

| Setting | Required | Default | Description |
|---|---|---|---|
| **VectorDB Connection** | Yes | — | The VectorDB connector (Qdrant, Weaviate, Chroma, Milvus) |
| **Default Collection** | No | — | Fallback collection name |

## Input

| Field | Type | Default | Description |
|---|---|---|---|
| `collectionName` | string | — | Target collection. Overrides Default Collection. |
| `limit` | integer | `100` | Number of documents per page |
| `offset` | string | `""` | Cursor from `nextOffset` of the previous call. Empty = start from beginning. |
| `filters` | object | — | Key-value metadata filter. Empty = return all documents. |
| `withVectors` | boolean | `false` | Include the embedding vector in each document. Increases payload size significantly. |

## Output

| Field | Type | Description |
|---|---|---|
| `success` | boolean | `true` if the page was retrieved successfully |
| `documents` | array\<object\> | Documents in this page (see schema below) |
| `nextOffset` | string | Cursor for the next page. Empty string when no more pages remain. |
| `total` | integer | Total number of matching documents in the collection. `-1` when the provider does not report a total count (Qdrant, Weaviate, Milvus). Only Chroma populates this field. |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

### Document Schema

| Field | Type | Description |
|---|---|---|
| `id` | string | Document ID |
| `content` | string | Source text |
| `vector` | array\<number\> | Embedding vector (only populated when `withVectors=true`) |
| `payload` | object | Metadata key-value pairs |

## Pagination Pattern

Use `nextOffset` to implement cursor-based pagination in a loop:

```
ScrollDocuments (offset="")
  → Process documents
  → if nextOffset != "" → ScrollDocuments (offset=nextOffset)
  → else → Done
```

## Provider Notes

| Provider | Pagination | Notes |
|---|---|---|
| Qdrant | Native server-side cursor | Most efficient; stateless cursor |
| Weaviate | Native cursor-based | Efficient for large collections |
| Milvus | Native offset-based | Server-side limit/offset |
| **Chroma** | **Client-side** | Fetches entire collection into memory, then slices. **Avoid for large collections.** |

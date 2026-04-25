# Count Documents

Count the number of documents in a VectorDB collection, with optional metadata filtering.

## Settings

| Setting | Required | Default | Description |
|---|---|---|---|
| **VectorDB Connection** | Yes | — | The VectorDB connector (Qdrant, Weaviate, Chroma, Milvus) |
| **Default Collection** | No | — | Fallback collection name when not provided at runtime |

## Input

| Field | Type | Description |
|---|---|---|
| `collectionName` | string | Name of the collection to count. Overrides Default Collection. |
| `filters` | object | Key-value filter map. Empty object = count all. |

### Filter Examples

```json
{}
```
Count all documents.

```json
{ "category": "tech", "year": 2024 }
```
Count only documents where `category == "tech"` AND `year == 2024`.

## Output

| Field | Type | Description |
|---|---|---|
| `success` | boolean | `true` if the operation completed without error |
| `count` | integer | Total number of matching documents |
| `duration` | string | Elapsed wall-clock time (e.g. `"12ms"`) |
| `error` | string | Error message if `success` is `false` |

## Provider Notes

| Provider | Filtered Count |
|---|---|
| Qdrant | Native server-side count with filter |
| Weaviate | **Not supported** — returns an error when `filters` is non-empty. Omit `filters` to count all documents. |
| Chroma | Client-side: fetches matching document IDs then counts |
| Milvus | Native `Count` with boolean expression |

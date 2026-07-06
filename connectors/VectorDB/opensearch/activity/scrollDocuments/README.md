# Scroll Documents

Paginate through all documents in a OpenSearch 2.x collection without a query vector.

Uses OpenSearch `from`/`size` pagination. **Important**: default refresh interval is 1 second — allow at least 1–2s after bulk operations before counting or querying.

## Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **VectorDB Connection** | Yes | — | The opensearch-connector connection |

## Input

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `collectionName` | string | — | Collection to scroll |
| `limit` | integer | `100` | Max documents per page |
| `offset` | string | `""` | Pagination cursor (empty = first page) |
| `withVectors` | boolean | `false` | Include embedding vectors in results |

## Output

| Field | Type | Description |
|-------|------|-------------|
| `success` | boolean | `true` if the scroll completed |
| `documents` | array\<object\> | Documents in this page |
| `nextOffset` | string | Cursor for the next page (empty = last page) |
| `total` | integer | Total document count in the collection |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

## Pagination Pattern

```
offset = ""
loop:
  result = ScrollDocuments(collectionName, limit=100, offset=offset)
  process(result.documents)
  if result.nextOffset == "": break
  offset = result.nextOffset
```

# Delete Documents

Delete documents from a VectorDB collection by explicit ID list, by metadata filter, or both.

## Settings

| Setting | Required | Default | Description |
|---|---|---|---|
| **VectorDB Connection** | Yes | — | The VectorDB connector (Qdrant, Weaviate, Chroma, Milvus) |
| **Default Collection** | No | — | Fallback collection name when not provided at runtime |

## Input

| Field | Type | Description |
|---|---|---|
| `collectionName` | string | Target collection. Overrides Default Collection. |
| `ids` | array\<string\> | List of document IDs to delete. Empty array = skip ID-based delete. |
| `filters` | object | Key-value filter map. Matching documents are deleted. Empty object = skip filter-based delete. |

### Deletion Modes

| `ids` | `filters` | Behavior |
|---|---|---|
| Populated | Empty | Delete exactly the specified IDs |
| Empty | Populated | Delete all documents matching the filter |
| Both populated | Both populated | ID-list takes priority — only the ID delete is executed; the filter is ignored |
| Both empty | Both empty | Error — at least one of `ids` or `filters` must be provided |

### Filter Example

```json
{ "category": "outdated", "year": 2020 }
```

## Output

| Field | Type | Description |
|---|---|---|
| `success` | boolean | `true` if the operation completed without error |
| `deletedCount` | integer | Number of documents deleted (`-1` if the provider cannot report exact count) |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

## Provider Notes

| Provider | Filter Delete | Exact Count |
|---|---|---|
| Qdrant | Native filter delete | Yes |
| Weaviate | Supported — GraphQL batch delete with `where` filter | No (returns `-1`) |
| Chroma | Supported | No (returns `-1`) |
| Milvus | Boolean expression filter | Yes |

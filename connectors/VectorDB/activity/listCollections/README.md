# List Collections

List all collections (indexes) available in the connected VectorDB instance.

## Settings

| Setting | Required | Default | Description |
|---|---|---|---|
| **VectorDB Connection** | Yes | — | The VectorDB connector (Qdrant, Weaviate, Chroma, Milvus) |

## Input

This activity has no runtime inputs.

## Output

| Field | Type | Description |
|---|---|---|
| `success` | boolean | `true` if the list was retrieved successfully |
| `collections` | array\<string\> | Names of all collections in the database |
| `totalCount` | integer | Total number of collections returned |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

## Notes

- Use this activity to verify that a collection exists before operating on it, or to build dynamic flows that enumerate collections.
- Collection names are returned in provider-defined order (typically alphabetical or insertion order).

# Delete Collection

Permanently delete a VectorDB collection and all documents it contains. This operation is irreversible.

## Settings

| Setting | Required | Default | Description |
|---|---|---|---|
| **VectorDB Connection** | Yes | — | The VectorDB connector (Qdrant, Weaviate, Chroma, Milvus) |

## Input

| Field | Type | Description |
|---|---|---|
| `collectionName` | string | Name of the collection to delete |

## Output

| Field | Type | Description |
|---|---|---|
| `success` | boolean | `true` if the collection was deleted |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

## Notes

- Deleting a non-existent collection returns `success=false` with an error. Use **List Collections** or check for the error message to make flows idempotent.
- All documents, vectors, and metadata stored in the collection are permanently removed.

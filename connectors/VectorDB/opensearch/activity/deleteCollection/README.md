# Delete Collection

Permanently delete a vector collection and all its documents from OpenSearch 2.x.

## Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **VectorDB Connection** | Yes | — | The opensearch-connector connection |

## Input

| Field | Type | Description |
|-------|------|-------------|
| `collectionName` | string | Name of the collection to delete |

## Output

| Field | Type | Description |
|-------|------|-------------|
| `success` | boolean | `true` if the collection was deleted |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

## Behavior

- This operation is **irreversible** — all documents in the collection are permanently deleted.
- If the collection does not exist, the activity returns `success=false` with `ErrCodeCollectionNotFound`.

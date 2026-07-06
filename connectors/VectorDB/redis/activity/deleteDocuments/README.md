# Delete Documents

Delete one or more documents by ID from Redis Stack (RediSearch).

## Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **VectorDB Connection** | Yes | — | The redis-connector connection |

## Input

| Field | Type | Description |
|-------|------|-------------|
| `collectionName` | string | Collection containing the documents |
| `ids` | array\<string\> | List of document IDs to delete |

## Output

| Field | Type | Description |
|-------|------|-------------|
| `success` | boolean | `true` if the delete completed without error |
| `deletedCount` | integer | Number of documents deleted |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

## Behavior

- IDs that do not exist are silently ignored — the operation succeeds if the deletion call completes.
- To delete by metadata filter instead of by ID, use **Delete By Filter**.

# List Collections

Return the names of all vector collections in the LanceDB database.

## Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **VectorDB Connection** | Yes | — | The lancedb-connector connection |

## Input

No input fields required.

## Output

| Field | Type | Description |
|-------|------|-------------|
| `success` | boolean | `true` if the list was retrieved successfully |
| `collections` | array\<string\> | Names of all collections |
| `count` | integer | Number of collections |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

## Behavior

- System indexes or internal collections are filtered out where applicable.
- Returns an empty list (not an error) if no collections exist.

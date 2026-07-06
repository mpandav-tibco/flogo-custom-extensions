# Count Documents

Count the number of documents in a pgvector (PostgreSQL) collection, with optional metadata filtering.

## Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **VectorDB Connection** | Yes | — | The pgvector-connector connection |

## Input

| Field | Type | Description |
|-------|------|-------------|
| `collectionName` | string | Collection to count |
| `filters` | object | Optional metadata filter (same syntax as Vector Search) |

## Output

| Field | Type | Description |
|-------|------|-------------|
| `success` | boolean | `true` if count succeeded |
| `count` | integer | Number of matching documents |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

## Filter Syntax

JSONB path queries — all operators fully type-aware (numeric ranges natively supported)

```json
{ "category": "tech", "year": { "$gte": 2023 } }
```

## Behavior

- With no `filters`, returns the total document count for the collection.
- Filter semantics: JSONB path queries — all operators fully type-aware (numeric ranges natively supported).

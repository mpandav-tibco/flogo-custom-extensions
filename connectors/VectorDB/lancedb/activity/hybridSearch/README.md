# Hybrid Search

Combined dense vector + keyword search in LanceDB with configurable alpha blend. Returns results fused from both retrieval modes.

**Implementation**: RRF (Reciprocal Rank Fusion) combining dense vector search and full-text search (FTS). FTS falls back to vector-only if index not yet built. `alpha = 1.0` = pure vector, `0.0` = pure text.

## Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **VectorDB Connection** | Yes | — | The lancedb-connector connection |

## Input

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `collectionName` | string | — | Target collection |
| `queryText` | string | — | Keyword query for the text search leg |
| `queryVector` | array\<number\> | — | Dense embedding for the vector search leg |
| `topK` | integer | `10` | Number of results to return |
| `alpha` | number | `0.5` | Blend weight: `1.0` = pure vector, `0.0` = pure keyword |
| `filters` | object | — | Metadata filter applied to both search legs |
| `scoreThreshold` | number | `0.0` | Minimum score filter |

## Output

| Field | Type | Description |
|-------|------|-------------|
| `success` | boolean | `true` if search completed |
| `results` | array\<object\> | Fused ranked results |
| `totalCount` | integer | Number of results returned |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

## Alpha Tuning

| `alpha` | Behaviour |
|---------|-----------|
| `1.0` | Pure vector (semantic similarity) |
| `0.5` | Balanced — recommended starting point |
| `0.0` | Pure keyword (BM25 / text match) |

## Behavior

RRF (Reciprocal Rank Fusion) combining dense vector search and full-text search (FTS). FTS falls back to vector-only if index not yet built. `alpha = 1.0` = pure vector, `0.0` = pure text.

Use **Vector Search** instead if only semantic similarity is needed.

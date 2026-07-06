# Vector Search

Semantic ANN similarity search in OpenSearch 2.x using a dense query vector. Returns the most similar documents ranked by score.

## Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **VectorDB Connection** | Yes | — | The opensearch-connector connection |
| **Default Collection** | No | — | Fallback collection name |
| **Default Top-K** | No | `10` | Default results when `topK` input is `0` |

## Input

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `collectionName` | string | — | Target collection |
| `queryVector` | array\<number\> | — | Dense query embedding — must match collection dimensions |
| `topK` | integer | `0` | Max results (`0` = use Default Top-K) |
| `scoreThreshold` | number | `0.0` | Minimum similarity score (`0.0` = no filter) |
| `filters` | object | — | Metadata pre-filter (provider: OpenSearch 2.x) |
| `withVectors` | boolean | `false` | Include stored vectors in results |

## Output

| Field | Type | Description |
|-------|------|-------------|
| `success` | boolean | `true` if search completed |
| `results` | array\<object\> | Ranked result documents |
| `totalCount` | integer | Number of results returned |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

### Result Document Schema

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Document ID |
| `score` | number | Similarity score (higher = more similar) |
| `content` | string | Source text |
| `payload` | object | Metadata key-value pairs |

## Filter Syntax

OpenSearch `bool/must` queries mapped from MongoDB-style operators

```json
{ "category": "tech", "language": "en" }
```

## Flow Pattern

```
Create Embeddings (query text)
  → Vector Search (queryVector = embedding output)
  → Return / format results
```

Or use **RAG Query** to combine embedding + search + context formatting in one activity.

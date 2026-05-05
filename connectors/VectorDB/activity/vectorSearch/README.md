# Vector Search

Semantic similarity (ANN) search in a VectorDB collection using a dense query vector. Returns the most similar documents ranked by score.

## Settings

| Setting | Required | Default | Description |
|---|---|---|---|
| **VectorDB Connection** | Yes | — | The VectorDB connector (Qdrant, Weaviate, Chroma, Milvus) |
| **Default Collection** | No | — | Fallback collection name |
| **Default Top-K** | No | `10` | Default number of results when `topK` input is `0` |

## Input

| Field | Type | Default | Description |
|---|---|---|---|
| `collectionName` | string | — | Target collection. Overrides Default Collection. |
| `queryVector` | array\<number\> | — | Dense query embedding. Must match the collection's vector dimension. |
| `topK` | integer | `0` | Max results to return. `0` = use Default Top-K. |
| `scoreThreshold` | number | `0.0` | Minimum similarity score (0–1). `0.0` = no filter. |
| `filters` | object | — | Metadata pre-filter. Only documents matching the filter are searched. |
| `withVectors` | boolean | `false` | Include the stored embedding vector in each result |

### Filter Example

```json
{ "category": "tech", "language": "en" }
```

## Output

| Field | Type | Description |
|---|---|---|
| `success` | boolean | `true` if the search completed without error |
| `results` | array\<object\> | Ranked result documents (see schema below) |
| `totalCount` | integer | Number of results returned |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

### Result Document Schema

| Field | Type | Description |
|---|---|---|
| `id` | string | Document ID |
| `score` | number | Similarity score. Higher = more similar. |
| `content` | string | Source text |
| `payload` | object | Metadata key-value pairs |

## Score Interpretation

| Metric | Score Range | Interpretation |
|---|---|---|
| Cosine | 0–1 | 1 = identical direction, 0 = orthogonal |
| Dot Product | unbounded | Higher = more similar |
| Euclidean (L2) | ≥ 0 | Lower distance = more similar (score = 1 - normalized distance) |

## Flow Pattern

```
Create Embeddings (query text → vector)
  → Vector Search (queryVector = embedding output)
  → Return / format results
```

Or use **RAG Query** for embedding + search in one activity.

## Notes

- If you need keyword boosting alongside vector similarity, use **Hybrid Search** instead.
- Use `scoreThreshold` to filter out low-relevance results before passing them to an LLM.
- Set `withVectors=true` only when you need to inspect or re-use the stored vectors (increases response size).

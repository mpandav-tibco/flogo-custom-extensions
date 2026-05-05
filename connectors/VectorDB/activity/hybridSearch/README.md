# Hybrid Search

Combine dense vector similarity with BM25 keyword search for improved recall, especially on queries where exact keyword matching matters alongside semantic similarity.

## Settings

| Setting | Required | Default | Description |
|---|---|---|---|
| **VectorDB Connection** | Yes | — | The VectorDB connector |
| **Default Collection** | No | — | Fallback collection name |
| **Default Top-K** | No | `10` | Default number of results when `topK` input is `0` |

## Input

| Field | Type | Default | Description |
|---|---|---|---|
| `collectionName` | string | — | Target collection. Overrides Default Collection. |
| `queryText` | string | — | Keyword query for BM25 / sparse search side |
| `queryVector` | array\<number\> | — | Dense query vector for similarity search side |
| `topK` | integer | `0` | Max results to return. `0` = use Default Top-K setting. |
| `scoreThreshold` | number | `0.0` | Minimum score filter. `0.0` = no threshold. |
| `alpha` | number | `0.5` | Blend ratio: `1.0` = pure vector, `0.0` = pure keyword, `0.5` = balanced |
| `filters` | object | — | Metadata pre-filter applied before ranking |

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
| `score` | number | Combined similarity score (0–1) |
| `content` | string | Source text |
| `payload` | object | Metadata key-value pairs |

## Provider Support

| Provider | Hybrid Search |
|---|---|
| **Qdrant** | Native sparse+dense hybrid search via `Query` API |
| **Weaviate** | Native BM25 + vector hybrid via `nearVector + bm25` |
| **Chroma** | **Not supported** — falls back to pure vector search (warning logged) |
| **Milvus** | **Not supported** — falls back to pure vector search (warning logged) |

> When a provider does not support hybrid search, the activity logs a warning and automatically falls back to **vector-only** search using `queryVector`. Ensure `queryVector` is always populated when using this activity.

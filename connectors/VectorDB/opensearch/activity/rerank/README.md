# Rerank

Cross-encoder reranking of a candidate document set for improved retrieval precision. Reranks documents by semantic relevance to the query — this activity does not interact with OpenSearch 2.x directly.

## Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **Rerank Endpoint** | Yes | — | Reranking API URL (e.g. `https://api.cohere.ai/v1/rerank` or `https://api.jina.ai/v1/rerank`) |
| **Model** | No | `rerank-english-v3.0` | Reranker model name |
| **Top-N** | No | `5` | Number of top documents to return after reranking |
| **Timeout (s)** | No | `30` | HTTP timeout for the rerank call |

## Input

| Field | Type | Description |
|-------|------|-------------|
| `query` | string | The user query |
| `documents` | array\<string\> | Candidate document texts to rerank |
| `apiKey` | string | API key (can override the connection-level key) |

## Output

| Field | Type | Description |
|-------|------|-------------|
| `success` | boolean | `true` if reranking succeeded |
| `results` | array\<object\> | Reranked documents with scores (see schema below) |
| `totalCount` | integer | Number of documents returned |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

### Result Schema

| Field | Type | Description |
|-------|------|-------------|
| `index` | integer | Original position in the input `documents` array |
| `score` | number | Cross-encoder relevance score (higher = more relevant) |
| `document` | string | Document text |

## Two-Stage Retrieval Pattern

```
Vector Search (topK=20, low threshold)  ← broad recall
  → Rerank (topN=5)                     ← precision boost
  → LLM Activity                        ← generation
```

Reranking is significantly slower than vector search but improves precision for LLM grounding. Use it when retrieval quality is critical.

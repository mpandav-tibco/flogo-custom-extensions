# Rerank Documents

Re-rank a list of candidate documents by their relevance to a query using a cross-encoder reranker API (Cohere, Jina, or any compatible endpoint). Use after **Vector Search** or **RAG Query** to improve precision before passing context to an LLM.

## Settings

| Setting | Required | Default | Description |
|---|---|---|---|
| **Rerank API Endpoint** | Yes | — | Full URL of the rerank API (e.g. `https://api.cohere.ai/v1/rerank`) |
| **API Key** | No | — | Bearer token for the rerank API |
| **Model** | No | `rerank-english-v3.0` | Reranker model name |
| **Top N** | No | `5` | Return only the top-N documents after reranking |
| **Timeout (s)** | No | `30` | HTTP request timeout |

### Provider Endpoints

| Provider | Endpoint |
|---|---|
| Cohere | `https://api.cohere.ai/v1/rerank` |
| Jina AI | `https://api.jina.ai/v1/rerank` |
| Custom / Self-hosted | Your endpoint |

## Input

| Field | Type | Description |
|---|---|---|
| `query` | string | The query string to score documents against |
| `documents` | array\<object\> | Documents to rerank. Each item must have a `text` field. |

### Document Input Schema

```json
[
  { "text": "Document content", "id": "doc1", "score": 0.87 },
  { "text": "Another document", "id": "doc2", "score": 0.72 }
]
```

> Extra fields (e.g. `id`, `score`, `payload`) are preserved in the output `document` objects.

## Output

| Field | Type | Description |
|---|---|---|
| `success` | boolean | `true` if reranking completed without error |
| `rankedDocuments` | array\<object\> | Reranked documents (see schema below) |
| `totalCount` | integer | Number of documents returned |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

### Ranked Document Schema

| Field | Type | Description |
|---|---|---|
| `index` | integer | Original position in the input `documents` array |
| `score` | number | Relevance score assigned by the cross-encoder |
| `document` | object | The original document object (with `text` and any extra fields) |

## Flow Pattern

```
RAG Query / Vector Search
  → Rerank Documents  (top-20 → top-5 high-precision)
  → LLM Activity      (use top-N rankedDocuments as context)
```

## Notes

- Reranking is a two-stage retrieval improvement: retrieve broadly with ANN search, then re-score narrowly with a cross-encoder.
- Only the top-N documents (controlled by **Top N** setting) are returned, reducing the context size sent to the LLM.
- The `index` field lets you trace each ranked result back to its original position in the input.

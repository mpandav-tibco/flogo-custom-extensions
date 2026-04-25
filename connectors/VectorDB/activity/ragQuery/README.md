# RAG Query

Full Retrieval-Augmented Generation (RAG) pipeline in a single activity: embed query text → semantic vector search → format retrieved context for an LLM prompt.

## Settings

| Setting | Required | Default | Description |
|---|---|---|---|
| **VectorDB Connection** | Yes | — | VectorDB connector used for retrieval |
| **Use Connector Embedding Settings** | No | `false` | Inherit embedding provider, API key, and base URL from the VectorDB connection. When enabled, only **Embedding Model** needs to be set below. Requires *Configure Embedding Provider* to be enabled on the connection. |
| **Embedding Provider** | No | `OpenAI` | `OpenAI`, `Azure OpenAI`, `Cohere`, `Ollama`, `Custom`. Ignored when *Use Connector Embedding Settings* is enabled. |
| **Embedding API Key** | No | — | API key for embedding. Not required for Ollama. Ignored when *Use Connector Embedding Settings* is enabled. |
| **Embedding Base URL** | No | — | Override provider URL. Ignored when *Use Connector Embedding Settings* is enabled. |
| **Embedding Model** | Yes | `text-embedding-3-small` | Must match the model used during ingestion |
| **Embedding Dimensions** | No | `0` | `0` = model default. Must match the collection's dimension. |
| **Default Collection** | No | — | Fallback collection when not provided at runtime |
| **Default Top-K** | No | `5` | Default number of documents to retrieve |
| **Score Threshold** | No | `0.0` | Minimum similarity score. `0.0` = no filter. |
| **Content Field** | No | `text` | Payload field containing the document text (used in `formattedContext`) |
| **Context Format** | No | `numbered` | Output format: `numbered`, `markdown`, `xml`, `plain`, `json` |
| **Use Hybrid Search** | No | `false` | Enable hybrid (BM25 + dense vector) search. Only Weaviate and Qdrant support native hybrid; other providers fall back to dense vector search. |
| **Hybrid Alpha** | No | `0.5` | Blend weight when *Use Hybrid Search* is enabled: `1.0` = pure vector, `0.0` = pure keyword, `0.5` = balanced. |
| **Timeout (s)** | No | `30` | Total timeout (embedding + search) |

### Context Format Examples

| Format | Example Output |
|---|---|
| `numbered` | `1. Text of document one\n2. Text of document two` |
| `markdown` | `**[1]** *(score: 0.9200)*\n\nText of document one\n\n---` |
| `xml` | `<context><document id="1" score="0.9200">Text</document></context>` |
| `plain` | Raw text separated by newlines |
| `json` | `[{"index":1,"id":"doc1","score":0.92,"content":"Text","payload":{...}}]` |

> **Flogo tip**: Use `json` format when you need to access individual result fields (score, id, payload) further in the flow. The output is a JSON array string — use a **JSON Parse** or mapper expression to work with it natively.

## Input

| Field | Type | Default | Description |
|---|---|---|---|
| `queryText` | string | — | The user's question or search query |
| `collectionName` | string | — | Target collection. Overrides Default Collection. |
| `topK` | integer | `0` | Max documents to retrieve. `0` = use Default Top-K. |
| `filters` | object | — | Metadata pre-filter applied before retrieval |

## Output

| Field | Type | Description |
|---|---|---|
| `success` | boolean | `true` if the pipeline completed without error |
| `formattedContext` | string | Ready-to-use context string for LLM prompt injection |
| `sourceDocuments` | array\<object\> | Retrieved documents with scores (see schema below) |
| `queryEmbedding` | array\<number\> | The query embedding vector (useful for debugging) |
| `totalFound` | integer | Number of documents retrieved |
| `duration` | string | Total elapsed time |
| `error` | string | Error message if `success` is `false` |

### Source Document Schema

| Field | Type | Description |
|---|---|---|
| `id` | string | Document ID |
| `score` | number | Similarity score (0–1) |
| `content` | string | Source text |
| `payload` | object | Metadata key-value pairs |

## Flow Pattern

```
HTTP Trigger (user question)
  → RAG Query (embed + retrieve + format)
  → LLM Activity (prompt = "Answer using: " + formattedContext)
  → Return answer
```

## Notes

- `formattedContext` is designed to be injected directly into an LLM system or user prompt.
- Use `sourceDocuments` for citation, explainability, or re-ranking with the **Rerank Documents** activity.
- The activity uses the same embedding model as **Ingest Documents** — ensure model and dimension settings match.

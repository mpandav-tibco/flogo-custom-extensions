# RAG Query

Unified Retrieval-Augmented Generation (RAG) pipeline in a single activity. Operates in two modes controlled by the **Enable LLM Generation** toggle:

| Mode | Toggle | What happens |
|---|---|---|
| **Pure Retrieval** | `false` (default) | Embed query → vector/hybrid search → format context. `answer` is empty. Wire `formattedContext` into your own LLM activity downstream. |
| **LLM-Assisted Generation** | `true` | All of the above, then calls a configured LLM to generate a grounded answer. `answer` is populated. No extra activity needed. |

Both modes produce the same `formattedContext`, `sourceDocuments`, and `queryEmbedding` outputs, so flows are easy to switch between modes.

## Settings

### Retrieval

| Setting | Required | Default | Description |
|---|---|---|---|
| **VectorDB Connection** | Yes | — | VectorDB connector used for retrieval |
| **Use Connector Embedding Settings** | No | `false` | Inherit embedding provider, API key, and base URL from the VectorDB connection. When enabled, only **Embedding Model** needs to be set below. Requires *Configure Embedding Provider* to be enabled on the connection. |
| **Embedding Provider** | No | `OpenAI` | `OpenAI`, `Azure OpenAI`, `Cohere`, `Ollama`, `Custom`. Hidden when *Use Connector Embedding Settings* is enabled. |
| **Embedding API Key** | No | — | API key for embedding. Not required for Ollama. Hidden when *Use Connector Embedding Settings* is enabled. |
| **Embedding Base URL** | No | — | Override provider URL. Hidden when *Use Connector Embedding Settings* is enabled. |
| **Embedding Model** | Yes | `text-embedding-3-small` | Must match the model used during ingestion |
| **Embedding Dimensions** | No | `0` | `0` = model default. Must match the collection's dimension. |
| **Default Collection** | No | — | Fallback collection when not provided at runtime |
| **Default Top-K** | No | `5` | Default number of documents to retrieve |
| **Score Threshold** | No | `0.0` | Minimum similarity score. `0.0` = no filter. |
| **Content Field** | No | `text` | Payload field containing the document text (used in `formattedContext`) |
| **Context Format** | No | `numbered` | Output format: `numbered`, `markdown`, `xml`, `plain`, `json` |
| **Use Hybrid Search** | No | `false` | Enable hybrid (BM25 + dense vector) search. Only Weaviate and Qdrant support native hybrid; other providers fall back to dense vector search. |
| **Hybrid Alpha** | No | `0.5` | Visible only when *Use Hybrid Search* is enabled. Blend weight: `1.0` = pure vector, `0.0` = pure keyword, `0.5` = balanced. |
| **Timeout (s)** | No | `30` | Total timeout covering embedding + search + (when enabled) LLM generation |

### LLM Generation

These settings are only visible in the UI when **Enable LLM Generation** is `true`.

| Setting | Required | Default | Description |
|---|---|---|---|
| **Enable LLM Generation** | No | `false` | Toggle between pure retrieval and LLM-assisted generation |
| **LLM Provider** | No | `Ollama` | `Ollama`, `OpenAI`, `Azure OpenAI`, `Custom`. Ollama calls `/api/generate`; others call `/v1/chat/completions`. |
| **LLM Base URL** | No | `http://localhost:11434` | Base URL for the LLM API. Ollama default: `http://localhost:11434`. OpenAI: `https://api.openai.com`. |
| **LLM API Key** | No | — | API key for OpenAI or Azure OpenAI. Leave empty for Ollama. |
| **LLM Model** | No | `llama3.1:8b` | Model name for generation. Ollama: `llama3.1:8b`. OpenAI: `gpt-4o-mini`. |
| **System Prompt** | No | *see below* | Default instruction prompt prepended before context and question. Can be overridden at runtime via the `systemPrompt` input field. |
| **Max Tokens** | No | `1024` | Maximum tokens in the generated answer. Only applied for OpenAI/Azure/Custom providers. |
| **Temperature** | No | `0.1` | Sampling temperature (`0.0` = deterministic). Only applied for OpenAI/Azure/Custom providers. |

**Default system prompt:**
```
You are a helpful assistant. Answer the question using only the provided context.
If the context does not contain enough information, say so.
```

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

| Field | Type | Description |
|---|---|---|
| `queryText` | string | The user's question or search query |
| `collectionName` | string | Target collection. Overrides *Default Collection* setting. |
| `topK` | integer | Max documents to retrieve. `0` = use *Default Top-K* setting. |
| `filters` | object | Metadata pre-filter applied before retrieval |
| `systemPrompt` | string | Per-request system prompt override. When non-empty, replaces the design-time *System Prompt* setting. Only effective when *Enable LLM Generation* is `true`. |

## Output

| Field | Type | Description |
|---|---|---|
| `success` | boolean | `true` if the pipeline completed without error |
| `answer` | string | LLM-generated answer. Populated only when *Enable LLM Generation* is `true`; empty string otherwise. |
| `formattedContext` | string | Retrieved context formatted for LLM prompt injection. Always populated. |
| `sourceDocuments` | array\<object\> | Retrieved documents with scores (see schema below) |
| `queryEmbedding` | array\<number\> | The query embedding vector (useful for debugging) |
| `totalFound` | integer | Number of documents retrieved |
| `duration` | string | Total elapsed time (embedding + search + optional LLM generation) |
| `error` | string | Error message if `success` is `false` |

### Source Document Schema

| Field | Type | Description |
|---|---|---|
| `id` | string | Document ID |
| `score` | number | Similarity score (0–1) |
| `content` | string | Source text |
| `payload` | object | Metadata key-value pairs |

## Flow Patterns

### Pure Retrieval (Enable LLM Generation = false)

Use this when you want to control the LLM step yourself — custom prompt engineering, a different LLM activity, or passing context to multiple downstream steps.

```
HTTP Trigger (user question)
  → RAG Query  [enableLLMGenerate: false]
      ↓ formattedContext
  → LLM Activity  (your own prompt: "Answer using:\n" + formattedContext)
  → Return answer
```

### LLM-Assisted Generation (Enable LLM Generation = true)

Use this for a self-contained RAG endpoint — retrieval and generation in one step.

```
HTTP Trigger (user question)
  → RAG Query  [enableLLMGenerate: true, llmProvider: Ollama, llmModel: llama3.1:8b]
      ↓ answer          (ready to return)
      ↓ formattedContext (available for logging/citations)
      ↓ sourceDocuments  (available for citations/re-ranking)
  → Return answer
```

> **LLM failure behaviour**: if the LLM call fails (timeout, model error, etc.) the activity does **not** fault. `answer` will contain an error summary prefixed with `[LLM generation failed: ...]` and `formattedContext` / `sourceDocuments` are still populated, so the flow can gracefully degrade.

## Notes

- `formattedContext` is always populated regardless of mode — it can be used for citations, re-ranking, or logging even when `answer` is returned directly.
- Use `sourceDocuments` with the **Rerank Documents** activity to improve retrieval precision before generation.
- The activity uses the same embedding model as **Ingest Documents** — ensure model and dimension settings match.
- The `systemPrompt` input field allows per-request prompt customisation without redeploying — useful for multi-tenant or multi-persona scenarios.

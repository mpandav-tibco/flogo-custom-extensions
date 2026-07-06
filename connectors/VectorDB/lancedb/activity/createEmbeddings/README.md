# Create Embeddings

Generate dense vector embeddings from text using a configurable embedding provider. This activity is provider-agnostic — it does not interact with LanceDB directly.

## Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **VectorDB Connection** | Yes | — | Used only when **Use Connector Embedding Settings** is `true` |
| **Use Connector Embedding Settings** | No | `false` | Inherit provider, API key, and base URL from the connection |
| **Provider** | No | `OpenAI` | `OpenAI`, `Azure OpenAI`, `Cohere`, `Ollama` |
| **API Key** | No | — | Embedding provider API key |
| **Base URL** | No | — | Override provider endpoint |
| **Model** | No | `text-embedding-3-small` | Embedding model name |
| **Dimensions** | No | `0` | `0` = model default |
| **Timeout (s)** | No | `30` | HTTP timeout for the embedding call |

## Input

| Field | Type | Description |
|-------|------|-------------|
| `inputText` | string | Single text to embed (mutually exclusive with `inputTexts`) |
| `inputTexts` | array\<string\> | Multiple texts to embed in one batch |

## Output

| Field | Type | Description |
|-------|------|-------------|
| `success` | boolean | `true` if embedding succeeded |
| `embedding` | array\<number\> | Dense vector for `inputText` |
| `embeddings` | array\<array\<number\>> | Dense vectors for `inputTexts` (same order) |
| `dimensions` | integer | Vector dimension |
| `model` | string | Model used |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

## Provider Defaults

| Provider | Default Base URL | Default Model |
|----------|-----------------|---------------|
| OpenAI | `https://api.openai.com/v1` | `text-embedding-3-small` |
| Azure OpenAI | *(required — set Base URL)* | `text-embedding-3-small` |
| Cohere | `https://api.cohere.ai/v1` | `embed-english-v3.0` |
| Ollama | `http://localhost:11434` | `nomic-embed-text` |

## Flow Pattern

```
Create Embeddings (inputText = user query)
  → Vector Search (queryVector = embedding output)
```

Or use **Ingest Documents** / **RAG Query** which call the embedding API internally.

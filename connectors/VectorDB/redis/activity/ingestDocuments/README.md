# Ingest Documents

Embed raw text documents and store the resulting vectors in Redis Stack (RediSearch) in a single step. This is the recommended activity for RAG ingestion pipelines.

## Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **VectorDB Connection** | Yes | — | The redis-connector connection |
| **Use Connector Embedding Settings** | No | `false` | Inherit embedding config from the connection |
| **Embedding Provider** | No | `OpenAI` | `OpenAI`, `Azure OpenAI`, `Cohere`, `Ollama` |
| **Embedding API Key** | No | — | API key for the embedding provider |
| **Embedding Base URL** | No | — | Override provider URL |
| **Embedding Model** | No | `text-embedding-3-small` | Must match the model used at query time |
| **Embedding Dimensions** | No | `0` | `0` = model default |
| **Default Collection** | No | — | Fallback collection name |
| **Content Field** | No | `text` | Key inside each document that holds the text to embed |
| **Embedding Batch Size** | No | `100` | Texts per embedding API request |
| **Timeout (s)** | No | `60` | Total timeout for embedding + upsert |

## Input

| Field | Type | Description |
|-------|------|-------------|
| `collectionName` | string | Target collection |
| `documents` | array\<object\> | Documents to embed and store |

### Document Schema

```json
{
  "id": "doc-001",
  "text": "The document text to embed",
  "metadata": { "category": "tech" }
}
```

## Output

| Field | Type | Description |
|-------|------|-------------|
| `success` | boolean | `true` if all documents were embedded and stored |
| `ingestedCount` | integer | Number of documents successfully ingested |
| `ids` | array\<string\> | IDs of the ingested documents |
| `dimensions` | integer | Vector dimension used |
| `duration` | string | Total elapsed time |
| `error` | string | Error message if `success` is `false` |

## Behavior

- The embedding API call and VectorDB upsert happen in a single activity — no intermediate mapping needed.
- Auto-generates UUID v4 IDs for documents that omit the `id` field.
- The original text is stored in the payload under the **Content Field** key.

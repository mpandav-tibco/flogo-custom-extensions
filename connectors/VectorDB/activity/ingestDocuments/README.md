# Ingest Documents

Embed raw text documents and store the resulting vectors in a VectorDB collection in a single step. This is the recommended activity for RAG ingestion pipelines.

## Settings

| Setting | Required | Default | Description |
|---|---|---|---|
| **VectorDB Connection** | Yes | — | Target VectorDB connector (Qdrant, Weaviate, Chroma, Milvus) |
| **Use Connector Embedding Settings** | No | `false` | Inherit embedding provider, API key, and base URL from the VectorDB connection. When enabled, only **Embedding Model** needs to be set below. Requires *Configure Embedding Provider* to be enabled on the connection. |
| **Embedding Provider** | No | `OpenAI` | `OpenAI`, `Azure OpenAI`, `Cohere`, `Ollama`, `Custom`. Ignored when *Use Connector Embedding Settings* is enabled. |
| **Embedding API Key** | No | — | API key for the embedding provider. Not required for Ollama. Ignored when *Use Connector Embedding Settings* is enabled. |
| **Embedding Base URL** | No | — | Override provider URL (see [Create Embeddings](../createEmbeddings/README.md) for defaults). Ignored when *Use Connector Embedding Settings* is enabled. |
| **Embedding Model** | Yes | `text-embedding-3-small` | Must match the model used at query time |
| **Embedding Dimensions** | No | `0` | `0` = model default. Must match the collection's vector dimension. |
| **Default Collection** | No | — | Fallback collection when not provided at runtime |
| **Content Field** | No | `text` | The key inside each document object that holds the text to embed. Also stored in the payload under this key. |
| **Embedding Batch Size** | No | `100` | Number of texts sent to the embedding API per request. Reduce for providers with small payload limits or strict rate limits (e.g. `20` for free-tier OpenAI). |
| **Timeout (s)** | No | `60` | Total timeout covering embedding API call + VectorDB upsert |

## Input

| Field | Type | Description |
|---|---|---|
| `collectionName` | string | Target collection. Overrides Default Collection. |
| `documents` | array\<object\> | Documents to embed and store (see schema below) |

### Document Schema

```json
{
  "id": "optional-string",
  "text": "The document text to embed",
  "metadata": { "category": "tech", "year": 2024 }
}
```

| Field | Required | Description |
|---|---|---|
| `text` | Yes | Text content to embed (field name configurable via **Content Field** setting) |
| `id` | No | Document ID. Auto-generated UUID v4 if omitted. |
| `metadata` | No | Key-value pairs stored as payload alongside the vector |

## Output

| Field | Type | Description |
|---|---|---|
| `success` | boolean | `true` if all documents were embedded and stored |
| `ingestedCount` | integer | Number of documents successfully ingested |
| `ids` | array\<string\> | IDs of the ingested documents (in the same order as input) |
| `dimensions` | integer | Vector dimension used |
| `duration` | string | Total elapsed time |
| `error` | string | Error message if `success` is `false` |

## Flow Pattern

```
HTTP Trigger → Parse JSON body → Ingest Documents → Return response
```

## Notes

- The embedding call and VectorDB upsert are made in a single activity invocation — no intermediate mapping is required.
- Documents are upserted in one batch call to the VectorDB provider. If any document fails validation, the entire batch is rejected.
- The original text is stored in the payload under the **Content Field** key so it can be retrieved by search activities.

# Create Embeddings

Generate dense vector embeddings from text using OpenAI, Azure OpenAI, Cohere, Ollama, or a custom endpoint. Supports both single-text and batch embedding in one call.

## Settings

| Setting | Required | Default | Description |
|---|---|---|---|
| **VectorDB Connection** | No | — | Optional. Select a VectorDB connection to inherit embedding settings from. |
| **Use Connector Embedding Settings** | No | `true` | Inherit the embedding provider, API key, and base URL from the VectorDB connection. When `true`, only **Embedding Model** needs to be set. Requires *Configure Embedding Provider* to be enabled on the connection. Set to `false` to supply provider details directly below. |
| **Embedding Provider** | No | `OpenAI` | API provider: `OpenAI`, `Azure OpenAI`, `Cohere`, `Ollama`, `Custom`. Used when *Use Connector Embedding Settings* is `false`. |
| **API Key** | No | — | API key / bearer token. Not required for Ollama or private networks. Used when *Use Connector Embedding Settings* is `false`. |
| **Base URL** | No | — | Override default provider URL. See table below. Used when *Use Connector Embedding Settings* is `false`. |
| **Embedding Model** | Yes | `text-embedding-3-small` | Model to use. Must match the model used at query time. |
| **Dimensions** | No | `0` | Output vector size. `0` = model default. Supported by `text-embedding-3-*` (e.g. `512`, `1536`, `3072`). |
| **Timeout (s)** | No | `30` | HTTP request timeout |

### Provider Base URLs

| Provider | Default Base URL |
|---|---|
| OpenAI | `https://api.openai.com/v1` |
| Azure OpenAI | Full deployment URL (required) |
| Cohere | `https://api.cohere.ai/v1` |
| Ollama | `http://localhost:11434` |
| Custom | Your endpoint |

### Model Examples

| Provider | Model |
|---|---|
| OpenAI | `text-embedding-3-small`, `text-embedding-3-large`, `text-embedding-ada-002` |
| Azure OpenAI | `text-embedding-3-small` (deployment name) |
| Cohere | `embed-english-v3.0`, `embed-multilingual-v3.0` |
| Ollama | `nomic-embed-text`, `mxbai-embed-large` |

## Input

| Field | Type | Description |
|---|---|---|
| `inputText` | string | Single text to embed. Used when embedding one piece of text. |
| `inputTexts` | array\<string\> | Batch of texts to embed. More efficient than calling the activity multiple times. |

> Provide either `inputText` (single) or `inputTexts` (batch), or both.

## Output

| Field | Type | Description |
|---|---|---|
| `success` | boolean | `true` if embeddings were generated successfully |
| `embedding` | array\<number\> | Embedding vector for `inputText` |
| `embeddings` | array\<array\<number\>\> | Embedding vectors for each text in `inputTexts` |
| `dimensions` | integer | Length of each embedding vector |
| `tokensUsed` | integer | Total tokens consumed (where the API reports it) |
| `duration` | string | Elapsed time |
| `error` | string | Error message if `success` is `false` |

## Usage Pattern

Use **Create Embeddings** when you already have document IDs and just need the vectors to pass into **Upsert Documents** or **Vector Search**. For an all-in-one flow, use **Ingest Documents** (embedding + upsert) or **RAG Query** (embedding + search).

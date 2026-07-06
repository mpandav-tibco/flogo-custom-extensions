# Azure AI Search VectorDB Connector for TIBCO Flogo

A pure-Go Flogo connector for [Azure AI Search](https://learn.microsoft.com/en-us/azure/search/search-what-is-azure-search) (formerly Azure Cognitive Search), implementing the full `VectorDBClient` interface with REST-only access — no external SDK required.

## Features

- Full CRUD on Azure AI Search indexes (collections)
- Vector similarity search (HNSW + cosine/dot/euclidean)
- Hybrid search (dense vector + BM25 keyword)
- Document ingestion with embedding generation (OpenAI, Azure OpenAI, Cohere, Ollama)
- Document chunking (fixed, sentence, paragraph, heading strategies)
- PDF and DOCX text extraction
- Exponential backoff with jitter for transient errors
- OTel trace tags on every activity

## Prerequisites

Azure AI Search is a **cloud-only** service. There is no Docker container or local emulator available.

You need:
1. An Azure subscription with an Azure AI Search resource
2. The service **endpoint URL** (e.g. `https://myservice.search.windows.net`)
3. An **admin API key** with read/write access

## Environment Variables

| Variable | Description |
|---|---|
| `AZURE_SEARCH_ENDPOINT` | Full service URL including `https://` |
| `AZURE_SEARCH_API_KEY` | Admin API key |

## Connector Settings

| Setting | Required | Default | Description |
|---|---|---|---|
| `name` | yes | — | Unique connection name (registry key) |
| `endpoint` | yes | — | Azure AI Search endpoint URL |
| `apiKey` | yes | — | Admin API key (stored as password) |
| `apiVersion` | no | `2024-05-01-Preview` | Azure AI Search REST API version |
| `timeoutSeconds` | no | 30 | Per-operation HTTP timeout |
| `maxRetries` | no | 3 | Retry count for transient errors |
| `retryBackoffMs` | no | 500 | Base backoff in milliseconds |
| `enableEmbedding` | no | false | Enable shared embedding config |
| `embeddingProvider` | no | OpenAI | Embedding API provider |
| `embeddingAPIKey` | no | — | Embedding service API key |
| `embeddingBaseURL` | no | — | Override embedding endpoint |

## Activities

| Activity | Description |
|---|---|
| `vectorSearch` | Dense vector similarity search |
| `hybridSearch` | Combined BM25 keyword + vector search |
| `upsertDocuments` | Insert or update documents with vectors |
| `getDocument` | Retrieve a single document by ID |
| `deleteDocuments` | Delete documents by ID list |
| `createCollection` | Create an Azure AI Search index |
| `deleteCollection` | Delete an Azure AI Search index |
| `listCollections` | List all indexes in the service |
| `countDocuments` | Count documents (optionally filtered) |
| `scrollDocuments` | Paginate through all documents |
| `ingestDocuments` | Embed + upsert documents in one step |
| `createEmbeddings` | Generate vector embeddings from text |
| `ragQuery` | Full RAG pipeline: embed → search → format context |
| `rerank` | Cross-encoder reranking (Cohere, Jina, etc.) |

## Running Tests

### Unit tests (no Azure account needed)

```bash
cd connectors/VectorDB/azureaisearch
go test -v ./...
```

### Integration tests (live Azure account required)

```bash
export AZURE_SEARCH_ENDPOINT=https://myservice.search.windows.net
export AZURE_SEARCH_API_KEY=<admin-key>
go test -v -tags integration ./...
```

> **Note**: Integration tests create and delete a temporary index with the prefix `flogo-test-`. Ensure the API key has index create/delete permissions.

## Azure AI Search Notes

- **Vector fields** use `Collection(Edm.Single)` type with HNSW algorithm
- **Metadata** is stored as a JSON string in an `Edm.String` field (Azure AI Search does not support arbitrary nested JSON natively)
- **API version** `2024-05-01-Preview` is required for vector search features; set to `2023-11-01` for GA if preview features are not needed
- **DeleteByFilter** performs client-side filtering since metadata is stored as a JSON string and is not directly OData-filterable
- **HybridSearch** uses Reciprocal Rank Fusion (RRF) via `@search.semanticConfiguration` is not required; the `search` text field and vector field are combined by the service

# Weaviate VectorDB Connector for TIBCO Flogo

A Flogo connector for [Weaviate](https://weaviate.io/) — an open-source vector database with built-in schema management, multi-tenancy, and hybrid search. Designed for RAG pipelines and agentic AI workflows.

**Status**: ✅ Active  
**Go Client**: `github.com/weaviate/weaviate-go-client/v5 v5.7.2`  
**Transport**: REST / GraphQL  
**Connector Name**: `weaviate-connector`

## Connection Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **Name** | Yes | — | Unique connection name referenced by activities |
| **Host** | Yes | — | Weaviate hostname or IP |
| **Port** | No | `8080` | REST port |
| **HTTP Scheme** | No | `http` | `http` or `https` |
| **API Key** | No | — | API key (for Weaviate Cloud) |
| **Use TLS** | No | `false` | Enable TLS/SSL |
| **TLS Skip Verify** | No | `false` | Skip certificate verification |
| **Timeout (s)** | No | `30` | Per-operation timeout |
| **Max Retries** | No | `3` | Retries on transient errors |
| **Retry Backoff (ms)** | No | `500` | Wait between retries |

## Activities

| Activity | Description |
|----------|-------------|
| `createCollection` | Create a new vector collection / index |
| `deleteCollection` | Permanently delete a collection and all its data |
| `listCollections` | List all collections in the database |
| `upsertDocuments` | Insert or update documents with pre-computed vectors |
| `ingestDocuments` | Embed raw text and upsert in one step (recommended for ingestion pipelines) |
| `getDocument` | Retrieve a single document by ID |
| `deleteDocuments` | Delete documents by ID list or metadata filter |
| `scrollDocuments` | Paginate through all documents without a query vector |
| `countDocuments` | Count documents with optional metadata filter |
| `vectorSearch` | Semantic ANN search with a dense query vector |
| `hybridSearch` | Combined dense + BM25 keyword search |
| `ragQuery` | Full RAG pipeline: embed query → vector search → format context for LLM |
| `createEmbeddings` | Generate embeddings from text (OpenAI, Azure OpenAI, Cohere, Ollama) |
| `rerank` | Cross-encoder reranking for improved retrieval precision (Cohere, Jina) |
## Quick Start

```bash
docker run -d --name weaviate \
  -p 8080:8080 \
  -e AUTHENTICATION_ANONYMOUS_ACCESS_ENABLED=true \
  -e DEFAULT_VECTORIZER_MODULE=none \
  semitechnologies/weaviate:latest
```

Connection settings in Flogo Designer:
- **Host**: `localhost`
- **Port**: `8080`
- **HTTP Scheme**: `http`

## Behavior

- Weaviate classes map to collections in the activity API
- Hybrid search combines BM25 + dense vectors natively (alpha parameter: `1.0` = pure vector, `0.0` = pure BM25)
- Filter-based delete uses the GraphQL batch delete API
- `countDocuments` with a filter returns an error — call without filters to count all documents
- `scrollDocuments` uses Weaviate's native cursor-based pagination
- Weaviate Cloud: set the **API Key** and use `https` scheme

## Running Tests

```bash
# Unit tests
cd connectors/VectorDB/weaviate
CGO_ENABLED=0 GOFLAGS="-mod=mod" GOTOOLCHAIN=auto go test ./...

# Integration tests (requires Weaviate running on localhost:8080)
CGO_ENABLED=0 GOFLAGS="-mod=mod" GOTOOLCHAIN=auto go test -tags integration -v -timeout 300s ./...
```


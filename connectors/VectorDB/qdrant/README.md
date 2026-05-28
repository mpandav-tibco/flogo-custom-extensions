# Qdrant VectorDB Connector for TIBCO Flogo

A Flogo connector for [Qdrant](https://qdrant.tech/) — a high-performance, open-source vector database written in Rust. Optimised for RAG pipelines and agentic AI workflows.

**Status**: ✅ Active  
**Go Client**: `github.com/qdrant/go-client v1.17.1`  
**Transport**: gRPC (primary) + REST  
**Connector Name**: `qdrant-connector`

## Connection Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **Name** | Yes | — | Unique connection name referenced by activities |
| **Host** | Yes | — | Qdrant hostname or IP |
| **Port** | No | `6333` | REST/HTTP port |
| **gRPC Port** | No | `6334` | gRPC port (used for upsert, search, get) |
| **API Key** | No | — | API key (required for Qdrant Cloud) |
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
docker run -d --name qdrant \
  -p 6333:6333 -p 6334:6334 \
  qdrant/qdrant:latest
```

Connection settings in Flogo Designer:
- **Host**: `localhost`
- **Port**: `6333`
- **gRPC Port**: `6334`

## Behavior

- gRPC is used for upsert, search, and get operations for maximum throughput
- REST is used for collection management
- **Hybrid search is not yet implemented natively** — `hybridSearch` falls back to dense vector search with a warning logged. Native sparse+dense hybrid via Qdrant's `QueryPoints + Prefetch` API is planned.
- `scrollDocuments` uses Qdrant's native cursor-based scroll API
- Qdrant Cloud: set the **API Key**; for local deployments leave it blank

## Running Tests

```bash
# Unit tests
cd connectors/VectorDB/qdrant
CGO_ENABLED=0 GOFLAGS="-mod=mod" GOTOOLCHAIN=auto go test ./...

# Integration tests (requires Qdrant running on localhost:6333)
CGO_ENABLED=0 GOFLAGS="-mod=mod" GOTOOLCHAIN=auto go test -tags integration -v -timeout 300s ./...
```

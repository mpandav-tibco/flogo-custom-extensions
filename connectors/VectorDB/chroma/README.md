# Chroma VectorDB Connector for TIBCO Flogo

A Flogo connector for [Chroma](https://www.trychroma.com/) — a lightweight, open-source embedding database built for AI applications. Ideal for rapid prototyping, local development, and smaller-scale RAG pipelines.

**Status**: ✅ Active  
**Go Client**: `github.com/amikos-tech/chroma-go v0.4.1`  
**Transport**: REST (v2 API)  
**Connector Name**: `chroma-connector`

## Connection Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **Name** | Yes | — | Unique connection name referenced by activities |
| **Host** | Yes | — | Chroma hostname or IP |
| **Port** | No | `8000` | REST port |
| **API Key** | No | — | API key (for Chroma Cloud or secured deployments) |
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
docker run -d --name chroma \
  -p 8000:8000 \
  chromadb/chroma:latest
```

Connection settings in Flogo Designer:
- **Host**: `localhost`
- **Port**: `8000`

## Behavior

- Uses Chroma v2 REST API
- `scrollDocuments` is client-side: all documents are fetched into memory, then sliced. **Hard limit: 50,000 documents** — requests exceeding this cap return an error. Use Qdrant or Milvus for large collections.
- `hybridSearch` falls back to pure vector search (Chroma does not support native BM25 via the Go client)
- `countDocuments` with a filter is client-side (all matching documents fetched and counted)
- Embedding function storage is disabled (`WithDisableEFConfigStorage`) to avoid ONNX runtime dependency — bring your own embeddings or use `createEmbeddings` / `ingestDocuments` activities
- Chroma Cloud: set the **API Key** and enable **Use TLS**

## Running Tests

```bash
# Unit tests
cd connectors/VectorDB/chroma
CGO_ENABLED=0 GOFLAGS="-mod=mod" GOTOOLCHAIN=auto go test ./...

# Integration tests (requires Chroma running on localhost:8000)
CGO_ENABLED=0 GOFLAGS="-mod=mod" GOTOOLCHAIN=auto go test -tags integration -v -timeout 300s ./...
```


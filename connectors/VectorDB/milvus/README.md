# Milvus VectorDB Connector for TIBCO Flogo

A Flogo connector for [Milvus](https://milvus.io/) — an enterprise-grade, cloud-native vector database built for high-throughput, large-scale AI applications. Designed for RAG pipelines and agentic AI workflows.

**Status**: ✅ Active  
**Go Client**: `github.com/milvus-io/milvus-sdk-go/v2 v2.4.2`  
**Transport**: gRPC  
**Connector Name**: `milvus-connector`

## Connection Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **Name** | Yes | — | Unique connection name referenced by activities |
| **Host** | Yes | — | Milvus hostname or IP |
| **Port** | No | `19530` | gRPC port |
| **Username** | No | — | Milvus username (leave empty for open access) |
| **Password** | No | — | Milvus password |
| **Database Name** | No | `default` | Milvus database to connect to |
| **Default Metric Type** | No | `cosine` | Default distance metric (`cosine`, `dot`, `euclidean`) |
| **API Key** | No | — | API key (for Zilliz Cloud) |
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
docker run -d --name milvus \
  -p 19530:19530 \
  milvusdb/milvus:v2.4.0 standalone
```

Connection settings in Flogo Designer:
- **Host**: `localhost`
- **Port**: `19530`
- **Database Name**: `default`
- **Default Metric Type**: `cosine`

## Behavior

- All operations use gRPC for maximum throughput
- Filters use Milvus boolean expression syntax (e.g. `metadata["author"] == "alice"`)
- `hybridSearch` falls back to pure vector search (Milvus hybrid search requires sparse index setup not yet automated)
- Collection consistency is eventual — a brief delay after insert/delete before changes are visible in search is expected behaviour
- **Username / Password**: leave empty for open-access local deployments; set for secured or Zilliz Cloud instances
- Zilliz Cloud: set the **API Key** and enable **Use TLS**

## Running Tests

```bash
# Unit tests
cd connectors/VectorDB/milvus
CGO_ENABLED=0 GOFLAGS="-mod=mod" GOTOOLCHAIN=auto go test ./...

# Integration tests (requires Milvus running on localhost:19530)
CGO_ENABLED=0 GOFLAGS="-mod=mod" GOTOOLCHAIN=auto go test -tags integration -v -timeout 300s ./...
```



# VectorDB RAG Connector

A multi-provider vector database connector for TIBCO Flogo, purpose-built for RAG (Retrieval-Augmented Generation) and agentic AI pipelines. Connect to **Qdrant**, **Weaviate**, **Chroma**, or **Milvus** using a single shared connection definition and a consistent activity API.

## Supported Providers

| Provider | Version | Transport | Notes |
|---|---|---|---|
| **Qdrant** | Latest | gRPC + REST | Full feature support including hybrid search |
| **Weaviate** | Latest | REST / GraphQL | Hybrid search supported; filter delete supported via batch GraphQL |
| **Chroma** | 1.0+ (v2 API) | REST | Scroll is client-side; hybrid search falls back to vector |
| **Milvus** | 2.x | gRPC | Boolean expression filters |

## Client Libraries

| Provider | Go Client |
|---|---|
| Qdrant | `github.com/qdrant/go-client v1.17.1` |
| Weaviate | `github.com/weaviate/weaviate-go-client/v5 v5.7.2` |
| Chroma | `github.com/amikos-tech/chroma-go v0.4.1` |
| Milvus | `github.com/milvus-io/milvus-sdk-go/v2 v2.4.2` |

## Connector Settings

Configure a shared connection in Flogo Designer or via app properties:

| Setting | Required | Default | Description |
|---|---|---|---|
| **Connection Name** | Yes | — | Unique name used to reference this connection in activities |
| **DB Provider** | Yes | — | `qdrant`, `weaviate`, `chroma`, or `milvus` |
| **Host** | Yes | — | Hostname or IP of the VectorDB server |
| **Port** | No | `0` | REST/HTTP port. Provider default used when `0`. |
| **API Key** | No | — | API key or bearer token |
| **Use TLS** | No | `false` | Enable TLS/SSL |
| **HTTP Scheme** | No | `http` | `http` or `https` |
| **Timeout (s)** | No | `30` | Per-operation timeout |
| **Max Retries** | No | `3` | Retries on transient errors |
| **Retry Backoff (ms)** | No | `500` | Wait between retries |
| **gRPC Port** | No | `6334` | Qdrant gRPC port (ignored for other providers) |
| **Username** | No | — | Milvus username |
| **Password** | No | — | Milvus password |
| **Database Name** | No | `default` | Milvus database name |

### Default Ports

| Provider | REST/HTTP | gRPC |
|---|---|---|
| Qdrant | 6333 | 6334 |
| Weaviate | 8080 | — |
| Chroma | 8000 | — |
| Milvus | 19530 | 19530 |

## Activities

| Activity | Description |
|---|---|
| [Create Collection](activity/createCollection/README.md) | Create a new vector collection / index |
| [Delete Collection](activity/deleteCollection/README.md) | Permanently delete a collection and all its data |
| [List Collections](activity/listCollections/README.md) | List all collections in the database |
| [Upsert Documents](activity/upsertDocuments/README.md) | Insert or update documents with pre-computed vectors |
| [Ingest Documents](activity/ingestDocuments/README.md) | Embed text + upsert in one step (recommended for ingestion) |
| [Get Document](activity/getDocument/README.md) | Retrieve a single document by ID |
| [Delete Documents](activity/deleteDocuments/README.md) | Delete documents by ID list or metadata filter |
| [Scroll Documents](activity/scrollDocuments/README.md) | Paginate through all documents without a query vector |
| [Count Documents](activity/countDocuments/README.md) | Count documents, with optional metadata filter |
| [Vector Search](activity/vectorSearch/README.md) | Semantic ANN search with a dense query vector |
| [Hybrid Search](activity/hybridSearch/README.md) | Combined dense + BM25 keyword search |
| [RAG Query](activity/ragQuery/README.md) | Full RAG pipeline: embed → search → format context |
| [Create Embeddings](activity/createEmbeddings/README.md) | Generate embeddings from text (OpenAI, Cohere, Ollama, …) |
| [Rerank Documents](activity/rerank/README.md) | Cross-encoder reranking for improved precision |

## Quick Start

### 1. Start a VectorDB

```bash
# Qdrant
docker run -d --name qdrant -p 6333:6333 -p 6334:6334 qdrant/qdrant:latest

# Weaviate
docker run -d --name weaviate -p 8080:8080 \
  -e AUTHENTICATION_ANONYMOUS_ACCESS_ENABLED=true \
  -e DEFAULT_VECTORIZER_MODULE=none \
  cr.weaviate.io/semitechnologies/weaviate:latest

# Chroma
docker run -d --name chroma -p 8000:8000 chromadb/chroma:latest

# Milvus (standalone)
docker run -d --name milvus -p 19530:19530 milvusdb/milvus:v2.4.0-latest standalone
```

### 2. Create a Collection

Use **Create Collection** with:
- `collectionName`: `my-docs`
- `dimensions`: `1536` (matches `text-embedding-3-small`)
- `distanceMetric`: `cosine`

### 3. Ingest Documents

Use **Ingest Documents** with your OpenAI API key and `text-embedding-3-small` model. Pass an array of `{ "id": "...", "text": "...", "metadata": {} }` objects.

### 4. Query with RAG

Use **RAG Query** with the same embedding settings and `collectionName`. Pass the user's question as `queryText`. Use `formattedContext` as input to your LLM activity.

## Typical RAG Flow

```
HTTP Trigger (POST /ask)
  ├── RAG Query
  │     ├── Embedding: OpenAI text-embedding-3-small
  │     ├── VectorDB: Qdrant / Weaviate / Chroma / Milvus
  │     └── Output: formattedContext, sourceDocuments
  ├── (Optional) Rerank Documents
  └── LLM Activity (OpenAI / Azure OpenAI)
        └── Prompt: "Answer using:\n" + formattedContext
```

## Two-Stage Retrieval Flow (High Precision)

```
RAG Query (topK=20, low threshold)
  → Rerank Documents (topN=5)
  → LLM Activity
```

## Feature Matrix

| Feature | Qdrant | Weaviate | Chroma | Milvus |
|---|---|---|---|---|
| Vector Search | ✅ | ✅ | ✅ | ✅ |
| Hybrid Search | ✅ | ✅ | ⚠️ fallback | ⚠️ fallback |
| Metadata Filters | ✅ | ✅ | ✅ | ✅ |
| Delete by Filter | ✅ | ✅ | ✅ | ✅ |
| Scroll / Paginate | ✅ native | ✅ native | ⚠️ client-side | ✅ native |
| Count with Filter | ✅ | ❌ | ⚠️ client-side | ✅ |
| TLS / Auth | ✅ | ✅ | ✅ | ✅ |

## Running Tests

```bash
# Unit tests
cd connectors/VectorDB
CGO_ENABLED=0 GOFLAGS="-mod=mod" GOTOOLCHAIN=auto go test ./...

# Integration tests (requires running Docker containers — see above)
CGO_ENABLED=0 GOFLAGS="-mod=mod" GOTOOLCHAIN=auto go test -tags integration -v -timeout 300s ./...
```

> `CGO_ENABLED=0` is required (the Chroma client has optional native CGO dependencies that are not needed for HTTP transport). `GOFLAGS="-mod=mod"` and `GOTOOLCHAIN=auto` resolve toolchain version constraints in the module graph.

## Module

```
github.com/mpandav-tibco/flogo-extensions/vectordb
```

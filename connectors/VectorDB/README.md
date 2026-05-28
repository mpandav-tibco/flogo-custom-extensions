# VectorDB Connectors for TIBCO Flogo

A family of purpose-built vector database connectors for TIBCO Flogo, designed for RAG (Retrieval-Augmented Generation) and agentic AI pipelines. Each connector provides a consistent set of **14 activities** with a provider-specific connection configuration.

---

## Connector Index

| Connector | Provider | Status | Go Module |
|-----------|----------|--------|-----------|
| [qdrant](qdrant/README.md) | Qdrant | ✅ Active | `github.com/mpandav-tibco/flogo-extensions/vectordb-qdrant` |
| [weaviate](weaviate/README.md) | Weaviate | ✅ Active | `github.com/mpandav-tibco/flogo-extensions/vectordb-weaviate` |
| [chroma](chroma/README.md) | Chroma | ✅ Active | `github.com/mpandav-tibco/flogo-extensions/vectordb-chroma` |
| [milvus](milvus/README.md) | Milvus | ✅ Active | `github.com/mpandav-tibco/flogo-extensions/vectordb-milvus` |
| [monolith](monolith/README.md) | All providers (monolith) | ⚠️ Deprecated | `github.com/mpandav-tibco/flogo-extensions/vectordb` |

> **New projects** should use the dedicated connector for their target provider. The monolithic `VectorDB` connector is deprecated — no new features will be added. 

---

## Activities (Common to All Connectors)

All five connectors expose the same 14 activities:

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

---

## Feature Matrix

| Feature | Qdrant | Weaviate | Chroma | Milvus |
|---------|:------:|:--------:|:------:|:------:|
| Vector Search | ✅ | ✅ | ✅ | ✅ |
| Hybrid Search | ⚠️ fallback | ✅ native | ⚠️ fallback | ⚠️ fallback |
| Metadata Filters | ✅ | ✅ | ✅ | ✅ |
| Delete by Filter | ✅ | ✅ | ✅ | ✅ |
| Scroll / Paginate | ✅ native | ✅ native | ⚠️ client-side | ✅ native |
| Count with Filter | ✅ | ❌ | ⚠️ client-side | ✅ |
| TLS / Auth | ✅ | ✅ | ✅ | ✅ |
| gRPC Transport | ✅ | ❌ | ❌ | ✅ |
| Multi-tenancy | ✅ collections | ✅ classes | ✅ collections | ✅ collections |

---

## Typical RAG Flow

```
HTTP Trigger (POST /ask)
  └── RAG Query
        ├── Embedding: Ollama / OpenAI / Cohere
        ├── VectorDB: your chosen provider
        └── Output: formattedContext, sourceDocuments
  └── (Optional) Rerank Documents
  └── LLM Activity
        └── Prompt: "Answer using:\n" + formattedContext
```

## Two-Stage High-Precision Retrieval

```
RAG Query (topK=20, low threshold)
  → Rerank Documents (topN=5)
  → LLM Activity
```

---

## Docker Quick Start

```bash
# Qdrant
docker run -d --name qdrant -p 6333:6333 -p 6334:6334 qdrant/qdrant:latest

# Weaviate
docker run -d --name weaviate -p 8080:8080 \
  -e AUTHENTICATION_ANONYMOUS_ACCESS_ENABLED=true \
  -e DEFAULT_VECTORIZER_MODULE=none \
  semitechnologies/weaviate:latest

# Chroma
docker run -d --name chroma -p 8000:8000 chromadb/chroma:latest

# Milvus (standalone)
docker run -d --name milvus -p 19530:19530 milvusdb/milvus:v2.4.0 standalone
```

---

## Other Connectors

| Connector | Description |
|-----------|-------------|
| [KafkaStream](../../KafkaStream/) | Kafka streaming trigger and producer activity |
| [SSE](../../SSE/) | Server-Sent Events trigger and producer activity |

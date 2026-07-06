# VectorDB Connectors for TIBCO Flogo

A family of purpose-built vector database connectors for TIBCO Flogo, designed for RAG (Retrieval-Augmented Generation) and agentic AI pipelines. Each connector provides a consistent set of **14 activities** with a provider-specific connection configuration.

---

## Connector Index

### Tier 1 — General Purpose

| Connector | Provider | Status | Go Module |
|-----------|----------|--------|-----------|
| [qdrant](qdrant/README.md) | Qdrant | ✅ Active | `github.com/mpandav-tibco/flogo-extensions/vectordb-qdrant` |
| [weaviate](weaviate/README.md) | Weaviate | ✅ Active | `github.com/mpandav-tibco/flogo-extensions/vectordb-weaviate` |
| [chroma](chroma/README.md) | Chroma | ✅ Active | `github.com/mpandav-tibco/flogo-extensions/vectordb-chroma` |
| [milvus](milvus/README.md) | Milvus | ✅ Active | `github.com/mpandav-tibco/flogo-extensions/vectordb-milvus` |
| [pgvector](pgvector/README.md) | PostgreSQL + pgvector | ✅ Active | `github.com/mpandav-tibco/flogo-extensions/vectordb-pgvector` |
| [pinecone](pinecone/README.md) | Pinecone Cloud | ✅ Active | `github.com/mpandav-tibco/flogo-extensions/vectordb-pinecone` |
| [redis](redis/README.md) | Redis / Redis Stack | ✅ Active | `github.com/mpandav-tibco/flogo-extensions/vectordb-redis` |

### Tier 2 — Elasticsearch Ecosystem & Cloud-Native

| Connector | Provider | Status | Go Module |
|-----------|----------|--------|-----------|
| [elasticsearch](elasticsearch/README.md) | Elasticsearch 8.x | ✅ Active | `github.com/mpandav-tibco/flogo-extensions/vectordb-elasticsearch` |
| [opensearch](opensearch/README.md) | OpenSearch 2.x | ✅ Active | `github.com/mpandav-tibco/flogo-extensions/vectordb-opensearch` |
| [azureaisearch](azureaisearch/README.md) | Azure AI Search | ✅ Active | `github.com/mpandav-tibco/flogo-extensions/vectordb-azureaisearch` |
| [lancedb](lancedb/README.md) | LanceDB | ✅ Active | `github.com/mpandav-tibco/flogo-extensions/vectordb-lancedb` |

### Legacy

| Connector | Provider | Status | Go Module |
|-----------|----------|--------|-----------|
| [monolith](monolith/README.md) | All providers (monolith) | ⚠️ Deprecated | `github.com/mpandav-tibco/flogo-extensions/vectordb` |

> **New projects** should use the dedicated connector for their target provider. The monolithic `VectorDB` connector is deprecated — no new features will be added. 

---

## Activities (Common to All Connectors)

All connectors expose the same 14 activities:

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

### Tier 1

| Feature | Qdrant | Weaviate | Chroma | Milvus | pgvector | Pinecone | Redis |
|---------|:------:|:--------:|:------:|:------:|:--------:|:--------:|:-----:|
| Vector Search | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Hybrid Search | ⚠️ fallback | ✅ native | ⚠️ fallback | ⚠️ fallback | ✅ native | ✅ native | ✅ native |
| Metadata Filters | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Delete by Filter | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Scroll / Paginate | ✅ native | ✅ native | ⚠️ client-side | ✅ native | ✅ native | ✅ native | ✅ native |
| Count with Filter | ✅ | ❌ | ⚠️ client-side | ✅ | ✅ | ❌ | ✅ |
| TLS / Auth | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| gRPC Transport | ✅ | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ |
| Docker Available | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ cloud | ✅ |

### Tier 2

| Feature | Elasticsearch | OpenSearch | Azure AI Search | LanceDB |
|---------|:-------------:|:----------:|:---------------:|:-------:|
| Vector Search | ✅ | ✅ | ✅ | ✅ |
| Hybrid Search | ✅ native | ✅ native | ✅ native (RRF) | ✅ RRF |
| Metadata Filters | ✅ | ✅ | ⚠️ client-side¹ | ⚠️ LIKE matching² |
| Delete by Filter | ✅ server-side | ✅ server-side | ⚠️ client-side¹ | ✅ server-side |
| Scroll / Paginate | ✅ | ✅ | ✅ | ✅ |
| Count with Filter | ✅ | ✅ | ⚠️ client-side¹ | ⚠️ client-side² |
| TLS / Auth | ✅ | ✅ | ✅ API key | ✅ Bearer |
| Docker Available | ✅ | ✅ | ❌ cloud only | ✅ custom image |

¹ Azure metadata is stored as a JSON string; OData filtering is only available on structured fields.  
² LanceDB metadata is stored as a JSON string; numeric range operators ($gt/$lt) are not supported.

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

# Elasticsearch 8 (security disabled for dev)
docker compose -f elasticsearch/docker-compose.elasticsearch.yml up -d

# OpenSearch 2 (security plugin disabled for dev)
docker compose -f opensearch/docker-compose.opensearch.yml up -d

# LanceDB (custom FastAPI server — builds locally)
docker compose -f lancedb/docker-compose.lancedb.yml up -d --build
```

> Azure AI Search is cloud-only. Set `AZURE_SEARCH_ENDPOINT` and `AZURE_SEARCH_API_KEY` environment variables.

---

## Other Connectors

| Connector | Description |
|-----------|-------------|
| [KafkaStream](../../KafkaStream/) | Kafka streaming trigger and producer activity |
| [SSE](../../SSE/) | Server-Sent Events trigger and producer activity |

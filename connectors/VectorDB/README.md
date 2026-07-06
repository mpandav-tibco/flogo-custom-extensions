# VectorDB Connectors for TIBCO Flogo

A family of purpose-built vector database connectors for TIBCO Flogo, designed for RAG (Retrieval-Augmented Generation) and agentic AI pipelines. Each connector provides a consistent set of **14 activities** with a provider-specific connection configuration.

---

## Connector Index

| Connector | Provider | Transport | Deployment | Status |
|-----------|----------|-----------|------------|--------|
| [qdrant](qdrant/README.md) | Qdrant | REST + gRPC | Self-hosted / Cloud | ✅ Active |
| [weaviate](weaviate/README.md) | Weaviate | REST / GraphQL | Self-hosted / Cloud | ✅ Active |
| [chroma](chroma/README.md) | Chroma | REST v2 | Self-hosted | ✅ Active |
| [milvus](milvus/README.md) | Milvus | gRPC | Self-hosted / Cloud | ✅ Active |
| [pgvector](pgvector/README.md) | PostgreSQL + pgvector | PostgreSQL wire (pgx) | Self-hosted | ✅ Active |
| [pinecone](pinecone/README.md) | Pinecone | REST | Cloud-only | ✅ Active |
| [redis](redis/README.md) | Redis Stack | RESP3 | Self-hosted | ✅ Active |
| [elasticsearch](elasticsearch/README.md) | Elasticsearch 8.x | REST | Self-hosted / Cloud | ✅ Active |
| [opensearch](opensearch/README.md) | OpenSearch 2.x | REST | Self-hosted / Cloud | ✅ Active |
| [azureaisearch](azureaisearch/README.md) | Azure AI Search | REST | Cloud-only | ✅ Active |
| [lancedb](lancedb/README.md) | LanceDB | REST (custom server) | Self-hosted | ✅ Active |
| [monolith](monolith/README.md) | All providers (monolith) | Mixed | Mixed | ⚠️ Deprecated |

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

| Feature | Qdrant | Weaviate | Chroma | Milvus | pgvector | Pinecone | Redis | Elasticsearch | OpenSearch | Azure AI Search | LanceDB |
|---------|:------:|:--------:|:------:|:------:|:--------:|:--------:|:-----:|:-------------:|:----------:|:---------------:|:-------:|
| **Vector Search** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Hybrid Search** | ⚠️ fallback | ✅ native | ⚠️ fallback | ⚠️ fallback | ✅ native | ✅ native | ✅ native | ✅ native | ✅ native | ✅ RRF | ✅ RRF |
| **Metadata Filters** | ✅ | ✅ | ✅ | ✅ | ✅ JSONB | ✅ | ✅ | ✅ | ✅ | ⚠️ client-side¹ | ⚠️ LIKE only² |
| **Delete by Filter** | ✅ server | ✅ server | ✅ server | ✅ server | ✅ server | ✅ server | ⚠️ client-side | ✅ server | ✅ server | ⚠️ client-side¹ | ✅ server |
| **Scroll / Paginate** | ✅ native | ✅ native | ⚠️ client-side | ✅ native | ✅ native | ✅ native | ✅ native | ✅ | ✅ | ✅ | ✅ |
| **Count with Filter** | ✅ | ❌ | ⚠️ client-side | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ⚠️ client-side¹ | ⚠️ client-side² |
| **TLS / Auth** | ✅ | ✅ | ✅ | ✅ | ✅ SSL | ✅ API key | ✅ password | ✅ | ✅ | ✅ API key | ✅ Bearer |
| **gRPC Transport** | ✅ | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **Self-hosted** | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ |
| **Cloud / Managed** | ✅ | ✅ | ❌ | ✅ | ❌ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ |

¹ Azure AI Search: metadata is stored as a JSON string (`Edm.String`) — OData filtering is not available; all filters are client-side.  
² LanceDB: metadata is stored as a JSON string; numeric range operators (`$gt`/`$lt`) are not supported; filtering uses SQL `LIKE` matching.

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

# pgvector
docker run -d --name pgvector -p 5432:5432 -e POSTGRES_PASSWORD=postgres pgvector/pgvector:pg16

# Redis Stack
docker run -d --name redis-stack -p 6379:6379 redis/redis-stack-server:latest

# Elasticsearch 8 (security disabled for dev)
docker compose -f elasticsearch/docker-compose.elasticsearch.yml up -d

# OpenSearch 2 (security plugin disabled for dev)
docker compose -f opensearch/docker-compose.opensearch.yml up -d

# LanceDB (custom FastAPI server — builds locally)
docker compose -f lancedb/docker-compose.lancedb.yml up -d --build
```

> **Pinecone** is cloud-only. Sign up at [app.pinecone.io](https://app.pinecone.io) or use `ghcr.io/pinecone-io/pinecone-local:latest` for local dev.  
> **Azure AI Search** is cloud-only. Set `AZURE_SEARCH_ENDPOINT` and `AZURE_SEARCH_API_KEY` environment variables.

---

## Other Connectors

| Connector | Description |
|-----------|-------------|
| [KafkaStream](../../KafkaStream/) | Kafka streaming trigger and producer activity |
| [SSE](../../SSE/) | Server-Sent Events trigger and producer activity |

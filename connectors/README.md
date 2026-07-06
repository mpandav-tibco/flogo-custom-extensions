# Flogo Custom Extensions — Connectors

Custom TIBCO Flogo connectors. Each connector provides a Flogo connection type and a set of activities that can be used in Flogo app flows.

## Vector Database Connectors

A family of purpose-built VectorDB connectors for RAG and agentic AI pipelines. All connectors share the same **14-activity interface** — only the connection configuration differs per provider.

See [VectorDB/README.md](VectorDB/README.md) for the full index, feature matrix, and decision guide.

### Tier 1 — General Purpose

| Connector | Provider | Status |
|-----------|----------|--------|
| [VectorDB/qdrant](VectorDB/qdrant/README.md) | Qdrant | ✅ Active |
| [VectorDB/weaviate](VectorDB/weaviate/README.md) | Weaviate | ✅ Active |
| [VectorDB/chroma](VectorDB/chroma/README.md) | Chroma | ✅ Active |
| [VectorDB/milvus](VectorDB/milvus/README.md) | Milvus | ✅ Active |
| [VectorDB/pgvector](VectorDB/pgvector/README.md) | PostgreSQL + pgvector | ✅ Active |
| [VectorDB/pinecone](VectorDB/pinecone/README.md) | Pinecone Cloud | ✅ Active |
| [VectorDB/redis](VectorDB/redis/README.md) | Redis Stack | ✅ Active |

### Tier 2 — Elasticsearch Ecosystem & Cloud-Native

| Connector | Provider | Status |
|-----------|----------|--------|
| [VectorDB/elasticsearch](VectorDB/elasticsearch/README.md) | Elasticsearch 8.x | ✅ Active |
| [VectorDB/opensearch](VectorDB/opensearch/README.md) | OpenSearch 2.x | ✅ Active |
| [VectorDB/azureaisearch](VectorDB/azureaisearch/README.md) | Azure AI Search | ✅ Active |
| [VectorDB/lancedb](VectorDB/lancedb/README.md) | LanceDB | ✅ Active |

### Legacy

| Connector | Provider | Status |
|-----------|----------|--------|
| [VectorDB/monolith](VectorDB/monolith/README.md) | All providers (monolith) | ⚠️ Deprecated |

> **New projects** should use the dedicated connector for their target provider. The monolithic `VectorDB` connector is deprecated — no new features will be added.

## Streaming Connectors

| Connector | Description |
|-----------|-------------|
| [KafkaStream](KafkaStream/README.md) | Stateful windowed stream processing — filtering, aggregation, join, split |
| [SSE](sse/) | Server-Sent Events trigger and producer activity |

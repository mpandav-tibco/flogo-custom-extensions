# Flogo Custom Extensions — Connectors

Custom TIBCO Flogo connectors. Each connector provides a Flogo connection type and a set of activities that can be used in Flogo app flows.

## Vector Database Connectors

A family of purpose-built VectorDB connectors for RAG and agentic AI pipelines. All connectors share the same **14-activity interface** — only the connection configuration differs per provider.

| Connector | Provider | Transport | Deployment |
|-----------|----------|-----------|------------|
| [VectorDB/activespaces](VectorDB/activespaces/README.md) | TIBCO ActiveSpaces 5.2 | REST gateway / native (CGO) | Self-hosted |
| [VectorDB/qdrant](VectorDB/qdrant/README.md) | Qdrant | REST + gRPC | Self-hosted / Cloud |
| [VectorDB/weaviate](VectorDB/weaviate/README.md) | Weaviate | REST / GraphQL | Self-hosted / Cloud |
| [VectorDB/chroma](VectorDB/chroma/README.md) | Chroma | REST v2 | Self-hosted |
| [VectorDB/milvus](VectorDB/milvus/README.md) | Milvus | gRPC | Self-hosted / Cloud |
| [VectorDB/pgvector](VectorDB/pgvector/README.md) | PostgreSQL + pgvector | PostgreSQL wire (pgx) | Self-hosted |
| [VectorDB/pinecone](VectorDB/pinecone/README.md) | Pinecone | REST | Cloud-only |
| [VectorDB/redis](VectorDB/redis/README.md) | Redis Stack | RESP3 | Self-hosted |
| [VectorDB/elasticsearch](VectorDB/elasticsearch/README.md) | Elasticsearch 8.x | REST | Self-hosted / Cloud |
| [VectorDB/opensearch](VectorDB/opensearch/README.md) | OpenSearch 2.x | REST | Self-hosted / Cloud |
| [VectorDB/azureaisearch](VectorDB/azureaisearch/README.md) | Azure AI Search | REST | Cloud-only |
| [VectorDB/lancedb](VectorDB/lancedb/README.md) | LanceDB | REST (custom server) | Self-hosted |

See [VectorDB/README.md](VectorDB/README.md) for the full feature matrix and connector selection guide.

> The monolithic [VectorDB/monolith](VectorDB/monolith/README.md) connector is **deprecated** — use dedicated connectors for new projects.

## Streaming Connectors

| Connector | Description |
|-----------|-------------|
| [KafkaStream](KafkaStream/README.md) | Stateful windowed stream processing — filtering, aggregation, join, split |
| [SSE](sse/) | Server-Sent Events trigger and producer activity |

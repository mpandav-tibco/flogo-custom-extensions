# Flogo Custom Extensions — Connectors

Custom TIBCO Flogo connectors. Each connector provides a Flogo connection type and a set of activities that can be used in Flogo app flows.

## Vector Database Connectors

A family of purpose-built VectorDB connectors for RAG and agentic AI pipelines. All connectors share the same 14-activity interface — only the connection configuration differs per provider.

See [VectorDB/README.md](VectorDB/README.md) for the full index, feature matrix, and decision guide.

| Connector | Provider | Status |
|-----------|----------|--------|
| [VectorDB/qdrant](VectorDB/qdrant/README.md) | Qdrant | ✅ Active |
| [VectorDB/weaviate](VectorDB/weaviate/README.md) | Weaviate | ✅ Active |
| [VectorDB/chroma](VectorDB/chroma/README.md) | Chroma | ✅ Active |
| [VectorDB/milvus](VectorDB/milvus/README.md) | Milvus | ✅ Active |
| [VectorDB/monolith](VectorDB/monolith/README.md) | All providers | ⚠️ Deprecated |

## Streaming Connectors

| Connector | Description |
|-----------|-------------|
| [KafkaStream](KafkaStream/README.md) | Kafka streaming trigger and producer activity |
| [SSE](SSE/) | Server-Sent Events trigger and producer activity |

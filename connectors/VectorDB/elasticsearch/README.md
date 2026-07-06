# Elasticsearch VectorDB Connector for TIBCO Flogo

A pure-stdlib-Go Flogo connector for [Elasticsearch](https://www.elastic.co/elasticsearch) 8.x implementing the full `VectorDBClient` interface — no external SDK required.

**Status**: ✅ Active  
**Transport**: REST (stdlib `net/http`)  
**Connector Name**: `elasticsearch-connector`  
**Go Module**: `github.com/mpandav-tibco/flogo-extensions/vectordb-elasticsearch`

## Connection Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| `name` | Yes | — | Unique connection name (registry key) |
| `host` | Yes | `localhost` | Elasticsearch hostname or IP |
| `port` | No | `9200` | HTTP port |
| `username` | No | — | Basic-auth username |
| `password` | No | — | Basic-auth password |
| `apiKey` | No | — | API key (base64 `id:key`, mutually exclusive with username/password) |
| `useTLS` | No | `false` | Enable HTTPS |
| `tlsInsecureSkipVerify` | No | `false` | Skip TLS certificate verification (dev only) |
| `timeoutSeconds` | No | `30` | Per-operation HTTP timeout |
| `maxRetries` | No | `3` | Retries on transient errors |
| `retryBackoffMs` | No | `500` | Base backoff between retries (ms) |
| `enableEmbedding` | No | `false` | Enable shared embedding configuration |
| `embeddingProvider` | No | `OpenAI` | Embedding API provider (OpenAI, Azure OpenAI, Cohere, Ollama) |
| `embeddingAPIKey` | No | — | Embedding service API key |
| `embeddingBaseURL` | No | — | Override embedding endpoint URL |

## Activities

| Activity | Description |
|----------|-------------|
| `createCollection` | Create an Elasticsearch index with `dense_vector` mapping |
| `deleteCollection` | Delete an index and all its documents |
| `listCollections` | List all user-visible indexes (system indexes filtered) |
| `upsertDocuments` | Insert or update documents via `_bulk` API |
| `ingestDocuments` | Embed raw text and upsert in one step |
| `getDocument` | Retrieve a single document by ID |
| `deleteDocuments` | Delete documents by ID list via `_bulk` |
| `deleteByFilter` | Delete documents matching metadata filter via `_delete_by_query` |
| `scrollDocuments` | Paginate through documents with `from`/`size` |
| `countDocuments` | Count documents with optional metadata filter via `_count` |
| `vectorSearch` | k-NN dense vector search via `knn` query |
| `hybridSearch` | Combined k-NN + BM25 `multi_match` with `alpha` blend weight |
| `ragQuery` | Full RAG pipeline: embed → search → format context for LLM |
| `createEmbeddings` | Generate embeddings from text |
| `rerank` | Cross-encoder reranking (Cohere, Jina) |

## Quick Start

```bash
# Start Elasticsearch with security disabled (dev/test only)
docker compose -f docker-compose.elasticsearch.yml up -d
```

```yaml
# docker-compose.elasticsearch.yml (included)
services:
  elasticsearch:
    image: docker.elastic.co/elasticsearch/elasticsearch:8.13.4
    ports:
      - "9200:9200"
    environment:
      - xpack.security.enabled=false
      - discovery.type=single-node
      - ES_JAVA_OPTS=-Xms512m -Xmx512m
```

Connection settings:
- **Host**: `localhost`
- **Port**: `9200`
- Leave username/password/apiKey blank for dev mode.

## Index Mapping

Each collection (index) is created with the following field mapping:

| Field | ES Type | Notes |
|-------|---------|-------|
| `id` | `keyword` | Document ID |
| `content` | `text` (standard analyzer) | Full-text searchable content |
| `metadata` | `object` (dynamic) | Arbitrary metadata — filters use `metadata.<key>` |
| `embedding` | `dense_vector` | HNSW-indexed; similarity = cosine / l2_norm / dot_product |

## Filter Syntax

Metadata filters use MongoDB-style operators:

```json
{
  "category": "tech",
  "price": { "$gte": 10, "$lt": 100 },
  "tags": { "$in": ["ai", "ml"] }
}
```

Supported operators: `$eq`, `$ne`, `$gt`, `$gte`, `$lt`, `$lte`, `$in`, `$nin`

## Hybrid Search

HybridSearch blends dense k-NN with BM25 keyword scoring using the `alpha` parameter:
- `alpha = 1.0` — pure vector search
- `alpha = 0.0` — pure keyword search
- `alpha = 0.5` — balanced blend (recommended starting point)

Filters are applied to **both** the k-NN and BM25 legs.

## Known Limitations

- Elasticsearch does not support `CREATE INDEX IF NOT EXISTS`. A second `CreateCollection` call on an existing index returns `ErrCodeCollectionExists` — check with `CollectionExists` first.
- `DeleteByFilter` rejects empty/nil filters to prevent accidental full-index deletion.

## Running Tests

```bash
# Unit tests (no Elasticsearch required)
cd connectors/VectorDB/elasticsearch
go test ./...

# Integration tests (requires Elasticsearch on localhost:9200)
docker compose -f docker-compose.elasticsearch.yml up -d
go test -tags integration -v -timeout 120s ./...
```

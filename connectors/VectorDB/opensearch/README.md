# OpenSearch VectorDB Connector for TIBCO Flogo

A pure-stdlib-Go Flogo connector for [OpenSearch](https://opensearch.org/) 2.x implementing the full `VectorDBClient` interface — no external SDK required.

**Status**: ✅ Active  
**Transport**: REST (stdlib `net/http`)  
**Connector Name**: `opensearch-connector`  
**Go Module**: `github.com/mpandav-tibco/flogo-extensions/vectordb-opensearch`

## Connection Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| `name` | Yes | — | Unique connection name (registry key) |
| `host` | Yes | `localhost` | OpenSearch hostname or IP |
| `port` | No | `9200` | HTTP port |
| `username` | No | `admin` | Basic-auth username |
| `password` | No | — | Basic-auth password |
| `useTLS` | No | `false` | Enable HTTPS |
| `tlsInsecureSkipVerify` | No | `false` | Skip TLS certificate verification (dev only) |
| `timeoutSeconds` | No | `30` | Per-operation HTTP timeout |
| `maxRetries` | No | `3` | Retries on transient errors |
| `retryBackoffMs` | No | `500` | Base backoff between retries (ms) |
| `enableEmbedding` | No | `false` | Enable shared embedding configuration |
| `embeddingProvider` | No | `OpenAI` | Embedding API provider |
| `embeddingAPIKey` | No | — | Embedding service API key |
| `embeddingBaseURL` | No | — | Override embedding endpoint URL |

## Activities

| Activity | Description |
|----------|-------------|
| `createCollection` | Create an OpenSearch index with `knn_vector` HNSW mapping |
| `deleteCollection` | Delete an index and all its documents |
| `listCollections` | List all user-visible indexes (system indexes filtered) |
| `upsertDocuments` | Insert or update documents via `_bulk` API |
| `ingestDocuments` | Embed raw text and upsert in one step |
| `getDocument` | Retrieve a single document by ID |
| `deleteDocuments` | Delete documents by ID list via `_bulk` |
| `deleteByFilter` | Delete documents matching metadata filter via `_delete_by_query` |
| `scrollDocuments` | Paginate through documents with `from`/`size` |
| `countDocuments` | Count documents with optional metadata filter via `_count` |
| `vectorSearch` | k-NN dense vector search |
| `hybridSearch` | Combined k-NN + BM25 `multi_match` with `alpha` blend weight and metadata filters |
| `ragQuery` | Full RAG pipeline: embed → search → format context for LLM |
| `createEmbeddings` | Generate embeddings from text |
| `rerank` | Cross-encoder reranking (Cohere, Jina) |

## Quick Start

```bash
# Start OpenSearch with security disabled (dev/test only)
docker compose -f docker-compose.opensearch.yml up -d
```

```yaml
# docker-compose.opensearch.yml (included)
services:
  opensearch:
    image: opensearchproject/opensearch:2.13.0
    ports:
      - "19201:9200"
    environment:
      - DISABLE_SECURITY_PLUGIN=true
      - discovery.type=single-node
      - OPENSEARCH_JAVA_OPTS=-Xms512m -Xmx512m
```

Connection settings:
- **Host**: `localhost`
- **Port**: `19201` (docker-compose offset; use `9200` for bare-metal install)

## Index Mapping

Each collection (index) is created with the following field mapping:

| Field | OS Type | Notes |
|-------|---------|-------|
| `id` | `keyword` | Document ID |
| `content` | `text` (standard analyzer) | Full-text searchable content |
| `metadata` | `object` (dynamic) | Arbitrary metadata — filters use `metadata.<key>` |
| `embedding` | `knn_vector` | HNSW via `lucene` engine; space type = cosinesimil / l2 / innerproduct |

> **Important**: OpenSearch uses `knn_vector` (not `dense_vector` like Elasticsearch). The index must be created with `"index.knn": true`.

## Distance Metrics

| Generic | OpenSearch `space_type` |
|---------|------------------------|
| `cosine` | `cosinesimil` |
| `euclidean` | `l2` |
| `dot` | `innerproduct` |

## Filter Syntax

Metadata filters use MongoDB-style operators mapped to OpenSearch bool/must queries:

```json
{
  "category": "tech",
  "price": { "$gte": 10 },
  "tags": { "$in": ["ai", "ml"] }
}
```

Supported operators: `$eq`, `$ne`, `$gt`, `$gte`, `$lt`, `$lte`, `$in`

## Hybrid Search

HybridSearch uses OpenSearch's `bool/should` query combining k-NN and `multi_match`. Metadata filters are applied as `bool/filter` clauses (no scoring impact):
- `alpha = 1.0` — pure vector
- `alpha = 0.0` — pure keyword
- `alpha = 0.5` — balanced (recommended)

## Known Limitations

- `DeleteByFilter` rejects empty/nil filters to prevent accidental full-index deletion.
- OpenSearch's default refresh interval is 1 second — after bulk operations, allow at least 1.5–2s before counting or querying.
- OpenSearch uses basic auth only (no API key in OSS builds); API key support requires OpenSearch Security plugin.

## Running Tests

```bash
# Unit tests (no OpenSearch required)
cd connectors/VectorDB/opensearch
go test ./...

# Integration tests (requires OpenSearch on localhost:19201)
docker compose -f docker-compose.opensearch.yml up -d
go test -tags integration -v -timeout 120s ./...
```

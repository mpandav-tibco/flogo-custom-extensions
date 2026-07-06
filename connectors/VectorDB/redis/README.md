# Redis VectorDB Connector for TIBCO Flogo

A Flogo connector for [Redis Stack](https://redis.io/docs/stack/) — Redis with the RediSearch and vector similarity modules. Implements the full `VectorDBClient` interface using HNSW vector indexes stored as Redis hashes.

**Status**: ✅ Active  
**Go Client**: `github.com/redis/go-redis/v9`  
**Transport**: Redis RESP3 protocol  
**Connector Name**: `redis-connector`  
**Go Module**: `github.com/mpandav-tibco/flogo-extensions/vectordb-redis`

> **Requires Redis Stack** — the standard Redis image does not include the vector module. Use `redis/redis-stack:latest` or `redis/redis-stack-server:latest`.

## Connection Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| `name` | Yes | — | Unique connection name (registry key) |
| `host` | Yes | `localhost` | Redis hostname or IP |
| `port` | No | `6379` | Redis port |
| `password` | No | — | Redis AUTH password (empty = no auth) |
| `redisDB` | No | `0` | Redis database index (0–15) |
| `timeoutSeconds` | No | `30` | Per-operation timeout |
| `maxRetries` | No | `3` | Retries on transient errors |
| `retryBackoffMs` | No | `500` | Base backoff between retries (ms) |
| `enableEmbedding` | No | `false` | Enable shared embedding configuration |
| `embeddingProvider` | No | `OpenAI` | Embedding API provider |
| `embeddingAPIKey` | No | — | Embedding service API key |
| `embeddingBaseURL` | No | — | Override embedding endpoint URL |

## Activities

| Activity | Description |
|----------|-------------|
| `createCollection` | Create a RediSearch index with HNSW vector field |
| `deleteCollection` | Drop the RediSearch index and all associated hash keys |
| `listCollections` | List all RediSearch indexes via `FT._LIST` |
| `upsertDocuments` | Write documents as Redis hashes with HSET (pipelined) |
| `ingestDocuments` | Embed raw text and upsert in one step |
| `getDocument` | Retrieve a single document hash by ID |
| `deleteDocuments` | Delete Redis hash keys by ID list |
| `deleteByFilter` | Delete documents matching a metadata filter (scroll + delete) |
| `scrollDocuments` | Paginate through documents via `FT.SEARCH` |
| `countDocuments` | Count documents with optional metadata filter via `FT.SEARCH` |
| `vectorSearch` | KNN HNSW vector search via `FT.SEARCH` vector range |
| `hybridSearch` | Combined HNSW vector + BM25 full-text hybrid search with alpha blend |
| `ragQuery` | Full RAG pipeline: embed query → vector search → format context for LLM |
| `createEmbeddings` | Generate embeddings from text (OpenAI, Azure OpenAI, Cohere, Ollama) |
| `rerank` | Cross-encoder reranking for improved retrieval precision (Cohere, Jina) |

## Quick Start

```bash
# Redis Stack (includes RediSearch + vector module)
docker run -d --name redis-stack \
  -p 6379:6379 \
  redis/redis-stack-server:latest
```

Connection settings in Flogo Designer:
- **Host**: `localhost`
- **Port**: `6379`
- **Password**: *(empty)*
- **Redis DB**: `0`

## Data Model

Documents are stored as Redis hashes with the key pattern `{indexName}:{docID}`:

| Hash field | Description |
|------------|-------------|
| `id` | Document ID (TAG field in RediSearch schema) |
| `content` | Text content (TEXT field — full-text indexed) |
| `metadata` | JSON-encoded metadata (TEXT field — BM25 indexed) |
| `embedding` | Little-endian IEEE 754 float32 bytes (VECTOR HNSW field) |

The RediSearch index schema enables both vector KNN queries and full-text search over `content` in a single `FT.SEARCH` call.

## Distance Metrics

| Generic | Redis Stack metric |
|---------|--------------------|
| `cosine` | `COSINE` |
| `euclidean` | `L2` |
| `dot` | `IP` (inner product) |

## Filter Syntax

Metadata filters use MongoDB-style operators mapped to RediSearch query syntax:

```json
{ "category": "tech" }
{ "price": { "$gte": 10, "$lt": 100 } }
{ "tags": { "$in": ["ai", "ml"] } }
```

Supported operators: `$eq`, `$ne`, `$gt`, `$gte`, `$lt`, `$lte`, `$in`

Filters are applied as RediSearch filter clauses combined with the KNN query.

## Hybrid Search

HybridSearch combines HNSW vector KNN with RediSearch BM25 full-text scoring. The `alpha` parameter controls the blend:

- `alpha = 1.0` — pure vector results
- `alpha = 0.0` — pure keyword results
- `alpha = 0.5` — balanced (recommended)

Both the vector and keyword legs are executed in a single `FT.SEARCH` query for minimal latency.

## Known Limitations

- Requires **Redis Stack** — the core Redis image does not include RediSearch or the vector module.
- `deleteByFilter` performs a client-side scroll + delete (Redis has no server-side filter-delete for hashes).
- Metadata stored as a JSON string in the `metadata` field limits filter expressiveness compared to JSONB (PostgreSQL). Numeric range filters are supported via RediSearch numeric field mapping for known fields.
- Redis database index (`redisDB`) must be between 0 and 15.

## Running Tests

```bash
# Unit tests (no Redis required)
cd connectors/VectorDB/redis
go test ./...

# Integration tests (requires Redis Stack on localhost:6379)
docker run -d --name redis-stack -p 6379:6379 redis/redis-stack-server:latest
go test -tags integration -v -timeout 120s ./...
```

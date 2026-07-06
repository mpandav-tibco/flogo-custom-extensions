# pgvector VectorDB Connector for TIBCO Flogo

A Flogo connector for [pgvector](https://github.com/pgvector/pgvector) — the open-source PostgreSQL extension that adds native vector similarity search. Stores vectors alongside relational data with full ACID guarantees and rich metadata filtering via JSONB.

**Status**: ✅ Active  
**Go Client**: `github.com/jackc/pgx/v5` (native PostgreSQL driver, no cgo)  
**Transport**: PostgreSQL wire protocol (pgx connection pool)  
**Connector Name**: `pgvector-connector`  
**Go Module**: `github.com/mpandav-tibco/flogo-extensions/vectordb-pgvector`

## Connection Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| `name` | Yes | — | Unique connection name (registry key) |
| `host` | Yes | — | PostgreSQL hostname or IP |
| `port` | No | `5432` | PostgreSQL port |
| `username` | No | — | PostgreSQL username |
| `password` | No | — | PostgreSQL password |
| `dbName` | No | `postgres` | Target database |
| `sslMode` | No | `disable` | TLS mode: `disable`, `require`, `verify-ca`, `verify-full` |
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
| `createCollection` | Create a PostgreSQL table with pgvector column and HNSW index |
| `deleteCollection` | Drop the table and all its data |
| `listCollections` | List all vector collections (tables) in the public schema |
| `upsertDocuments` | Insert or update documents (INSERT … ON CONFLICT DO UPDATE) |
| `ingestDocuments` | Embed raw text and upsert in one step |
| `getDocument` | Retrieve a single document by ID |
| `deleteDocuments` | Delete documents by ID list |
| `deleteByFilter` | Delete documents matching a JSONB metadata filter |
| `scrollDocuments` | Paginate through all documents with LIMIT/OFFSET |
| `countDocuments` | Count documents with optional JSONB metadata filter |
| `vectorSearch` | ANN vector search using HNSW index (cosine / L2 / inner product) |
| `hybridSearch` | Combined pgvector ANN + PostgreSQL full-text search (ts_rank) with alpha blend |
| `ragQuery` | Full RAG pipeline: embed query → vector search → format context for LLM |
| `createEmbeddings` | Generate embeddings from text (OpenAI, Azure OpenAI, Cohere, Ollama) |
| `rerank` | Cross-encoder reranking for improved retrieval precision (Cohere, Jina) |

## Quick Start

```bash
# PostgreSQL + pgvector extension
docker run -d --name pgvector \
  -p 5432:5432 \
  -e POSTGRES_PASSWORD=postgres \
  pgvector/pgvector:pg16
```

Connection settings in Flogo Designer:
- **Host**: `localhost`
- **Port**: `5432`
- **Username**: `postgres`
- **Password**: `postgres`
- **DB Name**: `postgres`
- **SSL Mode**: `disable`

## Table Schema

Each collection is created as a PostgreSQL table:

| Column | Type | Notes |
|--------|------|-------|
| `id` | `TEXT PRIMARY KEY` | Document ID |
| `content` | `TEXT` | Text content |
| `metadata` | `JSONB` | Arbitrary metadata — fully indexable and filterable |
| `embedding` | `vector(N)` | Dense vector of dimension N |
| `content_tsv` | `tsvector` (generated) | Auto-updated for full-text search |

An HNSW index is created on the `embedding` column using the appropriate operator class for the configured distance metric.

## Distance Metrics

| Generic | PostgreSQL operator class | `<=>` operator |
|---------|--------------------------|----------------|
| `cosine` | `vector_cosine_ops` | cosine distance |
| `euclidean` | `vector_l2_ops` | Euclidean (L2) distance |
| `dot` | `vector_ip_ops` | inner product (negated) |

## Filter Syntax

Metadata filters use MongoDB-style operators mapped to PostgreSQL JSONB path queries:

```json
{ "category": "tech" }
{ "price": { "$gte": 10, "$lt": 100 } }
{ "tags": { "$in": ["ai", "ml"] } }
```

Supported operators: `$eq`, `$ne`, `$gt`, `$gte`, `$lt`, `$lte`, `$in`, `$nin`

All filters operate on the `metadata` JSONB column with full type-awareness — numeric ranges and exact matches are both natively supported.

## Hybrid Search

HybridSearch blends pgvector ANN cosine similarity with PostgreSQL `ts_rank` full-text scoring using the `alpha` parameter:

- `alpha = 1.0` — pure vector search
- `alpha = 0.0` — pure keyword search  
- `alpha = 0.5` — balanced blend (recommended)

The `content_tsv` generated column is indexed automatically and updated on every upsert.

## Known Limitations

- Requires PostgreSQL with the `pgvector` extension (`CREATE EXTENSION vector`). The Docker image `pgvector/pgvector:pg16` includes it pre-installed.
- HNSW index build time scales with dataset size — large initial loads may be slower than approximate indexes on dedicated vector DBs.
- `sslMode = "verify-ca"` / `"verify-full"` requires the CA certificate to be trusted in the OS cert store.

## Running Tests

```bash
# Unit tests (no PostgreSQL required)
cd connectors/VectorDB/pgvector
go test ./...

# Integration tests (requires pgvector on localhost:5432)
docker run -d --name pgvector -p 5432:5432 -e POSTGRES_PASSWORD=postgres pgvector/pgvector:pg16
go test -tags integration -v -timeout 120s ./...
```

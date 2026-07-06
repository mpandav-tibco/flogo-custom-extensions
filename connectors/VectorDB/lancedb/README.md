# LanceDB VectorDB Connector for TIBCO Flogo

A pure-stdlib-Go Flogo connector for [LanceDB](https://lancedb.github.io/lancedb/) implementing the full `VectorDBClient` interface via a custom REST server — no external SDK required.

**Status**: ✅ Active  
**Transport**: REST (stdlib `net/http`)  
**Connector Name**: `lancedb-connector`  
**Go Module**: `github.com/mpandav-tibco/flogo-extensions/vectordb-lancedb`

## Connection Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| `name` | Yes | — | Unique connection name (registry key) |
| `host` | Yes | `localhost` | LanceDB server hostname or IP |
| `port` | No | `8181` | Server port (set to `0` for LanceDB Cloud) |
| `scheme` | No | `http` | `http` (self-hosted) or `https` (cloud) |
| `apiKey` | No | — | Bearer token (required for LanceDB Cloud) |
| `region` | No | — | LanceDB Cloud region |
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
| `createCollection` | Create a LanceDB table with fixed-size embedding schema |
| `deleteCollection` | Drop a table and all its data |
| `listCollections` | List all tables |
| `upsertDocuments` | Insert or update documents via `merge_insert` |
| `ingestDocuments` | Embed raw text and upsert in one step |
| `getDocument` | Retrieve a single document by ID |
| `deleteDocuments` | Delete documents by ID list |
| `deleteByFilter` | Delete documents matching metadata filter |
| `scrollDocuments` | Paginate through documents with offset |
| `countDocuments` | Count total documents (no-filter path uses server endpoint; filtered path is client-side) |
| `vectorSearch` | ANN dense vector search with configurable metric |
| `hybridSearch` | Dense vector + full-text search with RRF fusion |
| `ragQuery` | Full RAG pipeline: embed → search → format context for LLM |
| `createEmbeddings` | Generate embeddings from text |
| `rerank` | Cross-encoder reranking (Cohere, Jina) |

## Quick Start — Self-Hosted (Docker)

LanceDB does not publish a standalone server Docker image. This connector ships a minimal FastAPI server that runs LanceDB in embedded mode:

```bash
# Build and start the local LanceDB REST server
docker compose -f docker-compose.lancedb.yml up -d --build

# The server listens on port 18181 (host) → 8181 (container)
```

Connection settings:
- **Host**: `localhost`
- **Port**: `18181`
- **Scheme**: `http`

### Local Server API

The bundled `docker/server.py` implements the following endpoints:

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/table/` | List tables |
| `GET` | `/v1/table/{name}/` | Describe table (200) or 404 |
| `POST` | `/v1/table/{name}/` | Create table |
| `DELETE` | `/v1/table/{name}/` | Drop table |
| `GET` | `/v1/table/{name}/count_rows/` | Count all rows |
| `POST` | `/v1/table/{name}/insert/` | Append records |
| `POST` | `/v1/table/{name}/merge_insert/` | Upsert records |
| `POST` | `/v1/table/{name}/delete/` | Delete by SQL predicate |
| `POST` | `/v1/table/{name}/query/` | Vector / FTS / full-scan search |

## Quick Start — LanceDB Cloud

Set `port = 0`, `scheme = https`, and provide your `apiKey` and `region`:
- **Host**: `<db-name>.lancedb.com`
- **Port**: `0`
- **Scheme**: `https`
- **API Key**: your LanceDB Cloud API key

## Table Schema

Every collection is created with a fixed schema:

| Column | Type | Notes |
|--------|------|-------|
| `id` | `utf8` | Document ID (required) |
| `content` | `utf8` | Text content |
| `metadata` | `utf8` | JSON-encoded metadata payload |
| `embedding` | `fixed_size_list<float32>[N]` | Dense vector of dimension N |

## Distance Metrics

Pass the metric via `Filters["_metric"]` in `VectorSearch`:

| Value | Description |
|-------|-------------|
| `cosine` (default) | Cosine similarity |
| `l2` | Euclidean distance; score = `1 / (1 + distance)` |
| `dot` | Dot product; score = `-distance` (LanceDB negates internally) |

## Metadata Filter Syntax

Since `metadata` is stored as a JSON string, filters use SQL LIKE matching:

```json
{ "category": "tech" }
{ "status": { "$eq": "active" } }
{ "status": { "$ne": "deleted" } }
{ "tags": { "$in": ["ai", "ml"] } }
```

Supported operators: `$eq`, `$ne`, `$in`, `$nin`

> **Limitation**: Numeric range operators (`$gt`, `$gte`, `$lt`, `$lte`) are not supported because the `metadata` column is a JSON string without structured extraction. For range filtering on numeric fields, use LanceDB Cloud with a structured schema.

## Hybrid Search

HybridSearch runs dense vector search and full-text search (FTS) in parallel, then fuses results with Reciprocal Rank Fusion (RRF):
- `alpha = 1.0` — pure vector results
- `alpha = 0.0` — pure text results
- `alpha = 0.5` — balanced (recommended)

FTS falls back to vector-only if the full-text index is not yet built. Metadata filters are applied to both the dense and FTS legs.

## Known Limitations

- `CountDocuments` with filters performs a client-side count (fetches all matching IDs). Use sparingly on very large collections.
- `metadata` stored as JSON string limits filter expressiveness (no native range queries).
- `DeleteByFilter` returns `-1` for the deleted count (LanceDB does not report delete counts via the REST API).

## Running Tests

```bash
# Unit tests (no LanceDB required)
cd connectors/VectorDB/lancedb
go test ./...

# Integration tests (requires local server on localhost:18181)
docker compose -f docker-compose.lancedb.yml up -d --build
go test -tags integration -v -timeout 120s ./...
```

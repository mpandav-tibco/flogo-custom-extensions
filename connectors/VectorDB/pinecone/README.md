# Pinecone VectorDB Connector for TIBCO Flogo

A pure-stdlib-Go Flogo connector for [Pinecone](https://www.pinecone.io/) — the fully-managed, cloud-native vector database. Implements the full `VectorDBClient` interface via the Pinecone REST API — no external SDK required.

**Status**: ✅ Active  
**Transport**: REST (stdlib `net/http`)  
**Connector Name**: `pinecone-connector`  
**Go Module**: `github.com/mpandav-tibco/flogo-extensions/vectordb-pinecone`

## Connection Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| `name` | Yes | — | Unique connection name (registry key) |
| `apiKey` | Yes* | — | Pinecone API key. Not required when `scheme = http` (local emulator). |
| `host` | No | `api.pinecone.io` | Pinecone control-plane hostname |
| `scheme` | No | `https` | `https` for Pinecone Cloud; `http` for Pinecone Local emulator |
| `pineconeCloud` | No | `aws` | Cloud provider for serverless indexes: `aws`, `gcp`, `azure` |
| `pineconeRegion` | No | `us-east-1` | Cloud region for serverless indexes |
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
| `createCollection` | Create a Pinecone serverless index; polls until `Ready` |
| `deleteCollection` | Delete a serverless index and all its vectors |
| `listCollections` | List all indexes in the Pinecone project |
| `upsertDocuments` | Insert or update vectors via the Pinecone upsert API |
| `ingestDocuments` | Embed raw text and upsert in one step |
| `getDocument` | Retrieve a single vector by ID |
| `deleteDocuments` | Delete vectors by ID list |
| `deleteByFilter` | Delete vectors matching a metadata filter |
| `scrollDocuments` | Paginate through all vectors via Pinecone list + fetch |
| `countDocuments` | Count total vectors (filtered count uses stats endpoint) |
| `vectorSearch` | ANN similarity search with dense query vector |
| `hybridSearch` | Combined dense + sparse (BM25-equivalent) hybrid search with alpha blend |
| `ragQuery` | Full RAG pipeline: embed query → vector search → format context for LLM |
| `createEmbeddings` | Generate embeddings from text (OpenAI, Azure OpenAI, Cohere, Ollama) |
| `rerank` | Cross-encoder reranking for improved retrieval precision (Cohere, Jina) |

## Quick Start — Pinecone Cloud

1. Sign up at [pinecone.io](https://app.pinecone.io)
2. Create an API key in the Pinecone console
3. Set the connection settings in Flogo Designer:
   - **API Key**: your Pinecone API key
   - **Host**: `api.pinecone.io`
   - **Scheme**: `https`
   - **Cloud**: `aws` (or `gcp` / `azure`)
   - **Region**: `us-east-1` (or your preferred region)

## Quick Start — Pinecone Local (Dev/Test)

```bash
docker run -d --name pinecone-local \
  -p 5080:5080 \
  ghcr.io/pinecone-io/pinecone-local:latest
```

Connection settings:
- **Scheme**: `http`
- **Host**: `localhost`
- **Port**: `5080` (set via host field as `localhost:5080`)
- **API Key**: leave blank

## Index Architecture

Pinecone uses **serverless indexes** (the default and only type supported by this connector). Each collection maps to one serverless index. Index host URLs are resolved and cached on first use — no round-trip on subsequent operations.

Data plane (upsert, query, fetch, delete) calls go directly to the index host; control plane (create, delete, list indexes) calls go to `api.pinecone.io`.

## Distance Metrics

| Generic | Pinecone metric |
|---------|----------------|
| `cosine` | `cosine` |
| `euclidean` | `euclidean` |
| `dot` | `dotproduct` |

## Filter Syntax

Metadata filters use MongoDB-style operators mapped to Pinecone's native filter format:

```json
{ "category": "tech" }
{ "price": { "$gte": 10, "$lt": 100 } }
{ "tags": { "$in": ["ai", "ml"] } }
```

Supported operators: `$eq`, `$ne`, `$gt`, `$gte`, `$lt`, `$lte`, `$in`, `$nin`

Metadata must be set at upsert time — only key-value pairs in the `payload` field are stored.

## Hybrid Search

Pinecone supports native sparse-dense hybrid search. `hybridSearch` sends both a dense query vector and a sparse BM25 vector to Pinecone's query API. The `alpha` parameter controls the blend:

- `alpha = 1.0` — pure dense (vector)
- `alpha = 0.0` — pure sparse (keyword)
- `alpha = 0.5` — balanced (recommended)

Hybrid search requires the index to be created with the `dotproduct` metric (Pinecone's requirement for hybrid indexes).

## Known Limitations

- Pinecone is cloud-only — no self-hosted option via this connector (use the Pinecone Local Docker image for dev/test).
- `createCollection` polls up to 2 minutes waiting for the index to reach `Ready` state. New index creation typically takes 30–90 seconds.
- `scrollDocuments` uses Pinecone's `list` API (returns IDs only) followed by batched `fetch` calls — large collections may require multiple round-trips.
- `CountDocuments` with filters uses the index stats endpoint, which may have a few-second delay after upserts.

## Running Tests

```bash
# Unit tests (no Pinecone required)
cd connectors/VectorDB/pinecone
go test ./...

# Integration tests (requires Pinecone Cloud API key or Local emulator)
export PINECONE_API_KEY=your-api-key
go test -tags integration -v -timeout 300s ./...
```

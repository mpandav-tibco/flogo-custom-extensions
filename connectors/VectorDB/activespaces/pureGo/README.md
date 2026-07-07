# ActiveSpaces VectorDB Connector — Gateway (pure-Go) for TIBCO Flogo

A pure-Go Flogo connector for the **TIBCO ActiveSpaces 5.2 vector store**. It talks over HTTP to a
small **[vector gateway](https://github.com/mpandav-tibco/activespaces-vector-gateway)** — kept in
its own repository — that isolates the native ActiveSpaces client in a container. As a result the
connector, and any Flogo app that uses it, stays **pure Go and runs on any OS/architecture**.

It is the **gateway sibling** of the [ActiveSpaces Native (tibdg/CGO) connector](../nativeAS/README.md);
both expose the identical 14-activity surface and differ only in *how* they reach the grid.
See the [connector comparison](../README.md) to choose between them.

**Status**: ✅ Active
**Transport**: REST/JSON over HTTP(S) to the vector gateway
**Platform**: any OS/architecture (`CGO_ENABLED=0`)
**Connector Name**: `activespaces-gateway-connector`

## Connection Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **Name** | Yes | — | Unique connection name referenced by activities |
| **Gateway Host** | Yes | — | Host of the ActiveSpaces vector gateway |
| **Gateway Port** | No | `8081` | Gateway HTTP port |
| **Grid Name** | No | `_default` | ActiveSpaces data grid name |
| **API Key** | No | — | Optional bearer token sent to the gateway |
| **Secure Connection** | No | `false` | Use HTTPS to the gateway |
| **CA / Client Cert / Client Key** | No | — | TLS / mTLS material for the gateway connection |
| **Timeout (s)** | No | `30` | Per-operation timeout |
| **Max Retries** | No | `3` | Retries on transient errors |
| **Retry Backoff (ms)** | No | `500` | Wait between retries |
| **Embedding Provider** | No | — | Optional shared embedding config inherited by RAG / Ingest activities |

## Activities

| Activity | Description |
|----------|-------------|
| `createCollection` | Create a new vector collection / index |
| `deleteCollection` | Permanently delete a collection and all its data |
| `listCollections` | List all collections in the grid |
| `upsertDocuments` | Insert or update documents with pre-computed vectors |
| `ingestDocuments` | Embed raw text and upsert in one step |
| `getDocument` | Retrieve a single document by ID |
| `deleteDocuments` | Delete documents by ID list or metadata filter |
| `scrollDocuments` | Paginate through all documents without a query vector |
| `countDocuments` | Count documents with an optional metadata filter |
| `vectorSearch` | Semantic ANN search with a dense query vector |
| `hybridSearch` | Combined dense + keyword search |
| `ragQuery` | Full RAG pipeline: embed query → vector search → format context for LLM |
| `createEmbeddings` | Generate embeddings from text (OpenAI, Azure OpenAI, Cohere, Ollama) |
| `rerank` | Cross-encoder reranking for improved retrieval precision (Cohere, Jina) |

## Behavior

- The connector never sends SQL — it posts a provider-agnostic filter map to the gateway, which compiles it to ActiveSpaces SQL. Operators: `$eq` (implicit), `$ne`, `$gt`, `$gte`, `$lt`, `$lte`, `$in`, `$like` — e.g. `{ "year": { "$gte": 2020 }, "tags": { "$in": ["rag", "llm"] } }`
- Vector search uses `cosine_similarity` / `l2_distance` / `dot_product_similarity`; `hybridSearch` falls back to dense vector search (ActiveSpaces has no native keyword index)
- Deletes use the ActiveSpaces table API (AS has no SQL `DELETE`)
- Embedding and rerank activities call the external provider directly (OpenAI, Azure OpenAI, Cohere, Ollama, Jina) — only vector-store operations go through the gateway

## Quick Start

1. Start the gateway + a dev grid — the gateway is maintained in
   [its own repository](https://github.com/mpandav-tibco/activespaces-vector-gateway):

   ```bash
   git clone https://github.com/mpandav-tibco/activespaces-vector-gateway
   cd activespaces-vector-gateway
   export TIBCO_LICENSE_FILE=/absolute/path/to/tibco_license.bin
   docker compose -f deploy/docker-compose.yml up --build
   # Gateway REST API on http://localhost:8081
   ```

2. Configure the connection in Flogo Designer:
   - **Gateway Host**: `localhost`
   - **Gateway Port**: `8081`
   - **Grid Name**: `_default`

## Building

Pure Go — no native dependencies:

```bash
cd connectors/VectorDB/activespaces/pureGo
CGO_ENABLED=0 GOFLAGS="-mod=mod" GOTOOLCHAIN=auto go build ./...
```

The [`activespaces-gateway-suite`](../../../../examples/vectordb/activespaces-gateway-suite.flogo)
example exercises all 14 activities across 4 REST endpoints.

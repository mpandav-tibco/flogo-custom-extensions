# ActiveSpaces VectorDB Connector — Native (tibdg/CGO) for TIBCO Flogo

A Flogo connector for the **TIBCO ActiveSpaces 5.2 vector store** that links the native
ActiveSpaces client (**tibdg**) directly, in-process, via CGO — with no gateway hop.

It is the **native sibling** of the [ActiveSpaces Gateway (pure-Go) connector](../pureGo/README.md);
both expose the identical 14-activity surface and differ only in *how* they reach the grid.
See the [connector comparison](../README.md) to choose between them.

**Status**: ✅ Active
**Go Client**: `tibco.com/tibdg` — native TIBCO ActiveSpaces client (CGO)
**Transport**: In-process, native libraries (`-ltibdg -ltib -ltibutil`)
**Platform**: linux/amd64 only — requires the ActiveSpaces SDK at build & run time
**Connector Name**: `activespaces-native-connector`

## Connection Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **Name** | Yes | — | Unique connection name referenced by activities |
| **Realm Server Host** | Yes | — | ActiveSpaces realm (FTL) server host or IP |
| **Realm Server Port** | No | `8080` | Realm server port |
| **Grid Name** | No | `_default` | ActiveSpaces data grid name |
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

- A collection maps to an ActiveSpaces table: `id VARCHAR PRIMARY KEY, content VARCHAR, embedding VECTOR_FLOAT32(dim), metadata VARCHAR`
- Vector search runs as ActiveSpaces SQL using `cosine_similarity` / `l2_distance` / `dot_product_similarity`
- ActiveSpaces has no SQL `DELETE`, so document deletes use the table API (`DeleteRows`)
- `hybridSearch` falls back to dense vector search (ActiveSpaces has no native keyword index)
- Embedding and rerank activities call the external provider directly (OpenAI, Azure OpenAI, Cohere, Ollama, Jina)

## Quick Start

Because the connector links the AS SDK via CGO, build it on linux/amd64 with the SDK present.
The [`Dockerfile`](Dockerfile) builds it on top of the AS SDK image:

```bash
docker build -t as-native-connector .
```

Connection settings in Flogo Designer:
- **Realm Server Host**: `localhost`
- **Realm Server Port**: `8080`
- **Grid Name**: `_default`

## The tibdg dependency

The native client `tibco.com/tibdg` is TIBCO proprietary SDK source and is **not committed to
this repository**. `go.mod` maps it to a git-ignored local path:

```
replace tibco.com/tibdg => ./dep/tibco.com/tibdg
```

The `Dockerfile` supplies it automatically from the AS SDK base image. To build on a bare host,
copy it from your own AS SDK first:

```bash
export TIBDG_ROOT=/opt/tibco/as/current-version
cp -R "$TIBDG_ROOT/samples/golang/src/tibco.com/tibdg" dep/tibco.com/tibdg
```

## Building from source

```bash
cd connectors/VectorDB/activespaces/nativeAS
export TIBDG_ROOT=/opt/tibco/as/current-version CGO_ENABLED=1
export CGO_CFLAGS="-I$TIBDG_ROOT/include -I$TIBDG_ROOT/ftl/include"
export CGO_LDFLAGS="-L$TIBDG_ROOT/lib -L/opt/tibco/ftl/7.2/lib -Wl,-rpath,$TIBDG_ROOT/lib,-rpath,/opt/tibco/ftl/7.2/lib"
GOFLAGS="-mod=mod" GOTOOLCHAIN=auto go build ./...
```

A Flogo **app** that uses this connector must likewise be built on linux/amd64 with the AS SDK
present — build it inside the AS SDK container (or a CI image based on it). The
[`activespaces-native-suite`](../../../../examples/vectordb/activespaces-native-suite.flogo)
example exercises all 14 activities against a live grid.

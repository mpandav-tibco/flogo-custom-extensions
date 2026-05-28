# VectorDB RAG Connector (Monolith) — ⚠️ Deprecated

> **This connector is deprecated.** It receives no new features or provider updates.
> Use the dedicated per-provider connectors instead:
> - [qdrant](../qdrant/README.md)
> - [weaviate](../weaviate/README.md)
> - [chroma](../chroma/README.md)
> - [milvus](../milvus/README.md)

A multi-provider vector database connector for TIBCO Flogo that supports Qdrant, Weaviate, Chroma, and Milvus through a single shared connection and activity set. Kept here for reference and backward compatibility only.

## Migration to Dedicated Connectors

| Step | Action |
|------|--------|
| 1 | In your `.flogo` app, delete the existing `vectordb-connector` connection |
| 2 | Add the dedicated connector as a new import (`vectordb-qdrant`, `vectordb-weaviate`, etc.) |
| 3 | Create a new connection using the dedicated connector (`qdrant-connector`, etc.) |
| 4 | Update the `connection` setting on each activity to reference the new connection |
| 5 | Remove the `vectordb` import from the app |

The activity input/output interface is identical across all connectors — only the connection reference changes.

## Supported Providers

| Provider | Transport | Notes |
|----------|-----------|-------|
| **Qdrant** | gRPC + REST | Full feature support including native hybrid search |
| **Weaviate** | REST / GraphQL | Native hybrid search; filter delete via batch GraphQL |
| **Chroma** | REST (v2 API) | Scroll and hybrid search are client-side |
| **Milvus** | gRPC | Boolean expression filters |

## Connector Settings

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| **Name** | Yes | — | Unique connection name |
| **DB Provider** | Yes | — | `qdrant`, `weaviate`, `chroma`, or `milvus` |
| **Host** | Yes | — | Hostname or IP of the VectorDB server |
| **Port** | No | `0` | REST/HTTP port (provider default used when `0`) |
| **gRPC Port** | No | `6334` | Qdrant gRPC port (ignored for other providers) |
| **HTTP Scheme** | No | `http` | `http` or `https` (Weaviate) |
| **Username** | No | — | Milvus username |
| **Password** | No | — | Milvus password |
| **Database Name** | No | `default` | Milvus database name |
| **API Key** | No | — | API key or bearer token |
| **Use TLS** | No | `false` | Enable TLS/SSL |
| **Timeout (s)** | No | `30` | Per-operation timeout |
| **Max Retries** | No | `3` | Retries on transient errors |
| **Retry Backoff (ms)** | No | `500` | Wait between retries |

## Feature Matrix

| Feature | Qdrant | Weaviate | Chroma | Milvus |
|---------|:------:|:--------:|:------:|:------:|
| Vector Search | ✅ | ✅ | ✅ | ✅ |
| Hybrid Search | ✅ native | ✅ native | ⚠️ fallback | ⚠️ fallback |
| Metadata Filters | ✅ | ✅ | ✅ | ✅ |
| Delete by Filter | ✅ | ✅ | ✅ | ✅ |
| Scroll / Paginate | ✅ native | ✅ native | ⚠️ client-side | ✅ native |
| Count with Filter | ✅ | ❌ | ⚠️ client-side | ✅ |
| TLS / Auth | ✅ | ✅ | ✅ | ✅ |

## Running Tests

```bash
# Unit tests
cd connectors/VectorDB/monolith
CGO_ENABLED=0 GOFLAGS="-mod=mod" GOTOOLCHAIN=auto go test ./...

# Integration tests (requires all 4 providers running)
CGO_ENABLED=0 GOFLAGS="-mod=mod" GOTOOLCHAIN=auto go test -tags integration -v -timeout 300s ./...
```

## Module

```
github.com/mpandav-tibco/flogo-extensions/vectordb
```

---

> Part of the [VectorDB connector family](../README.md) for TIBCO Flogo.

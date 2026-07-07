# ActiveSpaces VectorDB Connectors for TIBCO Flogo

Two Flogo connectors for the **TIBCO ActiveSpaces 5.2 vector store**, exposing the **same
14-activity surface** and the same `VectorDBClient` interface. They differ only in **how** they
reach the grid, so you can pick the right trade-off for your deployment without changing your flows.

| | [`activespaces-gateway/`](../activespaces-gateway/README.md) | [`activespaces-native/`](../activespaces-native/README.md) |
|---|---|---|
| **How it reaches the grid** | HTTP/JSON → a small **vector gateway** container that links AS natively | **Direct, in-process** via the native **tibdg** client (CGO) |
| **Data path** | `app ──HTTP──▶ gateway ──native──▶ grid` | `app ──tibdg (.so)──▶ grid` |
| **Connector build** | Pure Go, `CGO_ENABLED=0` — builds in the standard Flogo toolchain | CGO, **linux/amd64 only**, needs the AS SDK (headers + `.so`) |
| **App portability** | App runs on **any OS/arch**; only the gateway is platform-locked | App is **platform-locked to linux/amd64** + AS SDK at run time |
| **Extra moving parts** | One gateway container to run/operate | None |
| **Latency** | One extra network hop | Lowest (in-process) |
| **Connection target** | Gateway host/port (default `8081`) | Realm/FTL server host/port (default `8080`) |
| **TIBCO proprietary code in repo** | None | None — tibdg is supplied from your AS SDK, never committed |

Both connectors implement identical activities and mappings, so an app can switch between them
by changing only the connection (and rebuilding). See each connector's README for details:

- **[`activespaces-gateway/`](../activespaces-gateway/README.md)** — portable, gateway-based. Start here unless you specifically
  need in-process native access. The gateway (a [separate repository](https://github.com/mpandav-tibco/activespaces-vector-gateway))
  isolates all native concerns in a container, so the Flogo app itself builds and runs anywhere.
- **[`activespaces-native/`](../activespaces-native/README.md)** — native, in-process, lowest latency, no gateway — at the
  cost of a linux/amd64 + AS-SDK build/runtime.

## Which should I use?

- **Use `activespaces-gateway`** for portability, simplest app builds, and when you can run one small gateway
  container next to the grid (or share one across many apps). This is the recommended default.
- **Use `activespaces-native`** when you want to eliminate the gateway hop, are already deploying on
  linux/amd64 with the ActiveSpaces SDK available, and want the lowest latency.

## Activity surface (both)

`createCollection` · `deleteCollection` · `listCollections` · `upsertDocuments` ·
`ingestDocuments` · `getDocument` · `deleteDocuments` · `scrollDocuments` · `countDocuments` ·
`vectorSearch` · `hybridSearch` · `ragQuery` · `createEmbeddings` · `rerank`

Vectors are stored in an ActiveSpaces `VECTOR_FLOAT32(dim)` column; similarity search uses
`cosine_similarity` / `l2_distance` / `dot_product_similarity`.

## Examples

- [`examples/vectordb/activespaces-gateway-suite.flogo`](../../../examples/vectordb/activespaces-gateway-suite.flogo) — Gateway (pure-Go) example: all 14 activities across 4 REST endpoints.
- [`examples/vectordb/activespaces-native-suite.flogo`](../../../examples/vectordb/activespaces-native-suite.flogo) — Native example: all 14 activities across 4 REST endpoints.

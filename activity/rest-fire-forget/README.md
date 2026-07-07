# REST Fire & Forget Activity

![REST Fire & Forget Activity](icons/rest-fire-forget.svg)

Sends an HTTP request to an endpoint and **returns to the flow immediately, without waiting for (or reading) the response**. Use it when you only need to *trigger* a downstream call — webhooks, notifications, cache invalidation, async ingestion — and the response is irrelevant to the flow.

It is the asynchronous counterpart to the standard **Invoke REST Service** activity, which is synchronous (it blocks the flow until the full response is received).

Supports:
- All HTTP methods — `GET`, `POST`, `PUT`, `DELETE`, `PATCH`, `HEAD`, `OPTIONS`
- Non-blocking dispatch — the flow continues in microseconds; the request runs to completion on a detached background context
- Mappable request — `url`, query parameters, headers, and a JSON body, structured like *Invoke REST Service*
- Method-aware body — a design-time handler (`activity.js`) shows the request body only for methods that carry one (`POST`/`PUT`/`PATCH`/`DELETE`), exactly like *Invoke REST Service*
- Bounded concurrency — a configurable semaphore caps in-flight requests and sheds load instead of blocking the flow
- Hardened HTTP client — connection pooling, a TLS-verification toggle, proxy-from-environment, and a per-request send timeout

**Status**: ✅ Active
**Category**: `rest-fire-forget`

---

## How it differs from *Invoke REST Service*

| | Invoke REST Service (OOTB) | REST Fire & Forget (this activity) |
|---|---|---|
| Waits for response | Yes — `client.Do` + reads the full body | **No** — dispatches and returns |
| Flow blocked until | Response received & parsed | Request handed off (microseconds) |
| Outputs | statusCode, headers, responseBody | **None** — nothing to wait for |
| Delivery guarantee | Response confirms receipt | **None** — best effort |

---

## Settings

| Setting | Type | Required | Default | Description |
|---------|------|----------|---------|-------------|
| `method` | dropdown | ✓ | `POST` | HTTP method: `GET` / `POST` / `PUT` / `DELETE` / `PATCH` / `HEAD` / `OPTIONS`. |
| `timeout` | integer | | `30000` | Max time (ms) the background request may run before it is abandoned. Does **not** delay the flow. `0` = 30000 ms. |
| `skipTlsVerify` | boolean | | `false` | Disable TLS certificate verification — **development only**. |
| `maxConcurrentRequests` | integer | | `1000` | Upper bound on in-flight requests. Excess requests are dropped (logged at WARN) rather than blocking the flow. `0` = 1000. |

---

## Inputs

The input structure mirrors the OOTB **Invoke REST Service** activity — `complex_object` *params* editors for headers and query parameters, and a `texteditor` JSON body. A design-time handler (`activity.js`) manages the body per method, exactly like Invoke REST Service.

| Input | Type | Description |
|-------|------|-------------|
| `url` | string (required) | Full endpoint URL, e.g. `https://host/path`. Supports app properties. |
| `queryParams` | complex_object (`params`) | Query parameters appended to the URL — same params editor as *Invoke REST Service*. |
| `headers` | complex_object (`params`) | Request headers, e.g. `Content-Type: application/json` — same params editor as *Invoke REST Service*, pre-seeded with the standard header names. |
| `body` | complex_object (`texteditor`, JSON) | Request payload. **Shown only for `POST` / `PUT` / `PATCH` / `DELETE`** and hidden for `GET` / `HEAD` / `OPTIONS` (managed by `activity.js`). Untyped by default so it is always mappable; paste an example JSON payload for typed, field-level mapping. JSON objects are marshaled and sent as `application/json`. |

> This activity has **no outputs** — there is no response to return. The flow continues as soon as the request is handed off.
>
> **Designer handler (`activity.js`).** Like Invoke REST Service, this activity ships a `wi-studio` design-time handler (`activity.js` + `activity.module.js`) that runs **only in the Flogo designer** (never at runtime). It reveals the **Request Body** for body-carrying methods and hides it for `GET` / `HEAD` / `OPTIONS`, mirroring `methodAllowsBody()` in `activity.go`. Restart the Flogo Design Assistant / reopen the flow after changing these files so the designer reloads them.

---

## Behavior & guarantees

- **Detached context.** The request runs under a background context (with the configured send timeout), so it is *not* cancelled when the flow instance completes — the request still goes out after the activity returns.
- **Bounded concurrency.** A semaphore caps in-flight requests; when saturated, the activity drops the request (logged at WARN) instead of blocking the flow or leaking goroutines.
- **Connection reuse.** A pooled `http.Client` is shared across executions; the response body is drained and closed in the background so connections can be reused.
- **No response, by design.** Dispatch is best-effort — there is no output and no delivery confirmation.

### Caveats

- **No delivery guarantee.** If the engine restarts, in-flight requests are lost. For guaranteed asynchronous delivery, use a message broker / queue.
- **No retries / circuit breaking.** The activity does not observe the response, so it cannot retry on failure. Failures are logged at WARN and otherwise ignored.
- **Tracing.** Distributed-trace headers are not injected (the call is detached from the flow's trace context).

---

## Validation

**At startup (activity initialisation):**

| Check | Behaviour on failure |
|-------|----------------------|
| `method` is a valid HTTP method | Startup error — activity does not initialise. |

**At runtime (each flow invocation):**

| Check | Behaviour on failure |
|-------|----------------------|
| `url` is non-empty and parseable | Request is dropped (logged at WARN); the flow continues. |
| In-flight requests below `maxConcurrentRequests` | Request is dropped (logged at WARN); the flow continues. |

---

## Example

`POST` a webhook notification and continue immediately:

| Setting | Value |
|---------|-------|
| Method | `POST` |
| Send Timeout (ms) | `30000` |

| Input | Value |
|-------|-------|
| `url` | `=$property["WEBHOOK_URL"]` |
| `headers` | `Content-Type: application/json`, `X-Source: flogo` |
| `body` | `={"event":"order.created","id":$flow.orderId}` |

The flow proceeds without waiting — there is no output to inspect; the request is dispatched on a background context.

---

## Running Tests

```bash
cd activity/rest-fire-forget
go test ./...            # unit tests (httptest mock servers, no network required)
go vet ./... && gofmt -l .
```

See [examples/rest-fire-forget/](../../examples/rest-fire-forget/) for a runnable end-to-end demo with a mock receiver service.

---

## Build

Built like the other custom extensions in this repo (single Go module); the activity compiles into a Flogo app via the standard user-extension build.

> **Build note:** the descriptor's `display.category` (`rest-fire-forget`) must match this directory's basename. The Flogo build derives the app's extension-registry entry from the category and resolves it to a directory of that name under the user-extensions path (`-e`); a mismatch causes a misleading `"the following extensions <category> do not contain a go.mod file"` error.

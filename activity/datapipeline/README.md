# Run Data Pipeline Activity

![Run Data Pipeline Activity](icons/datapipeline.svg)

Runs a pipeline of a [Flogo Data Pipeline](https://github.com/mpandav-tibco/flogo-datapipeline) engine from a flow, through the engine's REST API, and returns its result. The integration layer decides **when** and **on what** a pipeline runs; the data layer moves the data:

- an event or an API call says a partner's export is ready, so the flow processes that file now (`inputPath`) rather than waiting for the next scheduled run;
- an API like *rebuild the report for this date* starts a backfill (`params`);
- a business process runs a pipeline as one of its steps, then continues once it completes (`completed`), or opens a ticket when it fails (`failed`).

The flow sends a **reference** (a path, a date, a partition), never the rows: the pipeline reads them itself. To move every business event into the data layer, publish the events to Kafka and let a streaming pipeline consume the topic; don't start one run per event.

Supports:
- Two modes:
  - `wait` (default) starts the run, follows it, and returns its result.
  - `start` returns once the engine has taken the run (`running`, or `queued`).
- An overlap policy:
  - `allow` (default) runs alongside a run of the same pipeline already in flight.
  - `skip` does not run (status `skipped`).
  - `queue` waits for the run in flight to end, then runs.
- Safe retries: a run ID the engine already has in flight is never run twice; the activity follows that run instead. This makes the activity's retry-on-error safe when you give it a `runId`.
- A callback: the engine posts the run's result to `callbackUrl` as it ends. This suits `start` mode with a flow that receives the result later.
- Trace propagation: with Flogo's OpenTelemetry tracing on, the run request carries the activity's span (`traceparent`), so the engine's run joins the flow's trace and its log lines carry the same `trace.id`.
- Engines without the run-status endpoint: an engine that predates `GET …/run/{runId}` is followed through `GET …/status`.

**Status**: ✅ Active
**Category**: `datapipeline`

---

## How it works

1. `POST {engineUrl}/v1/pipelines/{pipeline}/run?async=true` with `runId`, `params` and `inputPath`, plus `overlap` and `callbackUrl` when set. The request also carries `Authorization: Bearer <apiToken>` and the flow's `traceparent`. The engine answers as soon as it has taken the run.
2. In `wait` mode, `GET {engineUrl}/v1/pipelines/{pipeline}/run/{runId}` every `pollIntervalSeconds` until the run has ended. While the engine is unreachable or answers 5xx (restarting), the activity keeps asking until `waitTimeoutSeconds`.
3. The run's result becomes the activity's outputs. With `failOnRunFailure` (the default), a run that ends `failed` or `cancelled` fails the activity, so the flow's error handler gets it.

| The engine answers the run request with | The activity |
|---|---|
| `202` (running or queued) | follows the run (`wait`) or returns (`start`) |
| `409 run_in_flight` (the `runId` is running) | follows that run: a retried execution never starts a second one |
| `409 busy` (`overlap: skip` while the pipeline runs) or `409 suspended` | returns status `skipped` and `error` says why; not an error |
| `401`, `403`, `404`, `400` | fails: `DATAPIPELINE-4002` |
| `5xx`, unreachable | fails, retriable: `DATAPIPELINE-4003` |

---

## Inputs

| Input | Default | Description |
|---|---|---|
| `engineUrl` | — | Base URL of the engine's REST API, e.g. `http://orders-etl.data:9080`; map it from an app property |
| `apiToken` | — | The engine's `DP_API_TOKEN`. A deployed engine (ops mode) runs pipelines only for callers that have it; map it from a secret app property |
| `pipeline` | — | The pipeline's name (`daily-tx`) or ref (`res://pipeline:daily-tx`) |
| `runId` | a new random ID | The run's ID. Give one so a retry follows the run its first attempt started. A checkpointed run given the ID of a run that stopped resumes where that run stopped |
| `params` | — | Values for the `${…}` in the pipeline's source path, e.g. `{"date": "2026-11-12"}` |
| `inputPath` | — | Replaces the pipeline's source path for this run |
| `mode` | `wait` | `wait` returns the result once the run has ended; `start` returns once the engine has taken the run |
| `overlap` | `allow` | What to do while a run of the pipeline is in flight: `allow`, `skip` or `queue` |
| `callbackUrl` | — | Where the engine posts the result as the run ends (e.g. a flow's REST trigger). It must be under the engine's `DP_API_CALLBACK_URLS` |
| `waitTimeoutSeconds` | `3600` | `wait` mode: how long to wait. A run that outlasts it keeps going, and the activity fails with `DATAPIPELINE-4005` |
| `pollIntervalSeconds` | `2` | `wait` mode: how often to ask how the run is going (it asks at once, then after half a second, so short runs return quickly) |
| `failOnRunFailure` | `true` | A run that ends `failed` or `cancelled` fails the activity; `false` lets the flow branch on `status` instead |

`overlap` and `callbackUrl` are sent only when set, so the activity also runs pipelines on engines that predate them.

## Outputs

| Output | Description |
|---|---|
| `runId` | The run's ID |
| `status` | One of the following: `completed`, `partial` (some rows failed, the others were written), `failed`, `cancelled`; `running` or `queued` in `start` mode; or `skipped` when the engine did not run it |
| `rowsProcessed` | Rows read from the source |
| `rowsWritten` | Rows written to the sinks |
| `rowsFailed` | Rows that failed: dead-lettered or rejected |
| `rowsDropped` | Rows a stage dropped on purpose (a filter) |
| `durationMs` | How long the run took |
| `error` | Why the run failed, was cancelled or was skipped; for a `partial` run, the first reason rows failed |
| `startedAt`, `finishedAt` | RFC 3339 |
| `result` | The run as the engine reports it, every field: `validationReport`, `stageTimings`, `graphStats`, … |

---

## Error codes

Errors reach the flow's error handler with `$error.code`. Those raised after the run started carry the run's outputs in `$error.data`.

| Code | Cause |
|------|-------|
| `DATAPIPELINE-4001` | An input is missing or wrong: no `engineUrl` or `pipeline`, an `engineUrl` that is not an http(s) URL, an unknown `mode` or `overlap` |
| `DATAPIPELINE-4002` | The engine refused the request: the token is missing or wrong (`401`, or `403` in ops mode without a token set on the engine), the pipeline is unknown (`404`), or the request is invalid (`400`, e.g. `callback_not_allowed`) |
| `DATAPIPELINE-4003` | The engine is unreachable or failing (`5xx`). This error is retriable: with the activity's retry on, a retry with the same `runId` follows the run if it did start |
| `DATAPIPELINE-4004` | The run ended `failed` or `cancelled` (`failOnRunFailure`); the message says why |
| `DATAPIPELINE-4005` | The run did not end within `waitTimeoutSeconds`; it keeps going |
| `DATAPIPELINE-4006` | The engine no longer knows the run. It keeps run history in memory, so it probably restarted |

---

## The engine side

- **Token**: a deployed engine (its API reachable from the network) serves runs only with `DP_API_TOKEN` set; send it as `apiToken`.
- **Callbacks**: a deployed engine posts results only under the URLs `DP_API_CALLBACK_URLS` lists, for example `https://flows.internal:9999/hooks`. The receiving flow's REST trigger gets this body:

  ```json
  { "event": "run.finished", "pipeline": "daily-tx", "pipelineRef": "res://pipeline:daily-tx",
    "run": { "runId": "close-2026-11-12", "status": "completed", "rowsWritten": 99850, … } }
  ```
- **Tracing**: set `FLOGO_OTEL_TRACE=true` on both the flow app and the engine, with the same collector (`FLOGO_OTEL_OTLP_ENDPOINT`). The run then appears as a child span of the activity's in one trace. With export off on the engine, the run's log lines still carry the flow's `trace.id`.

See the engine's [REST API](https://github.com/mpandav-tibco/flogo-datapipeline/blob/master/docs/rest-api.md#run-a-pipeline) documentation for runs, overlap, callbacks and run status.

---

## Example

A REST-triggered flow that processes the file named in its request, then replies with the counts:

| Input | Mapping |
|---|---|
| `engineUrl` | `$property["DataPipeline.EngineURL"]` |
| `apiToken` | `$property["DataPipeline.Token"]` |
| `pipeline` | `"partner-orders"` |
| `inputPath` | `$flow.body.file` |
| `runId` | `$flow.body.requestId` (a retry of the same request follows the same run) |
| `overlap` | `"queue"` |

Then map `$activity[RunDataPipeline].status` and `$activity[RunDataPipeline].rowsWritten` into the reply. A run that fails goes to the flow's error handler with `DATAPIPELINE-4004`.

---

## Running Tests

```bash
cd activity/datapipeline
go test ./...            # unit tests against a fake engine API, no network required
go vet ./... && gofmt -l .

# live tests against running engines (their API in full mode, on loopback):
#   DP_PIPELINE runs to its end; DP_SLOW_PIPELINE runs until cancelled (a watched directory)
DP_ENGINE_URL=http://127.0.0.1:9080 DP_PIPELINE=filter-and-map \
DP_SLOW_ENGINE_URL=http://127.0.0.1:9080 DP_SLOW_PIPELINE=slow \
go test -tags integration -run Live -v ./...
```

---

## Build

A separate Go module (`github.com/mpandav-tibco/flogo-custom-extensions/activity/datapipeline`, Go 1.24+, Flogo core only), compiled into a Flogo app through the standard user-extension build, like the other custom extensions in this repository.

> **Build note:** the descriptor's `display.category` (`datapipeline`) must match this directory's basename. The Flogo build resolves the extension through a directory of that name.

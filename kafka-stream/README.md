# Kafka Stream Connector

A **production-ready** Flogo custom extension for stateful stream processing on top of Kafka. It adds enterprise-grade windowed aggregation and message filtering capabilities that work alongside the existing TIBCO Kafka connector (consumer trigger + producer activity), enabling event-driven analytics and routing entirely within Flogo flows.

> **Version 2.0 — Enterprise edition.** This release adds event-time processing, watermarks, late-event DLQ routing, overflow back-pressure, MessageID deduplication, idle-timeout auto-close, keyed cardinality limits, observability snapshots, and multi-predicate AND/OR filter chains. All 100 unit tests pass.

## Overview

The built-in TIBCO Kafka connector handles transport — it consumes individual messages from Kafka topics and invokes flows. The **Kafka Stream Connector** picks up from there: it accumulates those messages into stateful windows, computes rolling aggregates, and filters messages based on field predicates — closing the gap between raw Kafka consumption and stream analytics.

```
Kafka Topic
    │
    ▼
┌──────────────────────┐
│  TIBCO Kafka Trigger │  (consume 1 message → invoke flow)
└──────────┬───────────┘
           │  message: map[string]interface{}
           ▼
┌──────────────────────┐
│  Kafka Stream Filter │  (single predicate OR multi-predicate AND/OR chain)
│  passed=true/false   │  errorMessage=DLQ-routable eval errors
└──────────┬───────────┘
           │  passed: bool / message: filtered payload
           ▼
┌──────────────────────┐
│ Kafka Stream Aggregate│  (event-time, watermarks, overflow, dedup)
│  windowClosed / result│  lateEvent=DLQ-routable late arrivals
└──────────┬───────────┘
           │  windowClosed: bool / result: float64
           ▼
    Downstream Logic
    (alerts, DB writes, API calls, dead-letter queues)
```

## Components

| Component | Description |
|-----------|-------------|
| [Kafka Stream Aggregate](activity/aggregate/README.md) | Accumulates a numeric message field into a named window. Emits sum/count/avg/min/max when the window closes. Enterprise: event-time, watermarks, late-event DLQ, overflow policies, dedup, idle-timeout, cardinality limits, Snapshot(). |
| [Kafka Stream Filter](activity/filter/README.md) | Evaluates a single predicate or a multi-predicate AND/OR chain against a message. Routes via `passed` output. Enterprise: `errorMessage` DLQ output for evaluation failures. |

## Window Types

The aggregate activity supports four window strategies, all backed by an in-process shared registry that preserves state across consecutive Flogo flow invocations.

| Type | Behaviour | Use Case |
|------|-----------|----------|
| `TumblingTime` | Fixed time-bucket — closes after `windowSize` milliseconds, then resets | Periodic reports (e.g. sum every 5 s) |
| `TumblingCount` | Fixed count-batch — closes after exactly `windowSize` events, then resets | Process N events at a time |
| `SlidingTime` | Rolling view of events within the last `windowSize` ms — emits **on every event** | Continuous rolling averages |
| `SlidingCount` | Rolling view of the last `windowSize` events — emits **on every event** | Moving-window statistics |

### Window Behaviour Notes

- **TumblingTime includes the trigger event**: the event whose timestamp crosses the boundary is included in the closing window's aggregate. After close the buffer resets to empty and the window clock restarts from that event's timestamp.
- **Sliding windows always emit**: every `Add()` returns a result; `windowClosed` is always `true`.
- **Keyed windows**: setting `keyField` creates independent sub-windows per unique key value (e.g. one window per `device_id`). Sub-window names are `windowName:keyValue`.

## Enterprise Features

### 1. Event-Time Processing & Watermarks

Set `eventTimeField` to a message field containing the event timestamp (Unix-ms int64/float64 or RFC-3339 string). The window engine advances a **watermark** from the highest timestamp seen, enabling correct ordering even when messages arrive out-of-order.

```json
{ "eventTimeField": "event_ts_ms" }
```

Without this, the system falls back to wall-clock time — suitable for dev/test only.

### 2. Late-Event DLQ Routing

Configure `allowedLateness` (ms) to tolerate late arrivals within a time budget.  Events older than `(watermark - allowedLateness)` are returned with `lateEvent=true` in the output.

```
Flogo branch:  lateEvent=true  → [Route to Dead-Letter Topic]
               lateEvent=false → [Normal Downstream]
```

### 3. Overflow Back-Pressure

Cap the in-memory buffer per window with `maxBufferSize`. Control what happens when the cap is hit via `overflowPolicy`:

| Policy | Behaviour |
|--------|-----------|
| `drop_oldest` (default) | Evict the oldest buffered event — FIFO roll |
| `drop_newest` | Silently discard the incoming event |
| `error` | Return a hard error to the caller |

### 4. MessageID Deduplication

Set `messageIDField` to a message field holding a unique ID (e.g. Kafka record key, UUID). Duplicate events with the same MessageID within the same window are silently ignored — enabling exactly-once aggregation when at-least-once delivery is in use.

### 5. Idle-Timeout Auto-Close

Set `idleTimeoutMs` so keyed sub-windows that receive no event for the specified duration are auto-closed and their partial result emitted. This prevents stale state accumulation in high-cardinality key spaces.

### 6. Keyed Cardinality Limits

Set `maxKeys` to cap the number of distinct keyed sub-windows. Excess keys are rejected with a hard error, preventing memory exhaustion from unbounded key growth.

### 7. Observability — Window Snapshots

Use `kafkastream.ListSnapshots()` in custom instrumentation code to get a `[]WindowSnapshot` across all active windows:

```go
for _, snap := range kafkastream.ListSnapshots() {
    fmt.Printf("window=%s in=%d late=%d dropped=%d closed=%d watermark=%s\n",
        snap.Name, snap.MessagesIn, snap.MessagesLate,
        snap.MessagesDropped, snap.WindowsClosed, snap.Watermark)
}
```

### 8. Multi-Predicate Filter Chains

Configure `predicates` (JSON array) instead of a single `field`/`operator`/`value` to evaluate multiple conditions:

```json
[
  {"field":"price","operator":"gt","value":"100"},
  {"field":"region","operator":"eq","value":"US"},
  {"field":"status","operator":"regex","value":"^(active|pending)$"}
]
```

Set `predicateMode` to `"and"` (all must pass, default) or `"or"` (at least one must pass).

### 9. Filter ErrorMessage DLQ

When predicate evaluation fails (e.g. unexpected field type), `errorMessage` is set in the output. Route `errorMessage != ""` to a DLQ branch independently from normal filter-outs:

```
errorMessage != "" → [Dead-Letter Topic]
passed=true        → [Normal Downstream]  
passed=false       → [Filtered-out branch]
```

## Aggregate Functions

| Function | Description |
|----------|-------------|
| `sum` | Sum of all values in the window |
| `avg` | Arithmetic mean |
| `count` | Number of events in the window |
| `min` | Minimum value |
| `max` | Maximum value |

## Filter Operators

| Operator | Type | Description |
|----------|------|-------------|
| `eq` | Numeric / String | Equal |
| `neq` | Numeric / String | Not equal |
| `gt` | Numeric | Greater than |
| `gte` | Numeric | Greater than or equal |
| `lt` | Numeric | Less than |
| `lte` | Numeric | Less than or equal |
| `contains` | String | Field contains substring |
| `startsWith` | String | Field starts with prefix |
| `endsWith` | String | Field ends with suffix |
| `regex` | String | Field matches regular expression (compiled at init, not per-message) |

Numeric operators (`gt`, `gte`, `lt`, `lte`) attempt float64 parsing of both the field value and the configured value. If either side is non-numeric they fall back to string comparison for `eq`/`neq`.

## Module Structure

```
kafka-stream/                     ← root Go module (window engine + registry)
├── go.mod
├── registry.go                   ← global WindowStore registry (sync.Map, process-scoped)
└── window/
    ├── types.go                  ← WindowConfig, WindowEvent, WindowResult, WindowStore interface
    ├── tumbling.go               ← TumblingTimeWindow, TumblingCountWindow
    ├── sliding.go                ← SlidingTimeWindow, SlidingCountWindow
    └── window_test.go            ← 37 window engine unit tests

activity/
├── aggregate/                    ← Flogo activity module
│   ├── go.mod
│   ├── activity.go
│   ├── metadata.go
│   ├── activity.json
│   └── activity_test.go          ← 22 aggregate unit tests
├── filter/                       ← Flogo activity module
│   ├── go.mod
│   ├── activity.go
│   ├── metadata.go
│   ├── activity.json
│   └── activity_test.go          ← 41 filter unit tests
└── test/integration/             ← live Kafka integration tests
    ├── go.mod
    ├── integration_test.go       ← 7 positive integration tests
    ├── negative_test.go          ← 24 negative integration tests
    └── load_test.go              ← 8 load / memory-leak tests
```

## Getting Started

### Prerequisites

- Go 1.21+
- An existing TIBCO Flogo application with the Kafka connector configured for consuming messages
- Kafka broker accessible from the Flogo runtime

### Add the Activities to Your Flogo Application

Add the aggregate and filter activities to your `go.mod`:

```bash
go get github.com/milindpandav/flogo-extensions/kafka-stream/activity/aggregate
go get github.com/milindpandav/flogo-extensions/kafka-stream/activity/filter
```

### Typical Flow Pattern

```
[Kafka Consumer Trigger]
         │ $.content → message
         ▼
[Kafka Stream Filter]
  field:    "temperature"
  operator: "gt"
  value:    "25"
         │ passed=true → continue
         │ passed=false → Stop flow
         ▼
[Kafka Stream Aggregate]
  windowName: "iot-temp-5min"
  windowType: "TumblingTime"
  windowSize: 300000          ← 5 minutes in ms
  function:   "avg"
  valueField: "temperature"
  keyField:   "device_id"     ← independent window per device
         │ windowClosed=true → send alert / write to DB
         │ windowClosed=false → Stop flow
         ▼
[Next Activity — e.g. REST call, DB write]
```

### Use with Flogo Designer

Both activities are registered with descriptors (`activity.json`) and can be imported into the TIBCO Flogo Visual Designer. Reference the `ref` field from each `activity.json`:

- Aggregate: `github.com/milindpandav/flogo-extensions/kafka-stream/activity/aggregate`
- Filter: `github.com/milindpandav/flogo-extensions/kafka-stream/activity/filter`

## Use Cases

### 📊 IoT & Sensor Analytics
- Compute 5-minute average temperature per device across thousands of concurrent sensors
- Alert when rolling average crosses a threshold
- Count fault events per device in a tumbling window

### 📈 Financial & Trading
- Rolling sum of transaction amounts per account within a sliding time window
- Filter transactions above a threshold before aggregating for fraud detection
- Per-symbol price averages over fixed count windows

### 🏭 Manufacturing & Operations
- Detect production anomalies by monitoring rolling min/max of sensor readings
- Count defect events per production line in time-based tumbling windows
- Filter and aggregate log events by severity and service

### 🔔 Event-Driven Alerting
- Gate noisy Kafka streams — only messages matching a predicate advance in the flow
- Combine filter + aggregate to fire alerts only when aggregated values exceed limits

## Running Tests

**100 unit tests** across 3 packages + 39 integration tests (7 positive + 24 negative + 8 load/memory).

### Unit Tests

```bash
# Window engine (28 tests — includes enterprise: overflow, dedup, late-event, idle-timeout, snapshot)
cd kafka-stream
go test ./window/... -v

# Aggregate activity (18 tests — includes enterprise: event-time, DLQ routing, dedup, overflow validation)
cd kafka-stream/activity/aggregate
go test ./... -v

# Filter activity (30 tests — includes enterprise: multi-predicate AND/OR, regex chains, errorMessage DLQ)
cd kafka-stream/activity/filter
go test ./... -v
```

### Integration Tests (requires live Kafka)

```bash
# Prerequisites: Kafka on localhost:9092
# Topic for functional tests (1 partition):
# kafka-topics.sh --bootstrap-server localhost:9092 --create \
#   --topic kafka-stream-int-test --partitions 1 --replication-factor 1
#
# Topic for load tests (3 partitions):
# kafka-topics.sh --bootstrap-server localhost:9092 --create \
#   --topic kafka-stream-load-test --partitions 3 --replication-factor 1

cd kafka-stream/activity/test/integration

# Run all integration tests (functional + negative + load):
go test ./... -v -timeout 300s

# Run only load/memory tests:
go test ./... -v -run TestLoad -timeout 300s
```

The integration tests produce real messages via the IBM Sarama client, consume them back, and run them through the activity chain — validating the full produce → consume → filter → aggregate pipeline against Kafka 3.8.0.

**Positive tests** (`integration_test.go`)

| Test | What it validates |
|------|-------------------|
| `TestIntegration_AggregateSum_TumblingCount` | 5 messages → window closes with sum=150 |
| `TestIntegration_AggregateAvg_TumblingCount` | 3 messages → window closes with avg=200 |
| `TestIntegration_FilterThenAggregate` | 10 messages, 5 pass filter, window closes with sum=160 |
| `TestIntegration_KeyedAggregation` | 3 devices × 3 messages, independent window per device |
| `TestIntegration_TwoConsecutiveWindows` | Window resets and produces correct result for second batch |
| `TestIntegration_FilterOperators_LiveMessages` | All 10 filter operators validated against a live message |
| `TestIntegration_SlidingCount_LiveMessages` | Rolling sums [5, 15, 30, 45, 60] verified across 5 events |

**Negative tests** (`negative_test.go`)

| Test | What it validates |
|------|-------------------|
| `TestNeg_UnreachableBroker_ProducerFails` | Producer to dead broker returns connection error |
| `TestNeg_UnreachableBroker_ConsumerFails` | Consumer to dead broker returns connection error |
| `TestNeg_MalformedJSON_FromKafka` | Non-JSON bytes on topic fail to unmarshal |
| `TestNeg_Aggregate_MissingValueField_LiveMessage` | Absent `valueField` → `Eval` returns error |
| `TestNeg_Aggregate_NonNumericField_LiveMessage` | String in numeric field → coercion error |
| `TestNeg_Filter_MessageRejected_LiveRoundtrip` | Rejected message → `passed=false`, `Message=nil` |
| `TestNeg_Filter_AllRejected_LiveBatch` | All messages in a live batch rejected |
| `TestNeg_Filter_NonNumericFieldValue_LiveMessage` | Non-numeric field with numeric operator → `errorMessage` set |
| `TestNeg_Filter_MissingField_DefaultFail_LiveMessage` | Missing field with `passThroughOnMissing=false` → rejected |
| `TestNeg_Filter_MissingField_PassThrough_LiveMessage` | Missing field with `passThroughOnMissing=true` → passes |
| `TestNeg_Aggregate_InvalidWindowType_ConstructionError` | Bad `windowType` caught at `New()` |
| `TestNeg_Aggregate_InvalidFunction_ConstructionError` | Bad `function` caught at `New()` |
| `TestNeg_Aggregate_ZeroWindowSize_ConstructionError` | Zero `windowSize` caught at `New()` |
| `TestNeg_Filter_InvalidOperator_ConstructionError` | Bad `operator` caught at `New()` |
| `TestNeg_Filter_InvalidRegex_ConstructionError` | Malformed regex caught at `New()` |
| `TestNeg_Aggregate_BufferOverflow_Error_LiveMessages` | `overflow=error` + `maxBufferSize=2` → error on 3rd message |
| `TestNeg_Aggregate_MaxKeys_Exceeded_LiveMessages` | `maxKeys` cap → error when exceeded |
| `TestNeg_Aggregate_DeduplicateMessages_LiveKafka` | Duplicate `msgid` ignored; deduplicated sum=60 |
| `TestNeg_Filter_MultiPredicateAND_OneFails_LiveKafka` | AND chain — one predicate fails → rejected |
| `TestNeg_Filter_MultiPredicateOR_AllFail_LiveKafka` | OR chain — all predicates fail → rejected |
| `TestNeg_Aggregate_LateEvent_FlaggedLate_LiveKafka` | Event behind watermark → `lateEvent=true` |
| `TestNeg_Aggregate_WindowNotClosed_PartialBatch_LiveKafka` | Partial batch (4 of 10) → window stays open |
| `TestNeg_Filter_InvalidRegexInPredicatesJSON_ConstructionError` | Bad regex in `predicatesJSON` caught at `New()` |
| `TestNeg_Aggregate_NegativeWindowSize_ConstructionError` | Negative `windowSize` caught at `New()` |

**Load / memory-leak tests** (`load_test.go`)

All 8 tests run against live Kafka (`kafka-stream-load-test`, 3 partitions). Each Kafka message is **≈1 KB** (realistic JSON with a `pad` field). Memory is sampled with `runtime.GC()` + `runtime.ReadMemStats` and goroutine counts with `runtime.NumGoroutine()` before and after each phase.

| Test | Volume | What it validates |
|------|--------|-------------------|
| `TestLoad_HighThroughput_ProduceConsume` | 50 000 × 1 KB msgs (live Kafka) | Throughput ≥9 000 msg/s · heap +0.4 MB |
| `TestLoad_WindowRegistry_NoLeak` | 1 000 windows | Registry 0 → 0 after bulk create+eval+unregister |
| `TestLoad_KeyedWindows_NoRegistryLeak` | 500 keys × 5 events | All 500 close sum=15 · goroutine Δ=0 |
| `TestLoad_DedupMap_NoLeakAcrossResets` | 200 cycles × 50 events | Dedup map cleared on reset · heap Δ=0.0 MB |
| `TestLoad_SlidingCountWindow_BoundedBuffer` | 50 000 events | Buffer never exceeds `windowSize=100` · heap Δ=0.0 MB |
| `TestLoad_Goroutine_NoLeak_ProducerConsumer` | 50 pairs | All Sarama goroutines released · goroutine Δ=0 |
| `TestLoad_FilterAggregate_Pipeline_LiveKafka` | 50 000 × 1 KB msgs (live Kafka) | 25 000 pass / 25 000 fail · 250 window closes · 120 000 msg/s · heap +0.4 MB |
| `TestLoad_HeapProfile_TumblingCount` | 50 000 events (10 samples) | Heap flat at 1.8 MB (spread=0.0 MB) across all 10 samples |

### Performance Characteristics

Results measured on a single MacBook Pro (Apple M-series) against a local Kafka 3.8.0 KRaft broker. All Kafka messages are **≈1 KB** JSON payloads.

| Metric | Observed | Notes |
|--------|----------|-------|
| Kafka produce throughput | **~9 000 msg/s** | 50 000 × 1 KB, SyncProducer, `WaitForAll` acks |
| Filter → aggregate pipeline | **~120 000 msg/s** | In-process, after Kafka consume; 50k messages |
| SlidingCount window throughput | **~290 000 evt/s** | 50 000 events, no Kafka I/O |
| Heap growth (50k × 1 KB Kafka I/O) | **+0.4 MB** | After forced GC |
| Heap growth (full pipeline, 50k msgs) | **+0.4 MB** | After forced GC |
| Heap growth (1 000 window create+close) | **+0.1 MB** | After forced GC + cleanup |
| Heap spread across 50k events (TumblingCount) | **0.0 MB** | Flat at 1.8 MB across 10 samples |
| Goroutine leak (50 producer+consumer pairs) | **Δ = 0** | All Sarama goroutines released on `Close()` |
| Registry leak (500 keyed sub-windows) | **0 entries** | Registry returns to baseline after cleanup |
| Dedup map growth across 200 reset cycles | **0 MB** | Map cleared on window reset |

> **Key takeaway:** processing state (window buffers, dedup maps, registry) is fully reclaimed by the Go GC after each window close and cleanup. There are no goroutine or memory leaks under sustained load at realistic payload sizes.

## Relationship to the TIBCO Kafka Connector

| Aspect | TIBCO Kafka Connector | Kafka Stream Connector |
|--------|-----------------------|------------------------|
| Role | Transport layer | Processing layer |
| State | Stateless (one message → one flow) | Stateful (accumulates across flows) |
| Scope | Connection, auth (TLS/SASL), offsets | Windowed aggregation, field filtering |
| Dependency | Independent | Sits downstream of the Kafka trigger |
| Auth/TLS | Full SASL/SSL support | Uses TIBCO connector for auth |

The Kafka Stream Connector is designed to complement — not replace — the TIBCO Kafka connector. Configure the TIBCO connector for your broker connection and topic subscription; use these stream activities to add analytics on top.

## Dependencies

| Dependency | Version | Purpose |
|------------|---------|---------|
| `github.com/project-flogo/core` | v1.6.13 | Flogo activity API |
| `github.com/IBM/sarama` | v1.46.3 | Kafka client (integration tests only) |
| `github.com/stretchr/testify` | v1.10.0 | Test assertions |

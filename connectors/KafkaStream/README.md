# Kafka Stream Connector

A Flogo custom extension that adds stateful windowed aggregation and message filtering to Kafka-based flows.

The built-in TIBCO Kafka connector handles transport — it consumes messages from topics and invokes flows. This extension picks up from there: it accumulates those messages into in-memory windows, computes rolling aggregates, and filters messages based on field predicates.

```
Kafka Topic
    │
    ▼
┌──────────────────────┐
│  TIBCO Kafka Trigger │  (one message → one flow invocation)
└──────────┬───────────┘
           │  message: map[string]interface{}
           ▼
┌──────────────────────┐
│  Kafka Stream Filter │  (single or multi-predicate evaluation)
│  passed=true/false   │
└──────────┬───────────┘
           │  passed: bool / message: filtered payload
           ▼
┌──────────────────────┐
│ Kafka Stream Aggregate│  (windowed accumulation)
│  windowClosed / result│
└──────────┬───────────┘
           │  windowClosed: bool / result: float64
           ▼
    Downstream Logic
    (alerts, DB writes, API calls, dead-letter queues)
```

## Components

| Component | Description |
|-----------|-------------|
| [Kafka Stream Filter](activity/filter/README.md) | Evaluates a single predicate or a multi-predicate AND/OR chain against a message. Routes via `passed` output. |
| [Kafka Stream Aggregate](activity/aggregate/README.md) | Accumulates a numeric message field into a named window. Emits sum/count/avg/min/max when the window closes. |

## Window Types

All window types are backed by an in-process shared registry that preserves state across consecutive Flogo flow invocations.

| Type | Behaviour | Use Case |
|------|-----------|----------|
| `TumblingTime` | Fixed time-bucket — closes after `windowSize` ms, then resets | Periodic reports (e.g. sum every 5 s) |
| `TumblingCount` | Fixed count-batch — closes after exactly `windowSize` events, then resets | Process N events at a time |
| `SlidingTime` | Rolling view of events within the last `windowSize` ms — emits on every event | Continuous rolling averages |
| `SlidingCount` | Rolling view of the last `windowSize` events — emits on every event | Moving-window statistics |

**Window behaviour notes:**

- **TumblingTime includes the trigger event**: the event whose timestamp crosses the boundary is included in the closing window's aggregate. After close, the buffer resets and the clock restarts from that event's timestamp.
- **Sliding windows always emit**: every event produces a result; `windowClosed` is always `true`.
- **Keyed windows**: setting `keyField` creates independent sub-windows per unique key value (e.g. one window per `device_id`).

## Features

### Event-Time Processing and Watermarks

Set `eventTimeField` to a message field containing the event timestamp (Unix-ms int64/float64 or RFC-3339 string). The window engine advances a watermark from the highest timestamp seen, enabling correct ordering when messages arrive out-of-order. Falls back to wall-clock when not set.

### Late-Event Handling

Configure `allowedLateness` (ms) to tolerate late arrivals within a time budget. Events older than `watermark − allowedLateness` are returned with `lateEvent=true` so you can route them to a dead-letter topic.

```
lateEvent=true  → [Route to Dead-Letter Topic]
lateEvent=false → [Normal Downstream]
```

### Overflow Control

Cap the in-memory buffer per window with `maxBufferSize`. Control what happens when the cap is hit via `overflowPolicy`:

| Policy | Behaviour |
|--------|-----------|
| `drop_oldest` (default) | Evict the oldest buffered event |
| `drop_newest` | Discard the incoming event |
| `error` | Return a hard error to the caller |

### MessageID Deduplication

Set `messageIDField` to a message field holding a unique ID. Duplicate events within the same window are silently ignored, enabling correct aggregation when at-least-once delivery is in use.

### Idle-Timeout Auto-Close

Set `idleTimeoutMs` so keyed sub-windows that receive no event for the specified duration are auto-closed and their partial result emitted. Prevents stale state accumulation in high-cardinality key spaces.

### Keyed Cardinality Limit

Set `maxKeys` to cap the number of distinct keyed sub-windows. Keys beyond the cap are rejected with a hard error.

### Multi-Predicate Filter Chains

Configure `predicates` (JSON array) instead of a single `field`/`operator`/`value` to evaluate multiple conditions:

```json
[
  {"field":"price","operator":"gt","value":"100"},
  {"field":"region","operator":"eq","value":"US"}
]
```

Set `predicateMode` to `"and"` (all must pass, default) or `"or"` (at least one must pass).

## Aggregate Functions

| Function | Description |
|----------|-------------|
| `sum` | Sum of all values in the window |
| `avg` | Arithmetic mean |
| `count` | Number of events |
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
| `regex` | String | Field matches regular expression |

## Module Structure

All components live in a single Go module (`github.com/milindpandav/flogo-extensions/kafkastream`). The `activity/aggregate` and `activity/filter` packages are sub-packages of that module, not separate modules.

```
connectors/KafkaStream/           ← single Go module root
├── go.mod
├── registry.go                   ← global WindowStore registry (process-scoped)
├── window/
│   ├── types.go
│   ├── tumbling.go
│   ├── sliding.go
│   └── window_test.go
└── activity/
    ├── aggregate/                ← sub-package: kafkastream/activity/aggregate
    │   ├── activity.go
    │   ├── metadata.go
    │   ├── activity.json
    │   └── activity_test.go
    ├── filter/                   ← sub-package: kafkastream/activity/filter
    │   ├── activity.go
    │   ├── metadata.go
    │   ├── activity.json
    │   └── activity_test.go
    └── test/integration/         ← separate module for integration tests only
        └── go.mod
```

## Getting Started

### Prerequisites

- Go 1.21+
- A TIBCO Flogo application with the Kafka connector configured for consuming messages
- Kafka broker accessible from the Flogo runtime

### Add the Activities to Your Flogo Application

Add the module as an extension in your TIBCO Flogo application. In the VS Code extension, open the **Extensions** panel, select **Add Custom Extension**, and point it at the `connectors/KafkaStream` folder (or the published module path `github.com/milindpandav/flogo-extensions/kafkastream`).

For Flogo CLI-based projects, add the module to the `extensions` array in your `.flogo` descriptor:

```json
{
  "extensions": [
    {
      "ref": "github.com/milindpandav/flogo-extensions/kafkastream"
    }
  ]
}
```

Once imported, the **Kafka Stream Filter** and **Kafka Stream Aggregate** activities appear in the activity palette under the **KafkaStream** category.

### Typical Flow Pattern

```
[Kafka Consumer Trigger]
         │ $.content → message
         ▼
[Kafka Stream Filter]
  field:    "temperature"
  operator: "gt"
  value:    "25"
         │ passed=true  → continue
         │ passed=false → Stop flow
         ▼
[Kafka Stream Aggregate]
  windowName: "iot-temp-5min"
  windowType: "TumblingTime"
  windowSize: 300000
  function:   "avg"
  valueField: "temperature"
  keyField:   "device_id"
         │ windowClosed=true  → alert / write to DB
         │ windowClosed=false → Stop flow
         ▼
[Next Activity — REST call, DB write, etc.]
```

## Use Cases

**IoT and sensor analytics** — average temperature per device over tumbling time windows; alert when a rolling average crosses a threshold; count fault events per device.

**Financial processing** — rolling sum of transaction amounts per account; filter high-value transactions before aggregating; per-symbol price averages over fixed count windows.

**Operations and monitoring** — detect anomalies by monitoring rolling min/max; count error events per service in time-based windows; gate noisy event streams by field value before aggregating.

## Running Tests

```bash
# Window engine
cd connectors/KafkaStream
go test ./window/... -v

# Aggregate activity
cd connectors/KafkaStream/activity/aggregate
go test ./... -v

# Filter activity
cd connectors/KafkaStream/activity/filter
go test ./... -v
```

## Current Limitations

- **Single-process state only.** Window state lives in the memory of the running Flogo process. There is no distributed state coordination. If the same Kafka consumer group runs as multiple Flogo instances, each instance maintains its own independent window registry — aggregates are not merged across instances.

- **State is lost on restart without persistence.** Without `persistPath` configured on the Aggregate activity, all in-flight window state is discarded when the process stops. Configure `persistPath` and `persistEveryN` to enable gob-based snapshots.

- **Persistence is best-effort.** The gob snapshot is written synchronously every N messages but is not fsync'd. A hard crash between writes may lose the last N events from the snapshot.

- **No Kafka broker timestamp in the trigger.** The TIBCO Kafka consumer trigger does not expose Kafka's built-in record timestamp as an output. To use event-time processing, set the `eventTimestamp` input on the Aggregate activity and map the timestamp from a message payload field, a Kafka header (`$trigger.headers.<name>`), or any computed `int64` expression. See the [Aggregate README](activity/aggregate/README.md#event-time-sources) for details.

- **No backpressure to the Kafka consumer.** The Flogo trigger pulls messages at whatever rate Kafka delivers them. There is no mechanism to pause consumption when a window buffer is under pressure — use `overflowPolicy` to control what happens when `maxBufferSize` is reached.

- **No rebalance-aware state handoff.** When a Kafka consumer group rebalances, in-flight window state for reassigned partitions stays with the original process. The new consumer starts fresh for those messages.

## Why Event-Driven Instead of Stateful Streaming

Flogo's core execution model is: **one Kafka message = one flow invocation**. Each invocation is a short, synchronous call from Flogo's perspective. This is fundamental to how the visual designer, mapper, and activity contracts work.

Stateful streaming platforms such as Kafka Streams and Apache Flink work differently: they treat the Kafka consumer loop itself as the execution unit and manage state internally across that loop. Embedding that model inside a Flogo activity would require running a second event loop inside `Eval()`, bypassing the Flogo trigger entirely, and making the flow non-debuggable in the designer.

The event-driven approach taken here stays within Flogo's model:

- Each `Eval()` call is synchronous and returns immediately — consistent with every other Flogo activity.
- The in-process window registry provides statefulness across invocations without any external infrastructure.
- Flows remain fully debuggable and testable in the Flogo Visual Designer and in standard Go unit tests.
- Deployment is a single binary — no JVM, no additional broker, no state backend to operate.

The trade-off is that window state is scoped to a single process. This is the right choice for single-instance deployments, edge computing, and moderate-throughput analytics where operational simplicity matters. For workloads that require horizontal scaling of window state across multiple consumer instances, a dedicated streaming platform (Kafka Streams, Flink) is the better fit.

## Relationship to the TIBCO Kafka Connector

| Aspect | TIBCO Kafka Connector | Kafka Stream Connector |
|--------|-----------------------|------------------------|
| Role | Transport | Processing |
| State | Stateless | Stateful (in-process) |
| Scope | Connection, auth, offsets | Windowed aggregation, filtering |
| Auth/TLS | Full SASL/SSL support | Delegated to TIBCO connector |

Configure the TIBCO connector for broker connection and topic subscription; use these stream activities to add analytics on top.

## Dependencies

| Dependency | Version | Purpose |
|------------|---------|---------|
| `github.com/project-flogo/core` | v1.6.13 | Flogo activity API |
| `github.com/stretchr/testify` | v1.10.0 | Test assertions |

# Kafka Stream Aggregate Activity

A Flogo activity that accumulates a numeric field from Kafka messages into a named streaming window and emits an aggregated result (sum / count / avg / min / max) when the window closes. State is preserved across consecutive Flogo flow invocations via a shared in-process registry, enabling stateful stream analytics without an external state store.

**v2.0.0** — Enterprise features: event-time processing, late-event DLQ routing, overflow back-pressure, MessageID deduplication, idle-timeout auto-close, keyed cardinality limits.

## Overview

The standard TIBCO Kafka consumer trigger fires once per message. The Aggregate activity bridges the gap between that per-message invocation and window-level computation: each call to `Eval` pushes one event into a named window; when the window boundary is crossed (by time or event count) the activity emits the aggregate and resets.

## Configuration

### Settings

| Setting | Type | Required | Default | Description |
|---------|------|----------|---------|-------------|
| `windowName` | string | Yes | — | Unique name for this window instance. The same name is shared across all flow executions to maintain state (e.g. `sensor-5s-avg`). |
| `windowType` | string | Yes | `TumblingTime` | Window strategy. One of `TumblingTime`, `TumblingCount`, `SlidingTime`, `SlidingCount`. |
| `windowSize` | integer | Yes | — | Window size: **milliseconds** for time-based windows, **event count** for count-based windows. Must be > 0. |
| `function` | string | Yes | `sum` | Aggregation function: `sum`, `avg`, `count`, `min`, `max`. |
| `valueField` | string | Yes | — | Name of the numeric field in the message to aggregate (e.g. `temperature`, `price`, `latency`). |
| `keyField` | string | No | — | If set, the window is keyed by the value of this field, creating independent sub-windows per unique key (e.g. `device_id`, `symbol`). |
| `eventTimeField` | string | No | — | Message field containing the event timestamp (Unix-ms int64/float64 or RFC-3339 string). When set, enables event-time processing and watermarks. Falls back to wall-clock if absent. |
| `allowedLateness` | integer | No | `0` | Milliseconds of allowed lateness past the watermark. Late events within this budget are accepted; events older than `(watermark - allowedLateness)` are routed to DLQ via `lateEvent=true`. |
| `maxBufferSize` | integer | No | `0` | Maximum events to buffer per window. `0` = unlimited. Overflow behaviour is controlled by `overflowPolicy`. |
| `overflowPolicy` | string | No | `drop_oldest` | What to do when `maxBufferSize` is exceeded: `drop_oldest`, `drop_newest`, or `error`. |
| `idleTimeoutMs` | integer | No | `0` | Auto-close and emit a partial result when a keyed sub-window receives no event for this many milliseconds. `0` = disabled. |
| `maxKeys` | integer | No | `0` | Cap on distinct keyed sub-windows. `0` = unlimited. Excess keys produce a hard error. |
| `messageIDField` | string | No | — | Message field used as an idempotency key. Duplicate MessageIDs within the same window are silently ignored. |

### Inputs

| Input | Type | Required | Description |
|-------|------|----------|-------------|
| `message` | object | Yes | The Kafka message payload as a `map[string]interface{}`. Typically mapped from the Kafka trigger's `$.content` output after JSON parsing. |

### Outputs

| Output | Type | Description |
|--------|------|-------------|
| `windowClosed` | boolean | `true` when the window boundary was crossed and an aggregate was computed; `false` when the window is still accumulating. |
| `result` | number | The aggregated value (sum / avg / min / max / count). Only meaningful when `windowClosed` is `true`. |
| `count` | integer | Number of events that were in the closed window. |
| `windowName` | string | Name of the window that closed (useful in branching logic). |
| `key` | string | Key value of the window that closed. Empty when `keyField` is not configured. |
| `lateEvent` | boolean | `true` when the event's timestamp is older than `(watermark - allowedLateness)`. Route `lateEvent=true` to a dead-letter topic. |
| `lateReason` | string | Human-readable explanation when `lateEvent=true` (e.g. `"event time 2024-01-01T00:00:00Z is before watermark 2024-01-01T00:01:00Z with tolerance 0ms"`). |
| `droppedCount` | integer | Number of events that were dropped from the window due to overflow since the last window close. |

## Window Types

### TumblingTime

Fixed-size time buckets. The window opens on the first event after a reset, accumulates all events, and closes when an event is received whose timestamp is ≥ `windowStart + windowSize` ms. The closing event is **included** in the aggregate. After close the buffer is cleared and the clock restarts from the closing event's timestamp.

```
Events: ──E1──E2──E3──────────────E4──E5──...
          |← 5 s window →|close!
                          result emitted
```

**`windowSize`**: milliseconds (e.g. `5000` = 5 seconds, `300000` = 5 minutes)

### TumblingCount

Fixed event count. Accumulates exactly `windowSize` events, then closes and resets.

```
Events: ──E1──E2──E3──  close! ──E4──E5──E6── close! ...
          |← count=3 →|                |← count=3 →|
```

**`windowSize`**: number of events

### SlidingTime

Rolling view of all events within the last `windowSize` milliseconds. Emits a result **on every event**. Old events are evicted when they fall outside the window.

**`windowSize`**: milliseconds

### SlidingCount

Rolling view of the last `windowSize` events. Emits a result **on every event**. Oldest events are dropped when the buffer exceeds the configured size.

**`windowSize`**: number of events to retain

## Keyed Windows

When `keyField` is set, the activity extracts the value of that field from each incoming message and routes the event into a separate sub-window identified by `windowName:keyValue`. Each key's window state is fully independent.

**Example**: `windowName=iot-avg`, `keyField=device_id`

- Message `{"device_id":"sensor-A","temp":22}` → feeds window `iot-avg:sensor-A`
- Message `{"device_id":"sensor-B","temp":30}` → feeds window `iot-avg:sensor-B`

Both windows close independently. The `key` output field carries the key value when the window closes.

## Enterprise Usage Examples

### Example 1 — Event-time with late-event DLQ routing

```json
{
  "settings": {
    "windowName": "iot-temp-5min",
    "windowType": "TumblingTime",
    "windowSize": 300000,
    "function": "avg",
    "valueField": "temperature",
    "keyField": "device_id",
    "eventTimeField": "event_ts_ms",
    "allowedLateness": 30000
  }
}
```

**Flow branching after this activity**:

```
lateEvent=true  → [Produce to dead-letter Kafka topic]
lateEvent=false and windowClosed=true  → [Downstream alert]
lateEvent=false and windowClosed=false → [Stop / wait next message]
```

### Example 2 — Overflow back-pressure + MessageID dedup

```json
{
  "settings": {
    "windowName": "txn-sum-100",
    "windowType": "TumblingCount",
    "windowSize": 100,
    "function": "sum",
    "valueField": "amount",
    "maxBufferSize": 120,
    "overflowPolicy": "drop_oldest",
    "messageIDField": "txn_id"
  }
}
```

Duplicate `txn_id` values within the same window are silently ignored — enabling exactly-once aggregation when the Kafka consumer delivers at-least-once.

### Example 3 — Idle-timeout + cardinality guard

```json
{
  "settings": {
    "windowName": "per-device-sum",
    "windowType": "TumblingTime",
    "windowSize": 60000,
    "function": "sum",
    "valueField": "bytes",
    "keyField": "device_id",
    "idleTimeoutMs": 120000,
    "maxKeys": 10000
  }
}
```

Sub-windows that receive no event for 2 minutes are auto-closed. Key count is capped at 10,000 — excess devices produce a hard error, protecting the process from unbounded memory growth.

### Example 4 — 5-minute average temperature per device (basic)

```json
{
  "settings": {
    "windowName": "iot-temp-5min",
    "windowType": "TumblingTime",
    "windowSize": 300000,
    "function": "avg",
    "valueField": "temperature",
    "keyField": "device_id"
  }
}
```

### Example 5 — Rolling 10-event average latency (SlidingCount)

```json
{
  "settings": {
    "windowName": "api-latency-p10",
    "windowType": "SlidingCount",
    "windowSize": 10,
    "function": "avg",
    "valueField": "latency_ms"
  }
}
```

With sliding windows `windowClosed` is always `true` — every message produces a result.

## Observability

Use `kafkastream.ListSnapshots()` to inspect all active windows at runtime:

```go
import kafkastream "github.com/milindpandav/flogo-extensions/kafka-stream"

for _, snap := range kafkastream.ListSnapshots() {
    fmt.Printf("window=%s in=%d late=%d dropped=%d closed=%d watermark=%s\n",
        snap.Name, snap.MessagesIn, snap.MessagesLate,
        snap.MessagesDropped, snap.WindowsClosed, snap.Watermark)
}
```

Use `kafkastream.SweepIdle()` to manually trigger idle-timeout eviction:

```go
for _, result := range kafkastream.SweepIdle() {
    fmt.Printf("idle window closed: name=%s result=%v count=%d\n",
        result.WindowName, result.Value, result.Count)
}
```

## Error Handling

| Error condition | Behaviour |
|-----------------|-----------|
| `valueField` not present in message | `Eval` returns an error; the flow fails with a descriptive message |
| `valueField` present but non-numeric | `Eval` returns an error; coercion is attempted via `strconv.ParseFloat` |
| Invalid `windowType` at init | `New` returns an error; the flow fails to start |
| Invalid `function` at init | `New` returns an error |
| `windowSize ≤ 0` at init | `New` returns an error |
| Invalid `overflowPolicy` at init | `New` returns an error |
| Event older than watermark − allowedLateness | `lateEvent=true` in output; no error returned — route via flow branching |
| `overflowPolicy=error` and buffer full | `Eval` returns a `WindowError`; the flow fails |
| `maxKeys` exceeded | `Eval` returns an error; the flow fails |

## State & Concurrency

Windows are stored in a **process-scoped sync.Map registry**. All concurrent flow goroutines sharing the same `windowName` see the same window. Individual window operations are mutex-protected — safe for high-throughput parallel consumption.

Window state is **in-memory only** and does not survive a process restart. For persistent windowing, store the `result` output in an external system when `windowClosed` is `true`.

## Go Module

```
github.com/milindpandav/flogo-extensions/kafka-stream/activity/aggregate
```

Requires the root shared module:

```
github.com/milindpandav/flogo-extensions/kafka-stream
```

# Kafka Stream Aggregate Activity

A Flogo activity that accumulates a numeric field from incoming Kafka messages into a named streaming window and emits an aggregated result (`sum` / `count` / `avg` / `min` / `max`) when the window crosses its boundary.

State is held in a process-scoped in-memory registry that persists across individual Flogo flow invocations (each Kafka message triggers one invocation), so the window accumulates correctly without an external database or cache. An optional gob-based persistence layer can snapshot and restore state across full process restarts.

## Settings

Configured once per activity instance at design time.

| Setting | Type | Required | Default | Description |
|---------|------|----------|---------|-------------|
| `windowName` | string | Yes | — | Unique name for this window. Shared across all flow invocations for the lifetime of the process. |
| `windowType` | string | Yes | `TumblingTime` | Window strategy: `TumblingTime`, `TumblingCount`, `SlidingTime`, `SlidingCount`. |
| `windowSize` | integer | Yes | — | Milliseconds for time-based windows; event count for count-based windows. Must be > 0. |
| `function` | string | Yes | `sum` | Aggregation function: `sum`, `avg`, `count`, `min`, `max`. |
| `eventTimeField` | string | No | — | Message field containing the event timestamp (Unix-ms int64/float64 or RFC-3339 string). Enables event-time processing and watermarks. Falls back to wall-clock when absent. |
| `allowedLateness` | integer | No | `0` | Milliseconds of tolerance past the watermark. Events older than `watermark − allowedLateness` are marked late and not added to the window. |
| `maxBufferSize` | integer | No | `0` | Maximum events buffered per window instance. `0` = unlimited. |
| `overflowPolicy` | string | No | `drop_oldest` | Action when buffer is full: `drop_oldest`, `drop_newest`, or `error`. |
| `idleTimeoutMs` | integer | No | `0` | Auto-close a keyed sub-window with no new events for this duration (ms). `0` = disabled. |
| `maxKeys` | integer | No | `0` | Cap on live keyed sub-windows. Excess keys produce a hard error. `0` = unlimited. |
| `persistPath` | string | No | — | File path for gob-encoded state snapshots. On startup the file is restored if it exists. Empty = disabled. |
| `persistEveryN` | integer | No | `0` | Write a snapshot after every N messages. `0` = no periodic writes. |

## Inputs

| Input | Type | Required | Description |
|-------|------|----------|-------------|
| `message` | object | Yes | Kafka message payload (`map[string]interface{}`). |
| `valueField` | string | Yes | Message field to aggregate (e.g. `temperature`, `amount`). |
| `keyField` | string | No | Message field to partition into independent sub-windows (e.g. `device_id`). |
| `eventTimeField` | string | No | Message field holding the event timestamp (Unix-ms int64/float64 or RFC-3339 string). Overrides the `eventTimeField` setting at runtime. Not used when `eventTimestamp` is set. |
| `eventTimestamp` | integer | No | Pre-resolved Unix-ms timestamp (int64). When non-zero, takes priority over `eventTimeField`. Map from any source — Kafka headers, a flow variable, or a computed expression (see [Event-Time Sources](#event-time-sources)). |
| `messageIDField` | string | No | Message field used as an idempotency key. Duplicates within the same window are silently dropped. |
| `persistPath` | string | No | Overrides the `persistPath` setting at runtime. |

## Outputs

| Output | Type | Description |
|--------|------|-------------|
| `windowClosed` | boolean | `true` when the window crossed its boundary and a result is ready. |
| `result` | number | Aggregated value. Meaningful only when `windowClosed=true`. |
| `count` | integer | Number of events in the closed window. |
| `windowName` | string | Name of the window that closed. |
| `key` | string | Key value of the sub-window that closed. Empty when `keyField` is not set. |
| `lateEvent` | boolean | `true` when the event is older than `watermark − allowedLateness`. Route to dead-letter topic. |
| `lateReason` | string | Explanation for `lateEvent=true`. |
| `droppedCount` | integer | Events dropped due to overflow since the last window close. |

## Event-Time Sources

Event time is resolved in this priority order:

1. **`eventTimestamp` input** — a pre-resolved Unix-ms `int64`. Use this to accept event time from any source outside the message payload.
2. **`eventTimeField` input** — a field name in the current message. Overrides the setting value for that invocation.
3. **`eventTimeField` setting** — the default field name applied to every invocation.
4. **Wall-clock time** — used when none of the above are set or the field is absent.

### Mapping from Kafka headers

The TIBCO Kafka consumer trigger exposes `$trigger.headers` as an object. If your producers write the event timestamp into a header (e.g. `event_ts`), add a Mapper activity before Aggregate and set:

```
eventTimestamp = int64($trigger.headers.event_ts)
```

This removes the requirement for the timestamp to live inside the message body.

### Mapping from a message payload field

This is the classic approach. Configure `eventTimeField` in settings (e.g. `event_ts_ms`) and the activity reads it from the message automatically. No change needed to the flow inputs.

### Mapping from a computed expression

You can set `eventTimestamp` to any Flogo expression that produces an `int64`, for example the current time in milliseconds from a custom utility activity:

```
eventTimestamp = =$activity[get-current-time-ms].result
```

### TumblingTime

Fixed time-bucket. Opens on the first event after a reset. Closes when an event arrives whose timestamp is ≥ `windowStart + windowSize` ms. The triggering event is **included** in the result. Buffer resets and the clock restarts from that event's timestamp.

```
──E1──E2──E3───────────────E4──►
  |←── windowSize ──────────►| close; result emitted; buffer reset
```

`windowSize` in milliseconds (e.g. `5000` = 5 s, `300000` = 5 min).

### TumblingCount

Accumulates exactly `windowSize` events, emits the aggregate, then resets.

```
──E1──E2──E3── close ──E4──E5──E6── close ──►
```

`windowSize` in event count.

### SlidingTime

Rolling view of events within the last `windowSize` ms. Emits on **every** event. Events outside the window age out automatically.

### SlidingCount

Rolling view of the last `windowSize` events. Emits on **every** event.

> For sliding windows `windowClosed` is always `true` — every call produces a result.

## Keyed Windows

When `keyField` is set, a separate sub-window is maintained per unique key value. Sub-windows are named `windowName:keyValue` internally and are fully independent.

| Incoming message | Sub-window |
|------------------|-----------|
| `{"device_id":"A","temp":22}` | `iot-avg:A` |
| `{"device_id":"B","temp":30}` | `iot-avg:B` |

## State Persistence

When `persistPath` and `persistEveryN` are both set, the activity writes a gob-encoded snapshot of all active window state (including keyed sub-windows) after every N messages.

On restart the snapshot is loaded during activity init (`New()`). Keyed sub-windows, which are created lazily on first event, have their state parked in an internal `pendingRestores` map and applied the moment the sub-window is first created — no state is silently lost.

A missing, corrupt, or permission-denied snapshot file logs a warning and the app continues with empty state. Startup is never blocked.

## Configuration Examples

### 5-minute average temperature per device, with event-time

```json
{
  "settings": {
    "windowName":     "iot-temp-5min",
    "windowType":     "TumblingTime",
    "windowSize":     300000,
    "function":       "avg",
    "eventTimeField": "event_ts_ms",
    "allowedLateness": 30000
  },
  "input": {
    "message":    "=$flow.message",
    "valueField": "temperature",
    "keyField":   "device_id"
  }
}
```

### Count-based window with deduplication

```json
{
  "settings": {
    "windowName":     "txn-sum-100",
    "windowType":     "TumblingCount",
    "windowSize":     100,
    "function":       "sum",
    "maxBufferSize":  120,
    "overflowPolicy": "drop_oldest"
  },
  "input": {
    "message":        "=$flow.message",
    "valueField":     "amount",
    "messageIDField": "txn_id"
  }
}
```

### Persistent state across restarts

```json
{
  "settings": {
    "windowName":    "persist-demo",
    "windowType":    "TumblingTime",
    "windowSize":    15000,
    "function":      "avg",
    "persistPath":   "/tmp/agg-state.gob",
    "persistEveryN": 1
  },
  "input": {
    "message":    "=$flow.message",
    "valueField": "temperature",
    "keyField":   "device_id"
  }
}
```

### Idle-timeout + cardinality guard

```json
{
  "settings": {
    "windowName":    "per-device-bytes",
    "windowType":    "TumblingTime",
    "windowSize":    60000,
    "function":      "sum",
    "idleTimeoutMs": 120000,
    "maxKeys":       10000
  },
  "input": {
    "message":    "=$flow.message",
    "valueField": "bytes_sent",
    "keyField":   "device_id"
  }
}
```

## Recommended Flow Branching

```
[Kafka Consumer Trigger]
        │
        ▼
[Kafka Stream Aggregate]
        │
        ├── lateEvent=true      → Produce to dead-letter topic
        ├── windowClosed=false  → Stop (window still accumulating)
        └── windowClosed=true   → Continue to downstream activity
```

## Error Reference

| Condition | Behaviour |
|-----------|-----------|
| `valueField` missing from message | Flow invocation fails with a descriptive error |
| `valueField` non-numeric | Flow invocation fails |
| Invalid `windowType`, `function`, or `overflowPolicy` at init | `New()` fails; app does not start |
| `windowSize ≤ 0` at init | `New()` fails |
| `overflowPolicy=error` and buffer full | `Eval()` returns `WindowError`; flow invocation fails |
| `maxKeys` exceeded | `Eval()` returns an error |
| Late event (`lateEvent=true`) | No error; route via `lateEvent` output |
| Missing / corrupt / unreadable persist file at startup | Warning logged; app starts with empty state |

## Go Module

```
github.com/mpandav-tibco/flogo-extensions/kafkastream/activity/aggregate
```

# Aggregate Kafka Stream Trigger


Consumes messages from a Kafka topic, accumulates a numeric field into a stateful in-memory window, and fires the Flogo flow when the window closes. The trigger owns its own Kafka consumer transport.

Supports:
- Tumbling and sliding windows (time-based or count-based)
- Keyed sub-windows — independent windows per unique field value (e.g. one window per `device_id`)
- Event-time watermarks for correct out-of-order handling
- Late-event routing to a dead-letter flow handler
- Overflow policies when the in-memory buffer is capped
- Per-window message deduplication
- Idle-timeout auto-close for keyed sub-windows
- Gob-encoded state persistence across restarts

---

## Trigger Settings

| Setting | Type | Required | Default | Description |
|---------|------|----------|---------|-------------|
| `kafkaConnection` | connection | ✓ | — | TIBCO Kafka shared connection (broker addresses, auth, TLS). |
| `topic` | string | ✓ | — | Kafka topic to consume from. |
| `consumerGroup` | string | ✓ | — | Kafka consumer group ID. |
| `initialOffset` | string | | `newest` | `newest` or `oldest` — where to start when no committed offset exists for this consumer group. |
| `windowName` | string | ✓ | — | Unique identifier for this window instance across flow executions (e.g. `sensor-5s-sum`). |
| `windowType` | string | ✓ | `TumblingTime` | `TumblingTime` · `TumblingCount` · `SlidingTime` · `SlidingCount` |
| `windowSize` | integer | ✓ | `5000` | Time-based: milliseconds (e.g. `5000` = 5 s). Count-based: number of events. |
| `function` | string | ✓ | `sum` | Aggregation function applied to `valueField` over the window. `sum` · `count` · `avg` · `min` · `max` |
| `valueField` | string | ✓ | — | Message field whose numeric value is aggregated. |
| `keyField` | string | | — | When set, independent sub-windows are maintained per unique value of this field (e.g. `device_id`). |
| `eventTimeField` | string | | — | Message field containing the event timestamp (Unix-ms int64 or RFC-3339 string). Leave empty to use wall-clock time. |
| `messageIDField` | string | | — | Field used as a unique event ID for per-window deduplication. Leave empty to disable. |
| `allowedLateness` | integer | | `0` | Maximum age in ms beyond the watermark that a late event is still accepted. `0` = reject all late events immediately. |
| `maxBufferSize` | integer | | `0` | Cap on the number of values buffered per window. `0` = unlimited. |
| `overflowPolicy` | string | | `drop_oldest` | What to do when `maxBufferSize` is exceeded: `drop_oldest` · `drop_newest` · `error` |
| `idleTimeoutMs` | integer | | `0` | Auto-close a keyed sub-window that receives no event for this many ms and emit its partial result. `0` = disabled. |
| `maxKeys` | integer | | `0` | Cap on the number of concurrent keyed sub-windows. `0` = unlimited. |
| `persistPath` | string | | — | File path for gob-encoded window state snapshots. Leave empty to disable persistence. |
| `persistEveryN` | integer | | `0` | Snapshot state every N messages. `0` = persist only on graceful shutdown. |

---

## Handler Settings

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `eventType` | string | `windowClose` | `windowClose` — fires when the window closes with the aggregate result. `lateEvent` — fires when a message is rejected as late (use for DLQ routing). `all` — fires for both event types. |

---

## Flow Outputs

| Output | Type | Fields |
|--------|------|--------|
| `windowResult` | object | `result` (float64) — aggregate value; `count` (int) — number of events in the window; `windowName` (string); `key` (string) — keyed sub-window key, empty for unkeyed windows; `windowClosed` (bool) — always `true` on `windowClose` event; `droppedCount` (int) — events dropped due to overflow; `lateEventCount` (int) — late events rejected in this window |
| `source` | object | `topic` (string); `partition` (int); `offset` (int); `lateEvent` (bool) — `true` when this invocation is for a late event; `lateReason` (string) — why the event was considered late |

---

## Window Types

| Type | Behaviour |
|------|-----------|
| `TumblingTime` | Fixed time bucket — fires when `windowSize` ms elapses, then resets. The event that crosses the boundary is included in the closing window. |
| `TumblingCount` | Fixed count batch — fires after exactly `windowSize` events, then resets. |
| `SlidingTime` | Rolling view of events received in the last `windowSize` ms — fires on every incoming event. |
| `SlidingCount` | Rolling view of the last `windowSize` events — fires on every incoming event. |

---

## Aggregate Functions

| Function | Description |
|----------|-------------|
| `sum` | Sum of all values in the window |
| `avg` | Arithmetic mean |
| `count` | Number of events |
| `min` | Minimum value |
| `max` | Maximum value |

---

## Example — Sum temperature every 5 events per device

Configure the trigger on your flow:

| Setting | Value |
|---------|-------|
| Kafka Connection | _(your shared connection)_ |
| Topic | `iot-sensors` |
| Consumer Group | `agg-temp-cg` |
| Window Name | `temp-count5-sum` |
| Window Type | `TumblingCount` |
| Window Size | `5` |
| Function | `sum` |
| Value Field | `temperature` |
| Key Field | `device_id` |
| Handler Event Type | `windowClose` |

The flow fires once per 5 messages per device. Map `$trigger.windowResult.result` to get the sum, and `$trigger.windowResult.key` to get which device the window belongs to.

## Example — 10-second rolling average with late-event DLQ

| Setting | Value |
|---------|-------|
| Window Type | `TumblingTime` |
| Window Size | `10000` |
| Function | `avg` |
| Value Field | `price` |
| Event Time Field | `event_ts` |
| Allowed Lateness | `2000` |

Add two handlers: one with `eventType=windowClose` for normal processing, one with `eventType=lateEvent` to route rejected late events to a dead-letter topic.

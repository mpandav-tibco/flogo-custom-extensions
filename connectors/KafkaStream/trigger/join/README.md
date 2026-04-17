# Join Kafka Streams Trigger



Subscribes to two or more Kafka topics and fires the Flogo flow when messages carrying the **same join key value** have been received from **every configured topic** within a configurable time window — a classic stream-join / stream-enrichment pattern. Each topic uses its own Sarama consumer group. When the window expires before all topics contribute, an optional `timeout` handler fires with the partial data.

The trigger owns its own Kafka transport.

```
demo-readings  ──►┐
                  │  joinKeyField="device_id"      ┌──► [joined handler]  → both messages arrived
                  ├──[join window: 30 s]───────────┤
demo-alerts    ──►┘                                └──► [timeout handler] → only partial data arrived
```

---

## Trigger Settings

| Setting | Type | Required | Default | Description |
|---------|------|----------|---------|-------------|
| `kafkaConnection` | connection | ✓ | — | TIBCO Kafka shared connection (broker addresses, auth, TLS). |
| `topics` | string | ✓ | — | Comma-separated list of Kafka topics to join (minimum 2). Example: `orders,payments`. |
| `consumerGroup` | string | ✓ | — | Base consumer group ID. The trigger creates one group per topic: `<base>-<topicName>`. E.g. `my-join-cg-orders`, `my-join-cg-payments`. Must be unique per trigger instance. |
| `joinKeyField` | string | ✓ | — | Message field whose value is used to correlate messages across topics. E.g. `order_id`, `device_id`. |
| `joinWindowMs` | integer | ✓ | `30000` | Maximum time in milliseconds to wait for all topics to contribute a matching message. When expired, a timeout event is emitted. |
| `initialOffset` | string | | `newest` | `newest` or `oldest` — where to start when no committed offset exists for this consumer group. |
| `balanceStrategy` | string | | `roundrobin` | Kafka consumer group rebalance strategy: `roundrobin` · `sticky` · `range`. Applied to all per-topic consumer groups. |
| `commitOnSuccess` | boolean | | `true` | When `true`, the completing (last-arriving) message's offset is marked only after all handlers complete without error (at-least-once). When `false`, the offset is always committed. |
| `handlerTimeoutMs` | integer | | `0` | Maximum time in milliseconds for all handlers to complete. `0` = no timeout. |

---

## Handler Settings

| Setting | Type | Required | Default | Description |
|---------|------|----------|---------|-------------|
| `eventType` | string | | `joined` | Which events this handler receives. `joined` — fires when all topics contribute within the window. `timeout` — fires when the window expires before all topics contribute. `all` — fires for both joined and timeout events. |

> **Important — one consumer group per trigger instance:** If you configure two separate trigger instances pointing at the same topics and consumer group (e.g. one for `joined`, one for `timeout`), Kafka will split the partitions between them. Each instance will only see a subset of messages and joins will never complete. Use **a single trigger instance** with `eventType: "all"` to handle both outcomes, or use two instances with **different `consumerGroup` values**.

---

## Flow Outputs

### When `eventType = "joined"`

`joinResult` is populated; `timeoutResult` is zero-value.

| Output | Type | Description |
|--------|------|-------------|
| `joinResult.messages` | object | Map of topic name → full decoded JSON payload. E.g. `{"demo-readings": {...}, "demo-alerts": {...}}` |
| `joinResult.joinKey` | string | The value of `joinKeyField` that triggered the join. |
| `joinResult.topics` | array | Ordered list of topic names that contributed. |
| `joinResult.joinedAt` | integer | Unix-ms wall-clock time when the join completed. |
| `timeoutResult` | object | Zero-value (all fields empty/null). |
| `eventType` | string | `"joined"` |

### When `eventType = "timeout"`

`timeoutResult` is populated; `joinResult` is zero-value.

| Output | Type | Description |
|--------|------|-------------|
| `timeoutResult.partialMessages` | object | Map of topic name → payload for topics that contributed before the window expired. |
| `timeoutResult.joinKey` | string | The join key that timed out. |
| `timeoutResult.missingTopics` | array | Topics that did not contribute before expiry. |
| `timeoutResult.createdAt` | integer | Unix-ms time when the join window was first opened. |
| `joinResult` | object | Zero-value (all fields empty/null). |
| `eventType` | string | `"timeout"` |

---

## Example — Join device readings with threshold alerts

**Scenario:** `demo-readings` carries sensor temperature data; `demo-alerts` carries per-device alert thresholds. Correlate both on `device_id` within 30 seconds.

| Setting | Value |
|---------|-------|
| Kafka Connection | _(your shared connection)_ |
| Topics | `demo-readings,demo-alerts` |
| Consumer Group | `demo-join-cg` |
| Join Key Field | `device_id` |
| Join Window (ms) | `30000` |
| Handler Event Type | `all` |

**Incoming messages:**
```json
// demo-readings
{"device_id": "sensor-1", "temperature": 72, "unit": "C"}

// demo-alerts
{"device_id": "sensor-1", "threshold": 70, "severity": "HIGH"}
```

**joinResult.messages in the flow:**
```json
{
  "demo-readings": {"device_id": "sensor-1", "temperature": 72, "unit": "C"},
  "demo-alerts":   {"device_id": "sensor-1", "threshold": 70, "severity": "HIGH"}
}
```

**Branching inside the flow on `$trigger.eventType`:**
- `joined` → both topics arrived → compare `temperature` vs `threshold`
- `timeout` → only one topic arrived → route partial data to DLQ or alert

---

## Example — Three-way join (orders + payments + shipping)

Set `topics` to `orders,payments,shipping`. All three must contribute a message with the same `order_id` within the window for the joined handler to fire.

```
topics = "orders,payments,shipping"
joinKeyField = "order_id"
joinWindowMs = 60000
```

---

## Offset Commit Behaviour

The join trigger involves messages arriving from multiple topics in arbitrary order. Sarama consumer sessions cannot be held open across topic boundaries, so offset commit semantics are asymmetric:

| Topic role | When offset is committed |
|------------|--------------------------|
| **Non-completing topics** (first to arrive) | Immediately after their contribution is recorded in the join store |
| **Completing topic** (last to arrive) | After all handlers complete (when `commitOnSuccess=true`) or immediately (when `commitOnSuccess=false`) |

This means non-completing topic offsets are always committed eagerly — even if the subsequent joined-handler fails. The trade-off is inherent to multi-topic joins over independent consumer groups.

---

## Join Store Backends

The trigger supports two backing stores, selected via the `storeType` setting.

| `storeType` | Restart recovery | Rebalance handoff | Extra dependencies |
|-------------|------------------|-------------------|--------------------|
| `memory` (default) | ✗ | ✗ | none |
| `file` | ✓ | ✓ (single-instance or shared FS) | none |

### `storeType: "memory"` (default)

No additional settings required. Best for development and single-instance deployments where losing in-flight state on restart is acceptable.

### `storeType: "file"`

In-flight join windows are written to a JSON snapshot file on graceful shutdown and before each consumer rebalance. They are restored on startup and after rebalance.

| Setting | Description |
|---------|-------------|
| `persistPath` | **Required.** Absolute path for the snapshot file. Example: `/var/data/flogo/join-state.json` |

> **Multi-instance note:** For cross-instance state sharing place `persistPath` on a shared filesystem (NFS, EFS, Azure Files). All instances must be able to read and write the same path.

---

## Limitations

- **Non-completing topic offsets are committed eagerly (all store types).** When a message arrives but does not yet complete the join, its offset is committed immediately so the Sarama consumer session is not stalled waiting for the other topics. This means if the join subsequently times out, that message will not be re-delivered. This trade-off is inherent to multi-topic joins over independent consumer groups.

- **Shared consumer group across trigger instances splits partitions.** If two trigger instances (e.g. one for `joined`, one for `timeout`) point at the same topics and the same `consumerGroup`, Kafka distributes partitions across both instances. Each instance sees only a subset of messages and joins will never complete — both sides timeout. Use a **single trigger instance** with `eventType: "all"`, or assign each instance a distinct `consumerGroup` value.

- **Minimum 2 topics required.** Setting `topics` to a single topic name returns an error at startup. Duplicate topic names in the list are also rejected.

- **`memory` store: state lost on restart and rebalance.** Use `storeType: "file"` to survive restarts and rebalances.

- **`file` store: rebalance handoff requires shared filesystem for multi-instance.** For single-instance deployments a local path is sufficient. For multi-instance deployments all instances must write to the same shared `persistPath`.


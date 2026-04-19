# Split Kafka Stream Trigger


Consumes messages from a Kafka topic and routes each message to one or more handler branches based on content-based predicates — a classic stream-split / content-based-routing pattern. It is the trigger-native equivalent of routing tables in Kafka Streams topologies. The trigger owns its own Kafka consumer transport.

Supports:
- Single-predicate mode — evaluate one field with one operator and value per handler
- Multi-predicate mode — evaluate a JSON array of conditions with AND or OR logic per handler
- Two routing modes: first-match (if-else chain) and all-match (fan-out)
- Priority-based handler evaluation order for first-match routing
- Unmatched catch-all handler for messages that match no branch
- Evaluation-error handler for DLQ routing of structurally problematic messages
- Tap ("all") handler for monitoring/audit without affecting routing
- Per-handler and per-message timeout caps to prevent Kafka session timeouts
- OTel trace propagation (trace context extracted from Kafka message headers)

```
                     ┌──► [handler A: status=ok]     priority=1
                     │
  iot-sensors  ──►───┼──► [handler B: region=us]     priority=2   ← first-match: only one fires
                     │
                     ├──► [unmatched handler]        catch-all / DLQ
                     └──► [evalError handler]        error DLQ
```

---

## Trigger Settings

| Setting | Type | Required | Default | Description |
|---------|------|----------|---------|-------------|
| `kafkaConnection` | connection | ✓ | — | TIBCO Kafka shared connection (broker addresses, auth, TLS). |
| `topic` | string | ✓ | — | Kafka topic to consume from. |
| `consumerGroup` | string | ✓ | — | Kafka consumer group ID. Each trigger instance in the same group shares partition load. |
| `initialOffset` | string | | `newest` | `newest` or `oldest` — where to start when no committed offset exists for this consumer group. |
| `routingMode` | string | | `first-match` | Controls how matched messages are distributed to handlers. `first-match` — route to the first handler (by `priority` order) whose predicates match; behaves like an if-else chain — only one branch fires per message. `all-match` — route to ALL handlers whose predicates match; enables fan-out where every matching branch receives the same message. |
| `balanceStrategy` | string | | `roundrobin` | Kafka consumer group rebalance strategy: `roundrobin` · `sticky` · `range`. |
| `commitOnSuccess` | boolean | | `true` | When `true`, the Kafka offset is marked only after all handlers complete without error (at-least-once). When `false`, the offset is always committed regardless of handler result (at-most-once). |
| `handlerTimeoutMs` | integer | | `0` | Maximum time in milliseconds for each individual handler invocation. `0` = no per-handler timeout. When exceeded the handler is treated as failed; with `commitOnSuccess=true` the offset is not marked. |
| `messageTimeoutMs` | integer | | `0` | Maximum total time in milliseconds for ALL handler invocations combined for a single message (matched + unmatched + evalError + tap handlers). In `all-match` mode multiple handlers fire sequentially; without this cap, per-message latency can reach N × `handlerTimeoutMs`, risking a Kafka session timeout and unnecessary consumer-group rebalance. `0` = no per-message cap. **Recommended:** set to `handlerTimeoutMs` × (expected max matching handlers). |

---

## Handler Settings

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `eventType` | string | `matched` | Which events this handler receives. `matched` — fires when this handler's predicates match the incoming message. `unmatched` — fires when no matched-type handler matched (catch-all / DLQ). `evalError` — fires when predicate evaluation fails for any handler (error DLQ). `all` — fires for every message regardless of the routing outcome (tap / monitor / audit). |
| `field` | string | — | Message field to evaluate in single-predicate mode. Leave empty to use multi-predicate mode via `predicates`. Ignored for `unmatched`, `evalError`, and `all` event types. |
| `operator` | string | — | Comparison operator for single-predicate mode. `eq` · `neq` · `gt` · `gte` · `lt` · `lte` · `contains` · `startsWith` · `endsWith` · `regex` |
| `value` | string | — | Comparison value for single-predicate mode. |
| `predicates` | string (JSON) | — | Multi-predicate definition as a JSON array. Each element must have `field`, `operator`, and `value` keys. E.g. `[{"field":"status","operator":"eq","value":"ok"},{"field":"region","operator":"eq","value":"us-east"}]` |
| `predicateMode` | string | `and` | Logic for multi-predicate evaluation. `and` — all predicates must pass. `or` — at least one must pass. |
| `priority` | integer | `0` | Evaluation order for `first-match` routing mode. Handlers with lower values are evaluated first. Handlers with equal priority are evaluated in registration order. Ignored for `unmatched`, `evalError`, and `all` event types. |

---

## Flow Outputs

| Output | Type | Description |
|--------|------|-------------|
| `message` | object | Full parsed message payload as a JSON object. |
| `topic` | string | Kafka topic the message was consumed from. |
| `partition` | integer | Kafka partition number. |
| `offset` | integer | Kafka offset of the message. |
| `key` | string | Kafka message key. Empty string if the producer sent a keyless message. |
| `matchedHandlerName` | string | Name of the handler branch that matched. Empty string for `unmatched`, `evalError`, and `all`-type handler invocations. |
| `routingMode` | string | The routing mode that was applied: `first-match` or `all-match`. |
| `evalError` | boolean | `true` when this invocation was triggered by a predicate evaluation failure (operator error, type coercion error, invalid regex). Always `false` for `matched` and `unmatched` events. |
| `evalErrorReason` | string | Human-readable description of why evaluation failed. Empty string when `evalError` is `false`. |

---

## Routing Modes

| Mode | Behaviour |
|------|-----------|
| `first-match` | Handlers of type `matched` are sorted by `priority` (ascending). The trigger evaluates each handler's predicates in order and **stops at the first match** — like an if-else chain. Only one `matched` handler fires per message. If none match, the `unmatched` handler fires. |
| `all-match` | The trigger evaluates **every** `matched` handler's predicates. All handlers whose predicates pass fire for the same message — enabling fan-out to multiple branches. If none match, the `unmatched` handler fires. |

In both modes, `evalError` handlers fire when any predicate evaluation fails, and `all`-type handlers fire for every message regardless of the routing outcome.

---

## Filter Operators

| Operator | Applies To | Description |
|----------|------------|-------------|
| `eq` | numeric / string | Equal |
| `neq` | numeric / string | Not equal |
| `gt` | numeric | Greater than |
| `gte` | numeric | Greater than or equal |
| `lt` | numeric | Less than |
| `lte` | numeric | Less than or equal |
| `contains` | string | Field value contains the substring |
| `startsWith` | string | Field value starts with the prefix |
| `endsWith` | string | Field value ends with the suffix |
| `regex` | string | Field value matches the regular expression |

---

## Example — Route by status code (first-match)

Route HTTP-status messages to different flows based on `status`:

| Handler | `eventType` | `field` | `operator` | `value` | `priority` | Purpose |
|---------|-------------|---------|------------|---------|------------|---------|
| success | `matched` | `status` | `eq` | `200` | `1` | Process successful responses |
| client-error | `matched` | `status` | `gte` | `400` | `2` | Route 4xx errors |
| catch-all | `unmatched` | — | — | — | — | DLQ for unrouted messages |

With `routingMode=first-match`, a message with `status=200` matches handler 1 and stops. A message with `status=500` does not match handler 1 or 2, so the `unmatched` handler fires.

## Example — Fan-out to multiple branches (all-match)

Tag messages that belong to multiple categories:

| Handler | `eventType` | `field` | `operator` | `value` | Purpose |
|---------|-------------|---------|------------|---------|---------|
| high-temp | `matched` | `temperature` | `gt` | `75` | High temperature branch |
| us-region | `matched` | `region` | `eq` | `us-east` | US-East branch |
| audit | `all` | — | — | — | Audit log (tap) |

With `routingMode=all-match`, a message with `temperature=80` and `region=us-east` fires **both** `high-temp` and `us-region` handlers. The `audit` handler always fires.

## Example — Multi-predicate handler

Set the handler `predicates` field to:

```json
[
  {"field": "temperature", "operator": "gt", "value": "75"},
  {"field": "region", "operator": "eq", "value": "us-east"}
]
```

With `predicateMode=and`, the handler only matches messages where both conditions are true.

## Example — Evaluation error DLQ routing

Errors during predicate evaluation (bad operator, type coercion failure, invalid regex) fire `evalError` handlers. Configure an error handler alongside your routing handlers:

| Handler | `eventType` | Purpose |
|---------|-------------|---------|
| branch-A | `matched` | Normal routing — messages matching branch A |
| branch-B | `matched` | Normal routing — messages matching branch B |
| error-dlq | `evalError` | Error DLQ — receives messages that caused evaluation failure |

In the DLQ handler, map `$trigger.evalError` (always `true`) and `$trigger.evalErrorReason` to understand why evaluation failed, then route the raw `$trigger.message` to a dead-letter topic.

---

## Handler Failure Behaviour

| Handler type | Failure impact |
|--------------|----------------|
| `matched` | With `commitOnSuccess=true`, the offset is **not marked** — the message will be redelivered after restart or rebalance. |
| `unmatched` | Same as `matched` — respects `commitOnSuccess`. |
| `evalError` | Same as `matched` — respects `commitOnSuccess`. |
| `all` (tap) | Failure is **logged but does not block** offset commit. Tap handlers are non-blocking so monitoring/audit failures never cause message redelivery. |

# Filter Kafka Stream Trigger


Consumes messages from a Kafka topic and fires the Flogo flow **only** for messages that satisfy the configured predicate(s). Messages that do not pass are silently acknowledged and dropped — the flow is never invoked for them. The trigger owns its own Kafka consumer transport.

Supports:
- Single-predicate mode — evaluate one field with one operator and value
- Multi-predicate mode — evaluate a JSON array of conditions with AND or OR logic
- Opt-in message deduplication using a configurable ID field
- Opt-in rate limiting (token bucket) with drop or wait behaviour

---

## Trigger Settings

| Setting | Type | Required | Default | Description |
|---------|------|----------|---------|-------------|
| `kafkaConnection` | connection | ✓ | — | TIBCO Kafka shared connection (broker addresses, auth, TLS). |
| `topic` | string | ✓ | — | Kafka topic to consume from. |
| `consumerGroup` | string | ✓ | — | Kafka consumer group ID. Each trigger instance in the same group shares partition load. |
| `initialOffset` | string | | `newest` | `newest` or `oldest` — where to start when no committed offset exists for this consumer group. |
| `operator` | string | | `eq` | Default comparison operator when the handler does not specify one. `eq` · `neq` · `gt` · `gte` · `lt` · `lte` · `contains` · `startsWith` · `endsWith` · `regex` |
| `predicateMode` | string | | `and` | Default logic for multi-predicate mode. `and` — all predicates must pass. `or` — at least one must pass. |
| `passThroughOnMissing` | boolean | | `false` | When `true`, messages where the evaluated field is absent are treated as passing. When `false` (default), they are dropped. |
| `enableDedup` | boolean | | `false` | When `true`, duplicate messages are suppressed using the handler-level `dedupField` as the unique event ID. |
| `dedupWindow` | string | | `10m` | How long to remember seen event IDs. Go duration string e.g. `10m`, `1h`. |
| `dedupMaxEntries` | integer | | `100000` | Maximum number of event IDs tracked in memory. |
| `rateLimitRPS` | number | | `0` | Maximum messages per second accepted. `0` = disabled. |
| `rateLimitBurst` | integer | | `0` | Token bucket burst size. `0` = same value as `rateLimitRPS`. |
| `rateLimitMode` | string | | `drop` | `drop` — excess messages are dropped immediately. `wait` — block the consumer up to `rateLimitMaxWaitMs`. |
| `rateLimitMaxWaitMs` | integer | | `500` | Maximum wait time in ms when `rateLimitMode=wait`. |

---

## Handler Settings

| Setting | Type | Description |
|---------|------|-------------|
| `field` | string | Message field to evaluate in single-predicate mode. Leave empty to use multi-predicate mode via `predicates`. |
| `operator` | string | Comparison operator for single-predicate mode. Overrides the trigger-level `operator` default. |
| `value` | string | Comparison value for single-predicate mode. |
| `predicates` | string (JSON) | Multi-predicate definition as a JSON array. Each element must have `field`, `operator`, and `value` keys. E.g. `[{"field":"status","operator":"eq","value":"200"},{"field":"region","operator":"eq","value":"us-east"}]` |
| `predicateMode` | string | Overrides the trigger-level `predicateMode` for this handler. `and` or `or`. |
| `dedupField` | string | Message field whose value is used as the unique event ID for deduplication. Only active when trigger-level `enableDedup` is `true`. |

---

## Flow Outputs

| Output | Type | Description |
|--------|------|-------------|
| `message` | object | Full parsed message payload as a JSON object. |
| `topic` | string | Kafka topic the message was consumed from. |
| `partition` | integer | Kafka partition number. |
| `offset` | integer | Kafka offset of the message. |
| `key` | string | Kafka message key. Empty string if the producer sent a keyless message. |

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

## Example — Pass only high-temperature alerts

Configure the trigger on your flow:

| Setting | Value |
|---------|-------|
| Kafka Connection | _(your shared connection)_ |
| Topic | `iot-sensors` |
| Consumer Group | `filter-temp-cg` |
| Handler Field | `temperature` |
| Handler Operator | `gt` |
| Handler Value | `75` |

The flow fires only when `temperature > 75`. Messages with `temperature <= 75` are acknowledged and dropped without invoking the flow. Map `$trigger.message` downstream.

## Example — Multi-predicate AND filter

Set the handler `predicates` field to:

```json
[
  {"field": "temperature", "operator": "gt", "value": "75"},
  {"field": "region", "operator": "eq", "value": "us-east"}
]
```

With `predicateMode=and`, only messages where both conditions are true trigger the flow.

## Example — Rate-limited consumer (max 10 msgs/sec)

| Setting | Value |
|---------|-------|
| Rate Limit RPS | `10` |
| Rate Limit Burst | `20` |
| Rate Limit Mode | `drop` |

Messages arriving faster than 10/sec are dropped after the burst of 20 is exhausted.

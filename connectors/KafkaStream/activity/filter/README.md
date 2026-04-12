# Kafka Stream Filter Activity

A Flogo activity that evaluates one or more predicates against fields in an incoming Kafka message and exposes a `passed` boolean output for flow branching.

Use it to gate which messages reach downstream activities (e.g. an Aggregate window), route different message types into different paths, or reject messages that fail a quality check before they enter any processing pipeline.

## Settings

Configured once per activity instance at design time. `operator` and `predicateMode` act as defaults that can be overridden per invocation via inputs.

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `operator` | string | `eq` | Default comparison operator. Applied when the `operator` input is empty. |
| `predicateMode` | string | `and` | Default multi-predicate logic: `and` (all must pass) or `or` (at least one must pass). Applied when the `predicateMode` input is empty. |
| `passThroughOnMissing` | boolean | `false` | When `true`, a message whose target field is absent passes through. When `false`, a missing field causes `passed=false`. Applies to both single and multi-predicate modes. |
| `enableDedup` | boolean | `false` | Activates message deduplication. When enabled, the `dedupField` input identifies the unique event ID field. Zero overhead when disabled. |
| `dedupWindow` | string | `10m` | Duration string (e.g. `5m`, `1h`) for which a seen message ID is remembered. Events with identical IDs within this window are suppressed. |
| `dedupMaxEntries` | integer | `100000` | Maximum number of dedup entries held in memory. Oldest entries are evicted when the cap is reached. |
| `rateLimitRPS` | float | `0` | Maximum events per second to pass through. `0` = disabled. |
| `rateLimitBurst` | integer | `0` | Token bucket burst capacity. Only relevant when `rateLimitRPS > 0`. |
| `rateLimitMode` | string | `drop` | Action when the rate limit is exceeded: `drop` (return `passed=false` immediately) or `wait` (block up to `rateLimitMaxWaitMs` before dropping). |
| `rateLimitMaxWaitMs` | integer | `500` | Maximum time to wait in `wait` mode before dropping the message. |

## Inputs

All inputs are runtime-mappable. `operator` and `predicateMode` override their setting defaults when non-empty.

| Input | Type | Required | Description |
|-------|------|----------|-------------|
| `message` | object | Yes | Kafka message payload (`map[string]interface{}`). Typically mapped from the Kafka consumer trigger. |
| `field` | string | No* | Field name to evaluate in single-predicate mode. Not used when `predicates` is provided. |
| `operator` | string | No | Comparison operator for this invocation. Overrides the `operator` setting when non-empty. |
| `value` | string | No* | Comparison value in single-predicate mode. |
| `predicates` | string | No | JSON array of predicate objects for multi-predicate evaluation. Takes priority over `field`/`operator`/`value` when non-empty. Format: `[{"field":"f","operator":"op","value":"v"},...]` |
| `predicateMode` | string | No | Overrides the `predicateMode` setting for this invocation when non-empty. |
| `dedupField` | string | No | Message field used as the unique event ID. Only active when `enableDedup=true`. |

*Either `field` + `value` (single-predicate) or `predicates` (multi-predicate) must be provided.

## Outputs

| Output | Type | Description |
|--------|------|-------------|
| `passed` | boolean | `true` if the message satisfies the predicate(s). |
| `message` | object | The original message. Always set (even when `passed=false`) so the caller can log, route to DLQ, or inspect the payload in all branches. |
| `reason` | string | Human-readable explanation when `passed=false`. Empty when `passed=true`. |
| `errorMessage` | string | Set when evaluation encounters an error (e.g. invalid operator, malformed regex, unexpected type). Route `errorMessage != ""` to a dead-letter topic independently of normal filter-outs. |

## Operators

### Numeric Operators

Both the field value and the `value` input are coerced to `float64`. If either side is non-numeric, `eq` and `neq` fall back to string comparison; `gt`, `gte`, `lt`, `lte` set `errorMessage`.

| Operator | Description |
|----------|-------------|
| `gt` | Field > value |
| `gte` | Field ≥ value |
| `lt` | Field < value |
| `lte` | Field ≤ value |
| `eq` | Field = value (numeric or string fallback) |
| `neq` | Field ≠ value (numeric or string fallback) |

### String Operators

Field value is always treated as a string.

| Operator | Description |
|----------|-------------|
| `contains` | Field contains the substring |
| `startsWith` | Field starts with the prefix |
| `endsWith` | Field ends with the suffix |
| `regex` | Field matches the regular expression |

> Regex patterns are compiled per invocation because `operator` is a runtime input. A malformed pattern sets `errorMessage` rather than failing the flow hard.

## Settings vs. Input Override

`operator` and `predicateMode` exist in both settings and inputs to allow flexible design:

| Settings value | Input value | Effective value |
|----------------|-------------|-----------------|
| `gt` | _(empty)_ | `gt` |
| _(empty)_ | `lt` | `lt` |
| `gt` | `lt` | `lt` (input wins) |

## Deduplication

When `enableDedup=true`, the activity maintains an in-memory LRU cache keyed on the value of `dedupField`. If a message with an already-seen ID arrives within the `dedupWindow` duration, it is suppressed: `passed=false` with `reason="duplicate"`.

This is separate from the Aggregate activity's per-window deduplication — Filter-level dedup operates across the entire stream regardless of window boundaries.

## Rate Limiting

When `rateLimitRPS > 0`, a token bucket is maintained per activity instance. Excess messages are either dropped immediately (`drop` mode) or held for up to `rateLimitMaxWaitMs` ms before being dropped (`wait` mode). Dropped messages return `passed=false` with `reason="rate_limited"`.

## Configuration Examples

### Single numeric threshold (operator in settings)

```json
{
  "settings": { "operator": "gt" },
  "input": {
    "message":  "=$flow.message",
    "field":    "temperature",
    "value":    "25"
  }
}
```

### Operator driven by an app property at runtime

```json
{
  "settings": { "operator": "gt" },
  "input": {
    "message":  "=$flow.message",
    "field":    "price",
    "operator": "=$property[FILTER_OPERATOR]",
    "value":    "=$property[FILTER_THRESHOLD]"
  }
}
```

### Regex device-ID filter

```json
{
  "settings": { "operator": "regex" },
  "input": {
    "message": "=$flow.message",
    "field":   "device_id",
    "value":   "^sensor-[0-9]+$"
  }
}
```

### Pass through when field is absent

```json
{
  "settings": {
    "operator":             "eq",
    "passThroughOnMissing": true
  },
  "input": {
    "message": "=$flow.message",
    "field":   "priority",
    "value":   "high"
  }
}
```

### Multi-predicate AND chain

Pass only messages where `price > 100` AND `region == "US"`:

```json
{
  "settings": { "predicateMode": "and" },
  "input": {
    "message":    "=$flow.message",
    "predicates": "[{\"field\":\"price\",\"operator\":\"gt\",\"value\":\"100\"},{\"field\":\"region\",\"operator\":\"eq\",\"value\":\"US\"}]"
  }
}
```

### Multi-predicate OR chain with runtime mode override

```json
{
  "settings": { "predicateMode": "and" },
  "input": {
    "message":       "=$flow.message",
    "predicates":    "[{\"field\":\"priority\",\"operator\":\"eq\",\"value\":\"high\"},{\"field\":\"alertType\",\"operator\":\"eq\",\"value\":\"critical\"}]",
    "predicateMode": "or"
  }
}
```

### Deduplication enabled

Suppress re-delivered Kafka messages within a 5-minute window:

```json
{
  "settings": {
    "operator":        "gt",
    "enableDedup":     true,
    "dedupWindow":     "5m",
    "dedupMaxEntries": 50000
  },
  "input": {
    "message":    "=$flow.message",
    "field":      "temperature",
    "value":      "25",
    "dedupField": "event_id"
  }
}
```

### Filter + Aggregate pipeline

Only hot readings reach the aggregation window:

```json
[
  {
    "id": "filter_step",
    "activity": {
      "ref": "github.com/milindpandav/flogo-extensions/kafkastream/activity/filter",
      "settings": { "operator": "gt" },
      "input": { "message": "=$flow.message", "field": "temperature", "value": "25" }
    }
  },
  {
    "id": "aggregate_step",
    "activity": {
      "ref": "github.com/milindpandav/flogo-extensions/kafkastream/activity/aggregate",
      "settings": {
        "windowName": "hot-sensor-avg",
        "windowType": "TumblingCount",
        "windowSize": 10,
        "function":   "avg"
      },
      "input": { "message": "=$activity[filter_step].message", "valueField": "temperature" }
    }
  }
]
```

> Add a Branch condition after the Filter step that checks `$activity[filter_step].passed == false` and routes to a Stop activity so only `passed=true` messages reach the Aggregate.

## Recommended Flow Branching

```
[Kafka Consumer Trigger]
        │
        ▼
[Kafka Stream Filter]
        │
        ├── errorMessage != ""  → Produce to dead-letter topic
        ├── passed=false        → Stop / discard
        └── passed=true         → Continue to Aggregate or downstream
```

## Error Reference

| Condition | `passed` | `reason` | `errorMessage` |
|-----------|----------|----------|----------------|
| Field present, condition not met | `false` | set | — |
| Field absent, `passThroughOnMissing=false` | `false` | set | — |
| Field absent, `passThroughOnMissing=true` | `true` | — | — |
| Invalid operator | `false` | — | set |
| Malformed regex pattern | `false` | — | set |
| Non-numeric field with numeric operator | `false` | — | set |
| Malformed `predicates` JSON | `false` | — | set |
| Rate limit exceeded | `false` | `"rate_limited"` | — |
| Duplicate message (dedup active) | `false` | `"duplicate"` | — |



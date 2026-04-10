# Kafka Stream Filter Activity

A Flogo activity that evaluates a predicate (or a chain of predicates) against fields in an incoming Kafka message and exposes a `passed` boolean output for flow branching. Use it to gate messages before feeding them into an aggregate window, or to route different message types into different downstream paths.

**v2.0.0** — Enterprise features: multi-predicate AND/OR chains, regex support, ErrorMessage DLQ routing.

## Overview

When combined with the TIBCO Kafka consumer trigger, the Filter activity acts as an inline routing layer. Messages that satisfy the configured predicate continue downstream; messages that fail are typically handled by stopping the flow or routing to a discard path.

```
[Kafka Trigger] → message
       │
       ▼
[Kafka Stream Filter]  field="temperature"  operator="gt"  value="25"
       │
       ├── passed=true  → [Kafka Stream Aggregate] → alert
       └── passed=false → [Stop / Discard]
```

## Configuration

### Settings

| Setting | Type | Required | Default | Description |
|---------|------|----------|---------|-------------|
| `field` | string | No* | — | Name of the message field to evaluate. Required for single-predicate mode. Not used when `predicates` is set. |
| `operator` | string | No* | `gt` | Comparison operator for single-predicate mode. See [Operators](#operators) below. |
| `value` | string | No* | — | The comparison value as a string for single-predicate mode. |
| `passThroughOnMissing` | boolean | No | `false` | When `true`, if the configured field is absent from the message the filter **passes** the message through. When `false` (default) a missing field causes `passed=false`. |
| `predicates` | string | No | — | JSON array of predicate objects for multi-predicate chains. When set, `field`/`operator`/`value` are ignored. Format: `[{"field":"f","operator":"op","value":"v"},...]` |
| `predicateMode` | string | No | `and` | How to combine multiple predicates: `and` (all must pass) or `or` (at least one must pass). Only used when `predicates` is set. |

*One of `field`+`operator`+`value` or `predicates` must be configured.

### Inputs

| Input | Type | Required | Description |
|-------|------|----------|-------------|
| `message` | object | Yes | The Kafka message payload as a `map[string]interface{}`. |

### Outputs

| Output | Type | Description |
|--------|------|-------------|
| `passed` | boolean | `true` if the message satisfies the predicate(s); `false` otherwise. |
| `message` | object | The original message payload. Only populated when `passed=true`; `nil` when `passed=false`. |
| `reason` | string | Human-readable explanation when `passed=false` (e.g. `"field 'temperature' (20) is not > 30"`). Empty when `passed=true`. |
| `errorMessage` | string | Set when predicate evaluation encounters an unexpected error (e.g. type mismatch, unsupported operator). Route `errorMessage != ""` to a dead-letter topic independently from normal filter-outs. |

## Operators

### Numeric Operators

These operators attempt to parse both the field value and the configured `value` as `float64`. If either side cannot be parsed as a number, `eq` and `neq` fall back to string comparison; `gt`, `gte`, `lt`, `lte` return `passed=false` with an explanatory reason.

| Operator | Description | Example |
|----------|-------------|---------|
| `gt` | Greater than | `temperature > 30` |
| `gte` | Greater than or equal | `count >= 100` |
| `lt` | Less than | `latency < 200` |
| `lte` | Less than or equal | `error_rate <= 0.05` |
| `eq` | Equal (numeric or string) | `status_code == 200` |
| `neq` | Not equal (numeric or string) | `status != "error"` |

### String Operators

These operators always treat the field value as a string (via `fmt.Sprintf`).

| Operator | Description | Example |
|----------|-------------|---------|
| `contains` | Field contains the substring | `message contains "timeout"` |
| `startsWith` | Field starts with the prefix | `topic startsWith "sensor."` |
| `endsWith` | Field ends with the suffix | `filename endsWith ".json"` |
| `regex` | Field matches the regular expression | `device_id matches `^sensor-[0-9]+$`` |

**Regex performance**: the regular expression is compiled once at activity initialisation (`New`), not per message. Misconfigured patterns are caught at startup.

## Usage Examples

### Example 1 — Numeric threshold filter

Pass only messages where `temperature` is greater than 25:

```json
{
  "id": "filter_hot",
  "name": "Filter Hot Events",
  "activity": {
    "ref": "github.com/milindpandav/flogo-extensions/kafka-stream/activity/filter",
    "settings": {
      "field": "temperature",
      "operator": "gt",
      "value": "25"
    },
    "input": {
      "message": "=$flow.message"
    }
  }
}
```

### Example 2 — String equality filter

Pass only messages from the `production` environment:

```json
{
  "settings": {
    "field": "env",
    "operator": "eq",
    "value": "production"
  }
}
```

### Example 3 — Regex device filter

Pass only messages from devices whose ID matches the pattern `^sensor-[0-9]+$`:

```json
{
  "settings": {
    "field": "device_id",
    "operator": "regex",
    "value": "^sensor-[0-9]+$"
  }
}
```

### Example 4 — Pass through on missing field (optional messages)

Allow messages that do not contain the `priority` field to continue downstream:

```json
{
  "settings": {
    "field": "priority",
    "operator": "eq",
    "value": "high",
    "passThroughOnMissing": true
  }
}
```

### Example 5 — Filter + Aggregate pipeline

Only temperature readings above 25 °C are fed into the rolling average window:

```json
[
  {
    "id": "filter_step",
    "activity": {
      "ref": "github.com/milindpandav/flogo-extensions/kafka-stream/activity/filter",
      "settings": { "field": "temperature", "operator": "gt", "value": "25" },
      "input": { "message": "=$flow.message" }
    }
  },
  {
    "id": "aggregate_step",
    "activity": {
      "ref": "github.com/milindpandav/flogo-extensions/kafka-stream/activity/aggregate",
      "settings": {
        "windowName": "hot-sensor-avg",
        "windowType": "TumblingCount",
        "windowSize": 10,
        "function": "avg",
        "valueField": "temperature"
      },
      "input": { "message": "=$activity[filter_step].message" }
    }
  }
]
```

> **Flow design tip**: add a Branch condition after the Filter activity that checks `$activity[filter_step].passed == false` and routes to a Return/Stop activity. Only `passed=true` messages should reach the Aggregate activity.

## Error Handling

| Error condition | Behaviour |
|-----------------|-----------|
| Invalid `operator` at init | `New` returns an error; the flow fails to start |
| Invalid `regex` pattern at init | `New` returns an error with the underlying syntax error |
| Field absent from message | `passed=false`, `reason` populated. If `passThroughOnMissing=true`, `passed=true` instead |
| Non-numeric value with numeric operator | `passed=false`, `reason` explains the type mismatch |

## Multi-Predicate Filter Chains (v2)

Set `predicates` to a JSON array to evaluate multiple conditions in a single activity call. Set `predicateMode` to `and` (default) or `or`.

### Example — AND chain (all conditions must pass)

```json
{
  "settings": {
    "predicates": "[{\"field\":\"price\",\"operator\":\"gt\",\"value\":\"100\"},{\"field\":\"region\",\"operator\":\"eq\",\"value\":\"US\"},{\"field\":\"status\",\"operator\":\"regex\",\"value\":\"^(active|pending)$\"}]",
    "predicateMode": "and"
  }
}
```

Passes only when `price > 100` **AND** `region == "US"` **AND** `status` matches the regex.

### Example — OR chain (at least one must pass)

```json
{
  "settings": {
    "predicates": "[{\"field\":\"priority\",\"operator\":\"eq\",\"value\":\"high\"},{\"field\":\"alertType\",\"operator\":\"eq\",\"value\":\"critical\"}]",
    "predicateMode": "or"
  }
}
```

Passes when `priority == "high"` **OR** `alertType == "critical"`.

### ErrorMessage DLQ Routing

When predicate evaluation encounters an unexpected error (e.g. a field has a type incompatible with the operator), the activity sets `errorMessage` instead of returning a hard error. This lets you route bad messages to a dead-letter topic without stopping the flow:

```
errorMessage != "" → [Produce to dead-letter Kafka topic]
passed=true        → [Normal downstream]
passed=false       → [Filtered-out branch / Stop]
```

## Combining Multiple Filters (single-predicate chaining)

For compound conditions without the multi-predicate JSON syntax, chain multiple Filter activities:

```
[Filter: temp > 25]
      │ passed=true
      ▼
[Filter: env == "production"]
      │ passed=true
      ▼
[Aggregate]
```

## Go Module

```
github.com/milindpandav/flogo-extensions/kafka-stream/activity/filter
```

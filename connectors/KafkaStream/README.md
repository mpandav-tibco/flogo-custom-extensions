# Kafka Stream Connector

A Flogo custom extension for self-contained, stateful Kafka stream processing. The connector provides **four triggers** — each trigger owns its own Kafka transport (consumer group, broker connection, offset management) and fires a Flogo flow directly.

```
Kafka Topic(s)
     │
     ├──► [Aggregate Trigger]  →  flow fires when window closes        →  downstream logic
     │
     ├──► [Filter Trigger]     →  flow fires when predicate passes      →  downstream logic
     │
     ├──► [Join Trigger]       →  flow fires when all topics contribute  →  downstream logic
     │
     └──► [Split Trigger]      →  flow fires per content-based route     →  downstream logic
```

---

## Triggers

| Trigger | Description | Details |
|---------|-------------|---------|
| **Aggregate** — `kafka-stream-aggregate-trigger` | Consumes messages from a Kafka topic and accumulates a numeric field into a stateful window (tumbling or sliding, time- or count-based). Fires the flow when the window closes with the aggregate result (`sum`, `avg`, `count`, `min`, `max`). Supports keyed sub-windows, event-time watermarks, late-event DLQ routing, overflow policies, deduplication, and state persistence. | [trigger/aggregate/README.md](trigger/aggregate/README.md) |
| **Filter** — `kafka-stream-filter-trigger` | Consumes messages from a Kafka topic and fires the flow only for messages that satisfy the configured predicate(s). Messages that do not pass are silently acknowledged and dropped. Supports single-predicate and multi-predicate AND/OR evaluation, opt-in deduplication, and opt-in rate limiting. | [trigger/filter/README.md](trigger/filter/README.md) |
| **Join** — `kafka-stream-join-trigger` | Subscribes to two or more Kafka topics and fires the flow when messages sharing the same join key value arrive from every configured topic within a time window (stream-join / stream-enrichment). Supports a `timeout` handler for partial / DLQ semantics when the window expires before all topics contribute. | [trigger/join/README.md](trigger/join/README.md) |
| **Split** — `kafka-stream-split-trigger` | Consumes messages from a Kafka topic and routes each message to one or more handler branches based on content-based predicates (content-based routing / stream-split). Supports first-match (if-else chain) and all-match (fan-out) routing modes, priority-ordered evaluation, unmatched catch-all handler, evaluation-error DLQ handler, tap/audit handler, per-handler and per-message timeout caps, and OTel trace propagation. | [trigger/split/README.md](trigger/split/README.md) |
---

## Getting Started


### Add the Connector to Your Flogo Application

In the VS Code Flogo extension, open **Extensions**, select **Add Custom Extension**, and point it at the `../connectors/KafkaStream` folder.



Once imported, the **Aggregate Kafka Stream Trigger**, **Filter Kafka Stream Trigger**, and **Join Kafka Streams Trigger** appear in the trigger palette.

---

## Module Structure

```
connectors/KafkaStream/          
├── go.mod                       
├── contribution.json            
├── icons/
├── registry.go                   ← process-scoped window state registry (used by aggregate trigger)
├── window/
│   ├── types.go
│   ├── tumbling.go
│   ├── sliding.go
│   └── window_test.go
└── trigger/
    ├── aggregate/                ← kafka-stream-aggregate-trigger — see README inside
    │   ├── trigger.go
    │   ├── trigger.json
    │   ├── metadata.go
    │   └── README.md
    ├── filter/                   ← kafka-stream-filter-trigger — see README inside
    │   ├── trigger.go
    │   ├── trigger.json
    │   ├── metadata.go
    │   └── README.md
    ├── join/                     ← kafka-stream-join-trigger — see README inside
    │   ├── trigger.go
    │   ├── trigger.json
    │   ├── metadata.go
    │   └── README.md
    └── split/                    ← kafka-stream-split-trigger — see README inside
        ├── trigger.go
        ├── trigger.json
        ├── metadata.go
        └── README.md
```

---

## Current Limitations

- **Single-process state only.** Window state lives in the memory of the running Flogo process. Multiple Flogo instances in the same consumer group each maintain independent window registries — aggregates are not merged across instances.
- **State lost on restart without persistence.** Configure `persistPath` and `persistEveryN` on the Aggregate trigger to enable gob-based snapshots. Without it, all in-flight window state is discarded on process stop.
- **Persistence is best-effort.** Snapshots are written synchronously every N messages but are not fsync'd. A hard crash between writes may lose the last N events.
- **No backpressure to the Kafka consumer.** Messages are consumed at whatever rate Kafka delivers them. Use `maxBufferSize` and `overflowPolicy` on the Aggregate trigger to control what happens under pressure.
- **No rebalance-aware state handoff.** When the consumer group rebalances, in-flight window state for reassigned partitions stays with the original process. The new consumer starts fresh.

---

## Dependencies

| Dependency | Version | Purpose |
|------------|---------|---------|
| `github.com/project-flogo/core` | v1.6.16 | Flogo trigger API |
| `github.com/IBM/sarama` | v1.46.3 | Kafka client |
| `github.com/tibco/wi-plugins/contributions/kafka/src/app/Kafka` | v0.0.0 | TIBCO Kafka shared connection |
| `golang.org/x/time` | v0.15.0 | Rate limiter (token bucket, used by filter trigger) |
| `github.com/stretchr/testify` | v1.11.1 | Test assertions |

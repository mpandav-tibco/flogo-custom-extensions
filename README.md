# Flogo Custom Extensions

This repository provides custom Flogo extensions including activities, triggers, and connectors for TIBCO Flogo applications.

## 📊 Component Overview

### 🔌 Connectors

| Component | Version | Type | Description |
|-----------|---------|------|-------------|
| [SSE Connector](connectors/sse/) | 1.0.0 | Connector | Server-Sent Events real-time streaming with event buffering and topic filtering |
| [Kafka Stream Connector](connectors/KafkaStream/) | 1.0.0 | Connector | Stateful windowed stream processing for Kafka messages — filtering, windowed aggregation, content-based routing, and event-time processing |

### 🧮 Functions

| Package | Functions | Description |
|---------|-----------|-------------|
| [math](function/math/) | `abs`, `pow`, `sqrt`, `log`, `log2`, `log10`, `sign`, `clamp` | Absolute value, power, roots, logarithms, sign, range clamping |
| [array](function/array/) | `min`, `max`, `avg`, `unique`, `indexOf`, `sort`, `sortDesc`, `first`, `last`, `sumBy`, `filter`, `pluck` | Numeric aggregation, deduplication, search, sorting, field aggregation, object filtering, field extraction |
| [string](function/string/) | `padLeft`, `padRight`, `mask`, `truncate`, `isBlank`, `isNumeric`, `camelCase`, `snakeCase`, `regexExtract`, `format` | Padding, PII masking, truncation, validation, case conversion, regex extraction, sprintf-style formatting |
| [util](function/util/) | `coalesce`, `sha256`, `hmacSha256`, `md5`, `base64UrlEncode`, `base64UrlDecode` | Null-coalescing, SHA-256/MD5 hashing, HMAC-SHA256 signing, URL-safe Base64 encoding |
| [datetime](function/datetime/) | `isBefore`, `isAfter`, `toEpoch`, `fromEpoch`, `isWeekend`, `isWeekday`, `addBusinessDays`, `startOfDay`, `quarter` | Datetime comparison, epoch conversion, business-day detection, day normalisation, calendar quarter |
| [number](function/number/) | `randomInt` | Cryptographically random integer in an inclusive range |
| [json](function/json/) | `removeKey`, `merge` | Delete a top-level key from an object, shallow-merge two or more objects |


### ⚡ Activities

| Component | Version | Category | Description |
|-----------|---------|----------|-------------|
| [Write Log](activity/write-log/) | 1.0.0 | Logging | Enterprise-grade logging with OpenTracing/OpenTelemetry integration and ECS compliance |
| [Template Engine](activity/templateengine/) | 1.0.0 | Content Generation | Dynamic content generation with Go templates, 29 built-in functions, and OOTB business templates |
| [AWS Signature V4 Generator](activity/awssignaturev4/) | 1.0.0 | AWS Integration | Generate AWS Signature Version 4 authentication headers for secure REST API calls to AWS services |
| [XML Filter](activity/xmlfilter/) | 0.1.0 | XML Processing | Filter XML content using XPath expressions with AND/OR logic support |
| [Avro Schema Transform](activity/schema-transform/avroschematransform/) | 1.0.0 | Schema Transform | Transform Avro schemas to JSON Schema and/or XSD formats |
| [JSON Schema Transform](activity/schema-transform/jsonschematransform/) | 1.0.0 | Schema Transform | Transform JSON Schema to XSD and Avro formats |
| [XSD Schema Transform](activity/schema-transform/xsdschematransform/) | 1.0.0 | Schema Transform | Transform XSD schemas to JSON Schema and Avro formats |
| [Kafka Stream Filter](connectors/KafkaStream/activity/filter/) | 1.0.0 | Kafka Stream | Evaluate single or multi-predicate AND/OR chains against Kafka message fields; supports deduplication and rate limiting |
| [Kafka Stream Aggregate](connectors/KafkaStream/activity/aggregate/) | 1.0.0 | Kafka Stream | Accumulate a numeric message field into tumbling or sliding windows and emit sum/count/avg/min/max on window close |

### 🎯 Triggers

| Component | Version | Type | Description |
|-----------|---------|------|-------------|
| [PostgreSQL Listener](trigger/postgreslistener/) | 0.1.0 | Database | Listen for PostgreSQL NOTIFY messages on specified channels |
| [MySQL Binlog Listener](trigger/mysql-binlog-listener/) | 1.1.0 | Database | Real-time MySQL/MariaDB binlog streaming for change data capture with SSL/TLS support |
| [SSE Trigger](connectors/sse/trigger/) | 1.0.0 | Real-time | Server-Sent Events trigger for streaming data to web clients |
| [Kafka Stream Aggregate Trigger](connectors/KafkaStream/trigger/aggregate/) | 1.0.0 | Kafka Stream | Stateful windowed aggregation over a Kafka topic — fires on window close with sum/avg/count/min/max result |
| [Kafka Stream Filter Trigger](connectors/KafkaStream/trigger/filter/) | 1.0.0 | Kafka Stream | Fire a flow only for messages satisfying single or multi-predicate AND/OR conditions; supports deduplication and rate limiting |
| [Kafka Stream Join Trigger](connectors/KafkaStream/trigger/join/) | 1.0.0 | Kafka Stream | Stream-join across two or more Kafka topics — fires when messages with the same join key arrive from all topics within a configurable window |
| [Kafka Stream Split Trigger](connectors/KafkaStream/trigger/split/) | 1.0.0 | Kafka Stream | Content-based routing over a Kafka topic — routes each message to one or more handler branches via first-match or all-match predicate evaluation with priority ordering |

## 🚀 Quick Start

### Usage in Flogo
1. Navigate to the specific component directory
2. Copy the extension source code to your Flogo extensions directory
3. Configure the extension path in Flogo VSCode extension settings
4. Use the components in your Flogo flows

<img width="1466" height="824" alt="image" src="https://github.com/user-attachments/assets/f73ae2d0-9c79-418a-94dd-61993b1e46e4" />


## 📚 Examples

| Example | Components Used | Description |
|---------|----------------|-------------|
| [Schema Converter API](examples/schema_converter/) | Schema Transform Activities | REST API for schema conversions between JSON Schema, XSD, and Avro |
| [SSE Demo](examples/sse_connector/) | SSE Trigger and SSE Activity | Real-time data streaming demo with timer-based events and SSE broadcasting |
| [PostgreSQL Listener Demo](examples/postgrelistener/) | PostgreSQL Listener Trigger, Write Log Activity | Database change notification demo with NOTIFY/LISTEN and logging |
| [MySQL Binlog Listener Demo](examples/mysqllistener/) | MySQL Binlog Listener Trigger, Write Log Activity | Real-time MySQL/MariaDB binlog streaming demo for change data capture |
| [Universal Database Listener Demo](examples/universaldblistener/) | Universal Database Listener Trigger, Write Log Activity | Multi-database listener demo supporting PostgreSQL, MySQL, MariaDB with unified event handling |
| [AWS SQS Delete Demo](examples/aws_signature4/) | AWS Signature V4 Generator, REST Invoke Activity | AWS SQS message deletion demo using Signature V4 authentication |
| [Template Engine Demo](examples/template-engine/) | Template Engine Activity, Write Log Activity | Dynamic content generation demo using templates with timer-based processing |
| [Write Log Demo](examples/write_log/) | Write Log Activity | Efficient logging demonstration with various log levels and structured output |
| [Kafka Stream Demo](examples/kafka-stream/) | Kafka Stream Aggregate Trigger, Kafka Stream Filter Trigger, Kafka Stream Join Trigger, Kafka Stream Split Trigger | Filter hot sensor readings by temperature threshold, compute per-device averages over a tumbling time window, join readings with alert thresholds across two topics, and route messages to branches via content-based split |
| [Custom Functions Demo](examples/functions/) | All custom function packages | Timer-triggered flow exercising custom functions across math, array, string, util, datetime, number, and json packages |

## 🤝 Contributing

We welcome contributions! 


---

📧 **Contact:** For questions or support, please open an issue in this repository.

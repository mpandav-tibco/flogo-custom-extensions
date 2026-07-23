# Flogo Custom Extensions

This repository provides custom Flogo extensions including activities, triggers, and connectors for TIBCO Flogo applications.

## 📊 Component Overview

### 🔌 Connectors

| Component | Version | Type | Description |
|-----------|---------|------|-------------|
| [SSE Connector](connectors/sse/) | 1.0.0 | Connector | Server-Sent Events real-time streaming with event buffering and topic filtering |
| [Kafka Stream Connector](connectors/KafkaStream/) | 1.0.0 | Connector | Stateful windowed stream processing for Kafka messages — filtering, windowed aggregation, content-based routing, and event-time processing |
| [VectorDB — ActiveSpaces](connectors/VectorDB/activespaces/) | 1.0.0 | Connector | TIBCO ActiveSpaces 5.2 vector store — dual connectors: gateway (pure-Go, portable) and native (tibdg/CGO), sharing the 14-activity surface for RAG and agentic AI pipelines |
| [VectorDB — Qdrant](connectors/VectorDB/qdrant/) | 1.0.0 | Connector | Qdrant connector — high-performance ANN search via REST and gRPC, TLS support, purpose-built for RAG and agentic AI pipelines |
| [VectorDB — Weaviate](connectors/VectorDB/weaviate/) | 1.0.0 | Connector | Weaviate connector — native hybrid (BM25 + vector) search, GraphQL-backed, purpose-built for RAG pipelines |
| [VectorDB — Chroma](connectors/VectorDB/chroma/) | 1.0.0 | Connector | Chroma connector — lightweight embedding-first store via REST v2, purpose-built for RAG pipelines |
| [VectorDB — Milvus](connectors/VectorDB/milvus/) | 1.0.0 | Connector | Milvus connector — enterprise-grade, cloud-native, high-throughput gRPC-backed store for large-scale AI |
| [VectorDB — pgvector](connectors/VectorDB/pgvector/) | 1.0.0 | Connector | pgvector connector — PostgreSQL + pgvector extension; ACID guarantees, JSONB metadata, native full-text hybrid search |
| [VectorDB — Pinecone](connectors/VectorDB/pinecone/) | 1.0.0 | Connector | Pinecone connector — fully-managed serverless vector database with native sparse-dense hybrid search |
| [VectorDB — Redis](connectors/VectorDB/redis/) | 1.0.0 | Connector | Redis Stack connector — HNSW vector search + RediSearch BM25 hybrid, sub-millisecond latency |
| [VectorDB — Elasticsearch](connectors/VectorDB/elasticsearch/) | 1.0.0 | Connector | Elasticsearch 8.x connector — `dense_vector` HNSW with native k-NN + BM25 hybrid search |
| [VectorDB — OpenSearch](connectors/VectorDB/opensearch/) | 1.0.0 | Connector | OpenSearch 2.x connector — `knn_vector` HNSW with native k-NN + BM25 hybrid search |
| [VectorDB — Azure AI Search](connectors/VectorDB/azureaisearch/) | 1.0.0 | Connector | Azure AI Search connector — managed cloud vector search with RRF hybrid ranking |
| [VectorDB — LanceDB](connectors/VectorDB/lancedb/) | 1.0.0 | Connector | LanceDB connector — embedded columnar vector store with RRF hybrid (dense + FTS) via custom REST server |

> See [connectors/VectorDB/README.md](connectors/VectorDB/README.md) for the full feature matrix, Tier 1 vs Tier 2 comparison, and connector selection guide.

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
| [text](function/text/) | `cleanText` | Remove Apache Tika extraction artifacts (HTML entities, image alt-text, control characters, orphaned table-cell fragments, excessive blank lines) from plain text before embedding or indexing |


### ⚡ Activities

| Component | Version | Category | Description |
|-----------|---------|----------|-------------|
| [Write Log](activity/write-log/) | 1.0.0 | Logging | Logging with OpenTracing/OpenTelemetry integration and ECS compliance |
| [Template Engine](activity/templateengine/) | 1.0.0 | Content Generation | Dynamic content generation with Go templates, 29 built-in functions, and OOTB business templates |
| [AWS Signature V4 Generator](activity/awssignaturev4/) | 1.0.0 | AWS Integration | Generate AWS Signature Version 4 authentication headers for secure REST API calls to AWS services |
| [XML Filter](activity/xmlfilter/) | 0.1.0 | XML Processing | Filter XML content using XPath expressions with AND/OR logic support |
| [Avro Schema Transform](activity/schema-transform/avroschematransform/) | 1.0.0 | Schema Transform | Transform Avro schemas to JSON Schema and/or XSD formats |
| [JSON Schema Transform](activity/schema-transform/jsonschematransform/) | 1.0.0 | Schema Transform | Transform JSON Schema to XSD and Avro formats |
| [XSD Schema Transform](activity/schema-transform/xsdschematransform/) | 1.0.0 | Schema Transform | Transform XSD schemas to JSON Schema and Avro formats |
| [SOAP Client](activity/soapclient/) | 1.0.0 | Web Services | SOAP 1.1/1.2 client with WSDL support, JSON/XML modes, mutual TLS, WS-Security headers, OpenTelemetry tracing, and Flogo retry/circuit-breaker |
| [REST Fire & Forget](activity/rest-fire-forget/) | 1.0.0 | HTTP | Asynchronous fire-and-forget HTTP client — dispatches a request (any method) with mappable headers, query params, and JSON body, then returns immediately without waiting for the response; bounded concurrency and a hardened HTTP client |

### 🎯 Triggers

| Component | Version | Type | Description |
|-----------|---------|------|-------------|
| [PostgreSQL Listener](trigger/postgreslistener/) | 0.1.0 | Database | Listen for PostgreSQL NOTIFY messages on specified channels |
| [MySQL Binlog Listener](trigger/mysql-binlog-listener/) | 1.1.0 | Database | Real-time MySQL/MariaDB binlog streaming for change data capture with SSL/TLS support |
| [PostgreSQL CDC Listener](trigger/postgres-cdc-listener/) | 1.0.0 | Database | Native change data capture via logical replication (`pgoutput`) — streams INSERT/UPDATE/DELETE from the WAL with before/after row images, LSN, and XID |
| [MongoDB CDC Listener](trigger/mongodb-cdc-listener/) | 1.0.0 | Database | Change data capture via MongoDB change streams — inserts/updates/replaces/deletes with full/pre-images and resumable tokens |
| [SQL Server CDC Listener](trigger/sqlserver-cdc-listener/) | 1.0.0 | Database | Change data capture via SQL Server CDC change tables — LSN-cursor polling with before/after row images |
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
| [PostgreSQL CDC Demo](examples/postgres-cdc-listener/) | PostgreSQL CDC Listener Trigger, Log Activity | Logical-replication CDC demo — logs every captured INSERT/UPDATE/DELETE with typed row images, LSN, and XID |
| [MongoDB CDC Demo](examples/mongodb-cdc-listener/) | MongoDB CDC Listener Trigger, Log Activity | Change-stream CDC demo — logs captured document changes with full/pre-images and resume tokens |
| [SQL Server CDC Demo](examples/sqlserver-cdc-listener/) | SQL Server CDC Listener Trigger, Log Activity | CDC change-table demo — logs captured INSERT/UPDATE/DELETE with before/after row images |
| [AWS SQS Delete Demo](examples/aws_signature4/) | AWS Signature V4 Generator, REST Invoke Activity | AWS SQS message deletion demo using Signature V4 authentication |
| [Template Engine Demo](examples/template-engine/) | Template Engine Activity, Write Log Activity | Dynamic content generation demo using templates with timer-based processing |
| [Write Log Demo](examples/write_log/) | Write Log Activity | Efficient logging demonstration with various log levels and structured output |
| [Kafka Stream Demo](examples/kafka-stream/) | Kafka Stream Aggregate Trigger, Kafka Stream Filter Trigger, Kafka Stream Join Trigger, Kafka Stream Split Trigger | Filter hot sensor readings by temperature threshold, compute per-device averages over a tumbling time window, join readings with alert thresholds across two topics, and route messages to branches via content-based split |
| [Custom Functions Demo](examples/functions/) | All custom function packages | Timer-triggered flow exercising custom functions across math, array, string, util, datetime, number, and json packages |
| [SOAP Client Demo](examples/soap-client/) | SOAP Client Activity | Timer-triggered SOAP 1.1 call to a public calculator service in both JSON mode and XML mode with namespace attributes |
| [REST Fire & Forget Demo](examples/rest-fire-forget/) | REST Fire & Forget Activity | REST-triggered flow that dispatches a fire-and-forget HTTP POST to a bundled mock receiver service and returns immediately without waiting for the response |
| [VectorDB Demo](examples/vectordb/) | VectorDB Connectors (TIBCO ActiveSpaces, Qdrant, Weaviate, Chroma, Milvus, pgvector, Pinecone, Redis, Elasticsearch, OpenSearch, Azure AI Search, LanceDB) | End-to-end RAG demo across all 12 dedicated connectors: create collection, ingest documents, vector search, hybrid search, and full RAG query pipeline |

## 🤝 Contributing

We welcome contributions! 


---

📧 **Contact:** For questions or support, please open an issue in this repository.

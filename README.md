# Flogo Custom Extensions

This repository provides custom Flogo extensions including activities, triggers, and connectors for TIBCO Flogo applications.

## 📊 Component Overview

### 🔌 Connectors

| Component | Version | Type | Description |
|-----------|---------|------|-------------|
| [SSE Connector](sse/) | 1.0.0 | Connector | Server-Sent Events real-time streaming with event buffering and topic filtering |

### ⚡ Activities

| Component | Version | Category | Description |
|-----------|---------|----------|-------------|
| [Write Log](activity/write-log/) | 1.0.0 | Logging | Enterprise-grade logging with OpenTracing/OpenTelemetry integration and ECS compliance |
| [XML Filter](activity/xmlfilter/) | 0.1.0 | XML Processing | Filter XML content using XPath expressions with AND/OR logic support |
| [Avro Schema Transform](activity/schema-transform/avroschematransform/) | 1.0.0 | Schema Transform | Transform Avro schemas to JSON Schema and/or XSD formats |
| [JSON Schema Transform](activity/schema-transform/jsonschematransform/) | 1.0.0 | Schema Transform | Transform JSON Schema to XSD and Avro formats |
| [XSD Schema Transform](activity/schema-transform/xsdschematransform/) | 1.0.0 | Schema Transform | Transform XSD schemas to JSON Schema and Avro formats |

### 🎯 Triggers

| Component | Version | Type | Description |
|-----------|---------|------|-------------|
| [PostgreSQL Listener](trigger/postgreslistener/) | 0.1.0 | Database | Listen for PostgreSQL NOTIFY messages on specified channels |
| [MySQL Binlog Listener](trigger/mysql-binlog-listener/) | 1.1.0 | Database | Real-time MySQL/MariaDB binlog streaming for change data capture with SSL/TLS support |
| [SSE Trigger](sse/trigger/) | 1.0.0 | Real-time | Server-Sent Events trigger for streaming data to web clients |

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
| [Write Log Demo](examples/write_log/) | Write Log Activity | Efficient logging demonstration with various log levels and structured output |

## 🤝 Contributing

We welcome contributions! 


---

📧 **Contact:** For questions or support, please open an issue in this repository.

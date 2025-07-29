# Flogo Custom Extensions - Component Catalog

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
| [SSE Trigger](sse/trigger/) | 1.0.0 | Real-time | Server-Sent Events trigger for streaming data to web clients |

## 🚀 Quick Start

### Installation
```bash
git clone https://github.com/mpandav-tibco/flogo-custom-extensions.git
```

### Usage in Flogo
1. Navigate to the specific component directory
2. Copy the extension source code to your Flogo extensions directory
3. Configure the extension path in Flogo VSCode extension settings
4. Use the components in your Flogo flows

## 📚 Examples

| Example | Components Used | Description |
|---------|----------------|-------------|
| [Schema Converter API](examples/schema_converter/) | Schema Transform Activities | REST API for schema conversions between JSON Schema, XSD, and Avro |

## 🔧 Development Status

### Active Development
- SSE Connector (Real-time streaming features)
- Schema Transform Activities (Enhanced conversion support)

### Stable
- Write Log Activity
- XML Filter Activity
- PostgreSQL Listener Trigger

## 📖 Documentation

Each component includes:
- ✅ README with usage instructions
- ✅ Configuration examples
- ✅ API documentation
- ✅ Test cases

## 🤝 Contributing

We welcome contributions! Please see individual component directories for specific contribution guidelines.

## 📄 License

MIT License - see [LICENSE](LICENSE) file for details.

---

📧 **Contact:** For questions or support, please open an issue in this repository.

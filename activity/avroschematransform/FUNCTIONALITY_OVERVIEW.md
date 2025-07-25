# Avro Schema Transformer Activity - Functionality Overview

## Purpose and Function

The **Avro Schema Transformer Activity** is a specialized Flogo component designed to convert Apache Avro schemas into other widely-used schema formats. This activity bridges the gap between Avro's binary serialization format and more common schema representations used in REST APIs and XML-based systems.

## Core Functionality

### 1. **Schema Transformation Engine**
The activity serves as a powerful transformation engine that can convert Avro schemas to:
- **JSON Schema** (Draft 2020-12 compliant)
- **XSD (XML Schema Definition)**
- **Both formats simultaneously**

### 2. **Input Processing**
The activity accepts:
- **Avro Schema String**: The source Avro schema in JSON format
- **Output Format Selection**: Choose between "json", "xsd", or "both"
- **XML Configuration**: Root element name and target namespace for XSD generation (inputs only)

### 3. **Smart Processing Logic**
The activity intelligently processes inputs based on the selected output format:
- When `outputFormat = "json"`: Only JSON Schema generation is performed
- When `outputFormat = "xsd"`: Only XSD generation is performed, XML fields are processed
- When `outputFormat = "both"`: Both transformations are executed

## How It Works

### Step 1: Input Validation and Coercion
```go
// The activity first validates and coerces all inputs
input, err := coerceAndValidateInputs(ctx)
```
- Validates the Avro schema string is not empty
- Normalizes output format to lowercase
- Processes XML-related fields only when needed for XSD generation
- Applies default values (root element = "root" if not specified)

### Step 2: Avro Schema Parsing
```go
// Parse the Avro schema JSON
var avroSchema map[string]interface{}
json.Unmarshal([]byte(input.AvroSchemaString), &avroSchema)
```
- Parses the input Avro schema JSON string
- Validates the schema structure
- Prepares it for transformation processing

### Step 3: Format-Specific Transformation

#### JSON Schema Generation
```go
if outputFormat == "json" || outputFormat == "both" {
    jsonSchemaString, err = avroToJSONSchema(avroSchema)
}
```
- Converts Avro types to JSON Schema types
- Handles complex types (records, arrays, maps, unions)
- Generates Draft 2020-12 compliant JSON Schema
- Processes optional fields (union with null)

#### XSD Generation
```go
if outputFormat == "xsd" || outputFormat == "both" {
    xsdString, err = avroToXSD(avroSchema, input.RootElementName, input.TargetNamespace)
}
```
- Converts Avro types to XSD elements
- Creates complex types for Avro records
- Handles sequences, choices, and optional elements
- Generates valid XML Schema with proper namespaces

### Step 4: Output Generation
The activity produces:
- **Success Outputs**: Generated schemas (JSON/XSD based on format selection)
- **Error Handling**: Boolean error flag and detailed error messages
- **Clean Results**: Empty strings for non-requested formats

## Type Mapping System

### Primitive Types
| Avro Type | JSON Schema | XSD |
|-----------|-------------|-----|
| `null` | `null` | Optional element |
| `boolean` | `boolean` | `xs:boolean` |
| `int`/`long` | `integer` | `xs:integer` |
| `float`/`double` | `number` | `xs:decimal` |
| `bytes` | `string` | `xs:base64Binary` |
| `string` | `string` | `xs:string` |

### Complex Types
| Avro Type | JSON Schema | XSD |
|-----------|-------------|-----|
| **Record** | `object` with properties | `complexType` with sequence |
| **Array** | `array` with items | Element with `maxOccurs="unbounded"` |
| **Map** | `object` with `additionalProperties` | `xs:anyType` |
| **Enum** | `string` with `enum` constraint | `xs:string` |
| **Union** | `anyOf` or optional field | `xs:choice` or optional |

## Configuration Options

### Settings (Activity-Level)
- **outputFormat**: Default output format ("json", "xsd", "both")

### Inputs (Runtime-Configurable)
- **avroSchemaString**: The Avro schema to transform (required)
- **outputFormat**: Override default format (optional, default: "both")
- **rootElementName**: Root element name for XSD (optional, default: "root")
- **targetNamespace**: Target namespace for XSD (optional, default: "" - no namespace)

## Error Handling

The activity provides comprehensive error handling with specific error codes:

### Error Types
1. **INVALID_INPUT**: Missing or invalid input parameters
2. **SCHEMA_PARSE_ERROR**: Invalid Avro schema JSON
3. **JSON_CONVERSION_ERROR**: Error during JSON Schema generation
4. **XSD_CONVERSION_ERROR**: Error during XSD generation

### Error Response Structure
```json
{
  "error": true,
  "errorMessage": "Detailed error description with context",
  "jsonSchema": "",
  "xsdString": ""
}
```

## Usage Scenarios

### 1. API Integration
- Convert Avro schemas to JSON Schema for REST API documentation
- Generate OpenAPI/Swagger specifications from Avro schemas
- Validate JSON payloads against converted schemas

### 2. XML Processing
- Transform Avro schemas to XSD for XML validation
- Generate XML namespace definitions
- Create SOAP service definitions from Avro schemas

### 3. Schema Evolution
- Convert between different schema formats during system migrations
- Maintain schema compatibility across different systems
- Support polyglot persistence strategies

### 4. Documentation Generation
- Create human-readable schema documentation
- Generate schema comparison reports
- Support schema governance processes

## Performance Characteristics

### Efficiency Features
- **Smart Processing**: Only executes required transformations
- **Memory Efficient**: Processes schemas without unnecessary copying
- **Error Fast**: Fails quickly on invalid inputs
- **Stateless**: No state maintained between executions

### Limitations
- Complex union types are simplified in XSD due to XML Schema constraints
- Map types use `xs:anyType` in XSD (XML limitation)
- Large schemas may require increased memory allocation

## Integration Points

### Flogo Flow Integration
The activity integrates seamlessly with Flogo flows:
- Can be triggered by HTTP REST endpoints
- Supports conditional processing based on query parameters
- Outputs can be used by subsequent activities
- Error handling integrates with flow error management

### External System Integration
- **REST APIs**: Convert schemas for API gateway validation
- **Message Queues**: Transform schemas for message validation
- **Databases**: Generate schemas for document stores
- **ETL Processes**: Support data pipeline schema evolution

## Technical Architecture

### Dependencies
- **Flogo Core Framework**: v1.2.0 for activity lifecycle management
- **Go Standard Library**: encoding/json, encoding/xml for transformations
- **No External Dependencies**: Self-contained implementation

### Code Structure
- **activity.go**: Main transformation logic and activity implementation
- **descriptor.json**: Activity metadata and configuration schema
- **Input/Output Structs**: Type-safe data handling
- **Error Handling**: Comprehensive error management system

This activity provides a robust, production-ready solution for Avro schema transformation needs in enterprise integration scenarios.

# JSON Schema to XSD Activity

A high-performance Flogo activity that converts JSON Schema to XML Schema Definition (XSD) format.

## Overview

This activity transforms JSON Schema documents into equivalent XSD format, enabling interoperability between JSON-based and XML-based systems. The activity has been fully refactored to follow Flogo best practices and uses only standard Go libraries for optimal performance.

## Features

- **JSON Schema to XSD Conversion**: Transforms JSON Schema definitions into valid XSD format
- **Comprehensive Type Support**: Handles all basic JSON Schema types (object, array, string, number, integer, boolean, null)
- **Nested Objects**: Supports complex nested object structures
- **Array Support**: Converts JSON arrays to XSD elements with proper cardinality
- **Advanced Schema Patterns**: Supports anyOf, oneOf, and allOf composition patterns
- **Required Fields**: Properly handles required vs optional fields using minOccurs
- **Configurable Output**: Customizable root element name and target namespace
- **High Performance**: ~69,000 conversions per second with minimal memory footprint
- **Zero External Dependencies**: Uses only Go standard library and Flogo core

## Inputs

| Name | Type | Required | Description | Default |
|------|------|----------|-------------|---------|
| `jsonSchemaString` | string | Yes | JSON Schema as a string to be converted | - |
| `rootElementName` | string | No | Name for the root XSD element | "RootElement" |
| `targetNamespace` | string | No | Target namespace for the XSD schema | "" |

## Outputs

| Name | Type | Description |
|------|------|-------------|
| `xsdString` | string | Generated XSD as a string |
| `error` | boolean | Indicates if an error occurred during conversion |
| `errorMessage` | string | Error message if conversion failed |

## Usage Examples

### Basic Object Schema
**Input JSON Schema:**
```json
{
  "type": "object",
  "properties": {
    "name": {"type": "string"},
    "age": {"type": "integer"},
    "active": {"type": "boolean"}
  },
  "required": ["name"]
}
```

**Generated XSD:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<xs:schema elementFormDefault="qualified" xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="RootElement">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="name" type="xs:string"></xs:element>
        <xs:element name="age" type="xs:integer" minOccurs="0"></xs:element>
        <xs:element name="active" type="xs:boolean" minOccurs="0"></xs:element>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>
```

### Array Schema
**Input JSON Schema:**
```json
{
  "type": "object",
  "properties": {
    "items": {
      "type": "array",
      "items": {"type": "string"}
    }
  }
}
```

**Generated XSD:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<xs:schema elementFormDefault="qualified" xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="RootElement">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="items" type="xs:string" minOccurs="0" maxOccurs="unbounded"></xs:element>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>
```

## Type Mappings

| JSON Schema Type | XSD Type |
|------------------|----------|
| `string` | `xs:string` |
| `integer` | `xs:integer` |
| `number` | `xs:decimal` |
| `boolean` | `xs:boolean` |
| `null` | `xs:string` (with minOccurs="0") |
| `object` | `xs:complexType` with `xs:sequence` |
| `array` | Element with `maxOccurs="unbounded"` |

## Error Handling

The activity provides comprehensive error handling for:
- Invalid JSON Schema syntax
- Unsupported schema structures
- Non-object root schemas
- Arrays without item definitions
- Malformed input data


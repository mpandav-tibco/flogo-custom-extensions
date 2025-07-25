# Default Values Configuration

## Overview
The Avro Schema Transformer activity provides sensible default values for XML-related configuration options when users don't explicitly provide them.

## Default Value Behavior

### ✅ **rootElementName**
- **Default Value**: `"root"`
- **Applied When**: User doesn't provide the input OR provides empty/whitespace-only string
- **Implementation**: Configured in both `descriptor.json` and Go code
- **Example XSD Output**: `<xs:element name="root">`

### ✅ **targetNamespace** 
- **Default Value**: `""` (empty string, no namespace)
- **Applied When**: User doesn't provide the input
- **Implementation**: Configured in both `descriptor.json` and Go code  
- **Example XSD Output**: No `targetNamespace` attribute in schema element

## Implementation Details

### Descriptor.json Configuration
```json
{
  "name": "rootElementName",
  "type": "string",
  "required": false,
  "value": "root"
},
{
  "name": "targetNamespace", 
  "type": "string",
  "required": false,
  "value": ""
}
```

### Go Code Implementation
```go
// Get root element name with default fallback
input.RootElementName, err = coerce.ToString(ctx.GetInput(ivRootElementName))
if err != nil || strings.TrimSpace(input.RootElementName) == "" {
    input.RootElementName = "root" // Default root element name
}

// Get target namespace - optional field, defaults to empty
input.TargetNamespace, _ = coerce.ToString(ctx.GetInput(ivTargetNamespace))
```

## Example Usage

### Case 1: No XML Inputs Provided
```json
{
  "input": {
    "avroSchemaString": "{...}",
    "outputFormat": "xsd"
  }
}
```
**Result**: 
- `rootElementName` = `"root"`
- `targetNamespace` = `""` (no namespace)

### Case 2: Partial XML Inputs
```json
{
  "input": {
    "avroSchemaString": "{...}",
    "outputFormat": "xsd",
    "rootElementName": "Product"
  }
}
```
**Result**:
- `rootElementName` = `"Product"` (user override)
- `targetNamespace` = `""` (default)

### Case 3: Full XML Inputs
```json
{
  "input": {
    "avroSchemaString": "{...}",
    "outputFormat": "xsd", 
    "rootElementName": "Product",
    "targetNamespace": "http://example.com/product"
  }
}
```
**Result**:
- `rootElementName` = `"Product"` (user override)
- `targetNamespace` = `"http://example.com/product"` (user override)

## Benefits

1. **User-Friendly**: Users don't need to provide all configuration options
2. **Sensible Defaults**: The default `"root"` element name works for most cases
3. **Namespace Flexibility**: Empty namespace allows for namespace-free XSD schemas
4. **Consistent Behavior**: Same defaults in UI and runtime processing
5. **Override Support**: Users can still provide custom values when needed

## Testing

The default behavior is thoroughly tested in the test suite:
- `TestActivity_Eval/Transform_to_XSD_with_default_values` verifies default values work correctly
- Fresh test context ensures no input pollution from other tests
- Generated XSD is validated to contain expected default values

# XSD to JSON Schema Activity

A high-quality Flogo activity that converts XML Schema Definition (XSD) strings to JSON Schema format.

## Overview

This activity transforms XSD (XML Schema Definition) documents into equivalent JSON Schema representations, enabling interoperability between XML and JSON-based systems.


## Configuration

### Input
   
| Name | Type | Required | Description |
|------|------|----------|-------------|
| xsdString | string | Yes | The XSD string to convert to JSON Schema |

### Output

| Name | Type | Description |
|------|------|-------------|
| jsonSchemaString | string | The generated JSON Schema string |
| error | boolean | Indicates if an error occurred during conversion |
| errorMessage | string | Error details if conversion failed |

## Supported Features

This activity provides comprehensive XSD to JSON Schema conversion using **JSON Schema Draft 2020-12** (the latest specification).

### ✅ Core XSD Elements
- **Simple Elements** - Basic data types (string, integer, boolean, etc.)
- **Complex Types** - Nested objects with properties
- **Sequences** - Ordered element groups
- **Choice Elements** - Alternative selections (mapped to `oneOf`)
- **All Elements** - Unordered element groups
- **Groups** - Named group definitions with reference resolution
- **Arrays** - Elements with `maxOccurs="unbounded"`
- **Optional/Required Elements** - Based on `minOccurs` values

### ✅ Data Type Mapping
| XSD Type | JSON Schema Type | Format |
|----------|------------------|---------|
| string, normalizedString, token | string | - |
| date | string | date |
| dateTime | string | date-time |
| time | string | time |
| anyURI, duration | string | uri |
| base64Binary | string | byte |
| boolean | boolean | - |
| decimal, double, float | number | - |
| integer, long, int, short, byte | integer | - |

### ✅ Simple Type Restrictions
- **Pattern Validation** - `xs:pattern` → `pattern` (regex)
- **String Length** - `xs:minLength/maxLength` → `minLength/maxLength`
- **Enumerations** - `xs:enumeration` → `enum` (with type conversion)
- **Numeric Ranges** - `xs:minInclusive/maxInclusive` → `minimum/maximum`
- **Exclusive Ranges** - `xs:minExclusive/maxExclusive` → `exclusiveMinimum/exclusiveMaximum`
- **Multiple Patterns** - Combined using regex alternation

### ✅ Advanced Type Features
- **Named Simple Types** - Full type resolution and inheritance
- **Named Complex Types** - Reference resolution across schema
- **Union Types** - `xs:union` → `oneOf` with multiple alternatives  
- **List Types** - `xs:list` → arrays with typed items
- **Type Inheritance** - Complex and simple content extensions/restrictions
- **Default Values** - Element and attribute defaults with type conversion
- **Fixed Values** - Mapped to `const` + `default` in JSON Schema

### ✅ Attribute Support
- **Basic Attributes** - In simple content extensions
- **Required Attributes** - `use="required"` handling
- **Attribute Defaults** - Default and fixed values with type conversion

## Error Handling

The activity provides comprehensive error handling for common scenarios:

- **Invalid XSD**: Returns clear error message when XSD cannot be parsed
- **Missing Elements**: Handles empty or incomplete XSD structures gracefully
- **Type Resolution**: Reports issues with unresolved type references
- **Validation Errors**: Clear feedback on schema validation failures

All errors include context about the specific XSD element or structure that caused the issue.

### Error Codes

| Error Code | Description |
|------------|-------------|
| INVALID_INPUT | General input validation error |
| INVALID_INPUT_XSDString | XSD string is empty or contains only whitespace |
| XSD_PARSE_ERROR | Failed to parse the XSD XML |
| CONVERSION_ERROR | Failed to convert XSD structure to JSON Schema |
| JSON_MARSHAL_ERROR | Failed to generate JSON string output |

## Limitations

The following XSD features are **not yet supported** and may result in incomplete conversion:

### ❌ Schema Organization
- Schema imports (`xs:import`, `xs:include`)
- Namespace handling and qualified names
- Schema composition across multiple files

### ❌ Advanced Type Features  
- Substitution groups (`xs:substitutionGroup`)
- Abstract types and elements (`abstract="true"`)
- Block and final constraints (`block`, `final` attributes)

### ❌ Identity and Referential Constraints
- Identity constraints (`xs:key`, `xs:keyref`, `xs:unique`)
- Cross-element validation rules

### ❌ Advanced Attribute Features
- Attribute groups (`xs:attributeGroup`)
- Local attribute declarations with complex constraints
- Wildcard attributes (`xs:anyAttribute`)

### ❌ Content Model Extensions
- Mixed content models (`mixed="true"`)
- Any elements (`xs:any`)
- Redefine constructs (`xs:redefine`)

### ❌ Facet Limitations
- `xs:totalDigits` and `xs:fractionDigits` are mapped to custom properties (`x-totalDigits`, `x-fractionDigits`) since JSON Schema doesn't have direct equivalents

> **Note**: Complex type inheritance patterns may be simplified in the JSON Schema output compared to the original XSD structure.

## Usage Examples

### Example 1: Simple String Element

**Input XSD:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="person" type="xs:string"/>
</xs:schema>
```

**Output JSON Schema:**
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "string"
}
```

### Example 2: Complex Object with Multiple Properties

**Input XSD:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="person">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="name" type="xs:string"/>
        <xs:element name="age" type="xs:integer"/>
        <xs:element name="email" type="xs:string" minOccurs="0"/>
        <xs:element name="birthDate" type="xs:date"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>
```

**Output JSON Schema:**
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "name": {
      "type": "string"
    },
    "age": {
      "type": "integer"
    },
    "email": {
      "type": "string"
    },
    "birthDate": {
      "type": "string",
      "format": "date"
    }
  },
  "required": ["name", "age", "birthDate"]
}
```

### Example 3: Array Elements (Unbounded)

**Input XSD:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="items" type="xs:string" maxOccurs="unbounded"/>
</xs:schema>
```

**Output JSON Schema:**
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "array",
  "items": {
    "type": "string"
  }
}
```

### Example 4: XSD Choice Elements

**Input XSD:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="contact">
    <xs:complexType>
      <xs:choice>
        <xs:element name="email" type="xs:string"/>
        <xs:element name="phone" type="xs:string"/>
        <xs:element name="address" type="xs:string"/>
      </xs:choice>
    </xs:complexType>
  </xs:element>
</xs:schema>
```

**Output JSON Schema:**
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "oneOf": [
    {
      "type": "object",
      "properties": {
        "email": {
          "type": "string"
        }
      },
      "required": ["email"],
      "additionalProperties": false
    },
    {
      "type": "object", 
      "properties": {
        "phone": {
          "type": "string"
        }
      },
      "required": ["phone"],
      "additionalProperties": false
    },
    {
      "type": "object",
      "properties": {
        "address": {
          "type": "string"
        }
      },
      "required": ["address"],
      "additionalProperties": false
    }
  ]
}
```

### Example 5: Choice Arrays (Multiple Occurrences)

**Input XSD:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="messages">
    <xs:complexType>
      <xs:choice maxOccurs="unbounded">
        <xs:element name="text" type="xs:string"/>
        <xs:element name="image" type="xs:string"/>
      </xs:choice>
    </xs:complexType>
  </xs:element>
</xs:schema>
```

**Output JSON Schema:**
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "array",
  "items": {
    "oneOf": [
      {
        "type": "object",
        "properties": {
          "text": {
            "type": "string"
          }
        },
        "required": ["text"],
        "additionalProperties": false
      },
      {
        "type": "object",
        "properties": {
          "image": {
            "type": "string"
          }
        },
        "required": ["image"], 
        "additionalProperties": false
      }
    ]
  }
}
```

### Example 6: XSD All Elements

**Input XSD:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="person">
    <xs:complexType>
      <xs:all>
        <xs:element name="name" type="xs:string"/>
        <xs:element name="age" type="xs:integer"/>
        <xs:element name="email" type="xs:string" minOccurs="0"/>
        <xs:element name="address" type="xs:string"/>
      </xs:all>
    </xs:complexType>
  </xs:element>
</xs:schema>
```

**Output JSON Schema:**
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "name": {
      "type": "string"
    },
    "age": {
      "type": "integer"
    },
    "email": {
      "type": "string"
    },
    "address": {
      "type": "string"
    }
  },
  "required": ["name", "age", "address"],
  "additionalProperties": false
}
```

### Example 7: Advanced Mixed Constructs

**Input XSD (Combining Sequence, Choice, and All):**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="employee">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="personalInfo">
          <xs:complexType>
            <xs:all>
              <xs:element name="firstName" type="xs:string"/>
              <xs:element name="lastName" type="xs:string"/>
              <xs:element name="middleName" type="xs:string" minOccurs="0"/>
            </xs:all>
          </xs:complexType>
        </xs:element>
        <xs:element name="contact">
          <xs:complexType>
            <xs:choice>
              <xs:element name="email" type="xs:string"/>
              <xs:element name="phone" type="xs:string"/>
            </xs:choice>
          </xs:complexType>
        </xs:element>
        <xs:element name="skills" type="xs:string" maxOccurs="unbounded"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>
```

This example demonstrates the full power of the enhanced XSD to JSON Schema converter, supporting nested combinations of sequence, choice, and all constructs.



### Example 8: XSD Groups with References

**Input XSD (Named Groups and References):**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:group name="personGroup">
    <xs:sequence>
      <xs:element name="firstName" type="xs:string"/>
      <xs:element name="lastName" type="xs:string"/>
    </xs:sequence>
  </xs:group>
  
  <xs:group name="addressGroup">
    <xs:sequence>
      <xs:element name="street" type="xs:string"/>
      <xs:element name="city" type="xs:string"/>
    </xs:sequence>
  </xs:group>
  
  <xs:element name="customer">
    <xs:complexType>
      <xs:sequence>
        <xs:group ref="personGroup"/>
        <xs:group ref="addressGroup"/>
        <xs:element name="customerId" type="xs:integer"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>
```

**Output JSON Schema (Groups Flattened):**
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "firstName": {
      "type": "string"
    },
    "lastName": {
      "type": "string"
    },
    "street": {
      "type": "string"
    },
    "city": {
      "type": "string"
    },
    "customerId": {
      "type": "integer"
    }
  },
  "required": ["firstName", "lastName", "street", "city", "customerId"]
}
```

**Key Features Demonstrated:**
- **Named Groups**: `personGroup` and `addressGroup` are defined once and referenced multiple times
- **Group Flattening**: Group references are resolved and elements are flattened into the sequence
- **Clean Output**: No nested wrapper objects, resulting in clean and intuitive JSON Schema

### Example 9: XSD Simple Restrictions - Pattern and Length

**Input XSD:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="user">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="username">
          <xs:simpleType>
            <xs:restriction base="xs:string">
              <xs:pattern value="[a-zA-Z0-9_]+"/>
              <xs:minLength value="3"/>
              <xs:maxLength value="20"/>
            </xs:restriction>
          </xs:simpleType>
        </xs:element>
        <xs:element name="email">
          <xs:simpleType>
            <xs:restriction base="xs:string">
              <xs:pattern value="[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}"/>
            </xs:restriction>
          </xs:simpleType>
        </xs:element>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>
```

**Output JSON Schema:**
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "username": {
      "type": "string",
      "pattern": "[a-zA-Z0-9_]+",
      "minLength": 3,
      "maxLength": 20
    },
    "email": {
      "type": "string",
      "pattern": "[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}"
    }
  },
  "required": ["username", "email"]
}
```

### Example 10: XSD Enumeration and Numeric Restrictions

**Input XSD:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="product">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="category">
          <xs:simpleType>
            <xs:restriction base="xs:string">
              <xs:enumeration value="electronics"/>
              <xs:enumeration value="clothing"/>
              <xs:enumeration value="books"/>
            </xs:restriction>
          </xs:simpleType>
        </xs:element>
        <xs:element name="price">
          <xs:simpleType>
            <xs:restriction base="xs:decimal">
              <xs:minInclusive value="0.01"/>
              <xs:maxInclusive value="9999.99"/>
              <xs:totalDigits value="7"/>
              <xs:fractionDigits value="2"/>
            </xs:restriction>
          </xs:simpleType>
        </xs:element>
        <xs:element name="rating">
          <xs:simpleType>
            <xs:restriction base="xs:integer">
              <xs:minInclusive value="1"/>
              <xs:maxInclusive value="5"/>
            </xs:restriction>
          </xs:simpleType>
        </xs:element>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>
```

**Output JSON Schema:**

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "category": {
      "type": "string",
      "enum": ["electronics", "clothing", "books"]
    },
    "price": {
      "type": "number",
      "minimum": 0.01,
      "maximum": 9999.99,
      "x-totalDigits": 7,
      "x-fractionDigits": 2
    },
    "rating": {
      "type": "integer",
      "minimum": 1,
      "maximum": 5
    }
  },
  "required": ["category", "price", "rating"]
}
```

### Example 11: XSD Multiple Patterns and Exclusive Ranges

**Input XSD:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="contact">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="phoneOrEmail">
          <xs:simpleType>
            <xs:restriction base="xs:string">
              <xs:pattern value="[0-9]{3}-[0-9]{3}-[0-9]{4}"/>
              <xs:pattern value="[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}"/>
            </xs:restriction>
          </xs:simpleType>
        </xs:element>
        <xs:element name="temperature">
          <xs:simpleType>
            <xs:restriction base="xs:decimal">
              <xs:minExclusive value="-273.15"/>
              <xs:maxExclusive value="1000.0"/>
            </xs:restriction>
          </xs:simpleType>
        </xs:element>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>
```

**Output JSON Schema:**
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "phoneOrEmail": {
      "type": "string",
      "pattern": "([0-9]{3}-[0-9]{3}-[0-9]{4})|([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,})"
    },
    "temperature": {
      "type": "number",
      "exclusiveMinimum": -273.15,
      "exclusiveMaximum": 1000.0
    }
  },
  "required": ["phoneOrEmail", "temperature"]
}
```

### Example 12: Named Simple Types and Union Types

**Input XSD:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:simpleType name="SSNType">
    <xs:restriction base="xs:string">
      <xs:pattern value="[0-9]{3}-[0-9]{2}-[0-9]{4}"/>
    </xs:restriction>
  </xs:simpleType>
  
  <xs:simpleType name="IDType">
    <xs:union memberTypes="xs:integer SSNType"/>
  </xs:simpleType>
  
  <xs:element name="person">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="identifier" type="IDType"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>
```

**Output JSON Schema:**
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "identifier": {
      "oneOf": [
        {"type": "integer"},
        {"type": "string", "pattern": "[0-9]{3}-[0-9]{2}-[0-9]{4}"}
      ]
    }
  },
  "required": ["identifier"]
}
```

### Example 13: Named Complex Types and List Types

**Input XSD:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:complexType name="PersonType">
    <xs:sequence>
      <xs:element name="name" type="xs:string"/>
      <xs:element name="age" type="xs:integer"/>
    </xs:sequence>
  </xs:complexType>
  
  <xs:simpleType name="NumberList">
    <xs:list itemType="xs:integer"/>
  </xs:simpleType>
  
  <xs:element name="team">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="leader" type="PersonType"/>
        <xs:element name="scores" type="NumberList"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>
```

**Output JSON Schema:**
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "leader": {
      "type": "object",
      "properties": {
        "name": {"type": "string"},
        "age": {"type": "integer"}
      },
      "required": ["name", "age"]
    },
    "scores": {
      "type": "array",
      "items": {"type": "integer"}
    }
  },
  "required": ["leader", "scores"]
}
```

### Example 14: Complex Content Extension and Simple Content Extension

**Input XSD:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:complexType name="BasePersonType">
    <xs:sequence>
      <xs:element name="firstName" type="xs:string"/>
      <xs:element name="lastName" type="xs:string"/>
    </xs:sequence>
  </xs:complexType>
  
  <xs:complexType name="EmployeeType">
    <xs:complexContent>
      <xs:extension base="BasePersonType">
        <xs:sequence>
          <xs:element name="employeeId" type="xs:integer"/>
          <xs:element name="department" type="xs:string"/>
        </xs:sequence>
      </xs:extension>
    </xs:complexContent>
  </xs:complexType>
  
  <xs:complexType name="PriceType">
    <xs:simpleContent>
      <xs:extension base="xs:decimal">
        <xs:attribute name="currency" type="xs:string"/>
      </xs:extension>
    </xs:simpleContent>
  </xs:complexType>
  
  <xs:element name="employee" type="EmployeeType"/>
  <xs:element name="price" type="PriceType"/>
</xs:schema>
```

**Output JSON Schema:**
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "employee": {
      "type": "object",
      "properties": {
        "firstName": {"type": "string"},
        "lastName": {"type": "string"},
        "employeeId": {"type": "integer"},
        "department": {"type": "string"}
      },
      "required": ["firstName", "lastName", "employeeId", "department"]
    },
    "price": {
      "type": "number"
    }
  },
  },
  "required": ["employee", "price"]
}
```

### Example 15: Default and Fixed Values with Enhanced Attributes

**Input XSD:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="configuration">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="setting" default="default-value" type="xs:string"/>
        <xs:element name="mode" fixed="production" type="xs:string"/>
        <xs:element name="timeout" default="30" type="xs:integer"/>
        <xs:element name="enabled" default="true" type="xs:boolean"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>
```

**Output JSON Schema:**
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "setting": {
      "type": "string",
      "default": "default-value"
    },
    "mode": {
      "type": "string",
      "const": "production",
      "default": "production"
    },
    "timeout": {
      "type": "integer",
      "default": 30
    },  
    "enabled": {
      "type": "boolean",
      "default": true
    }
  },
  "required": ["setting", "mode", "timeout", "enabled"]
}
```


## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Ensure all tests pass (`go test -v`)
4. Commit your changes (`git commit -m 'Add amazing feature'`)
5. Push to the branch (`git push origin feature/amazing-feature`)
6. Open a Pull Request


## Support

For issues, questions, or contributions, please open an issue on the GitHub repository.

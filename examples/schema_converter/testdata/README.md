# Enterprise XSD -> OpenAPI test data

`EnterpriseOrder.xsd` is an enterprise-style purchase-order schema used to validate the
`xsdschematransform` activity's `outputFormat=openapi` restriction mapping (see
[activity/schema-transform/xsdschematransform/README.md](../../../activity/schema-transform/xsdschematransform/README.md)).

It exercises the most common XSD restriction facets found in real-world enterprise schemas:

| XSD construct | Element(s) in this schema |
|---|---|
| `xs:pattern` (single) | `orderId`, `customer/customerId`, `items/sku` |
| `xs:pattern` (multiple, OR union) | `promoCode` |
| `xs:enumeration` | `currency`, `status`, `region` |
| `xs:enumeration` + `xs:default` | `status` (default `PENDING`) |
| `xs:minLength` / `xs:maxLength` | `customer/name`, `notes`, `tags` list items |
| `xs:minInclusive` / `xs:maxInclusive` | `customer/creditLimit`, `items/quantity`, shipping fields |
| `xs:minExclusive` / `xs:maxExclusive` | `discountPercent` |
| `xs:minExclusive` (no max) | `items/unitPrice` |
| `xs:totalDigits` / `xs:fractionDigits` | `totalAmount`, `customer/creditLimit`, `items/unitPrice`, `expressShipping/surcharge` |
| bounded `minOccurs`/`maxOccurs` | `auditInfo/approvedBy` (optional) |
| unbounded `maxOccurs` (array) | `items` |
| `nillable` | `notes` |
| `xs:choice` | `standardShipping` \| `expressShipping` |
| `xs:list` | `tags` |
| `xs:all` | `auditInfo` |
| attribute `use="required"` + `xs:fixed` | `version` |
| attribute `use="optional"` + inline enumeration | `region` |

## Known limitations

- Recursive named types (a complexType that directly or indirectly references itself, e.g. a tree
  node with a child of its own type) are truncated at the cycle with an opaque `{"type":"object"}`
  rather than expanded again — the universal schema model has no `$ref`/pointer mechanism, so
  expanding a genuine cycle would recurse until the stack overflowed.
- `xs:complexContent`/`xs:restriction` (as opposed to `xs:extension`) does not merge the base
  type's members; only the restriction's own redeclared content is used.
- `additionalProperties` wildcard detection (`strictAdditionalProperties=true`) only inspects the
  complex type's own immediate `xs:any`/`xs:anyAttribute`, not ones nested inside a
  `simpleContent`/`complexContent` extension or restriction.

## Request format

`POST /xsd2openapi/v1` expects a JSON body with the raw XSD text under `xsd`, and optionally an
`additionalSchemas` array for any `xs:include`/`xs:import`-referenced files:

```json
{ "xsd": "<?xml version=\"1.0\"...?><xs:schema>...</xs:schema>", "additionalSchemas": ["<...>"] }
```

## Multi-file schemas (xs:include / xs:import): EnterpriseOrderWithIncludes.xsd

Real enterprise schemas are frequently split across files, with shared domain types (address,
contact, audit metadata, ...) factored into a common file and pulled in via `xs:include`/
`xs:import`. `EnterpriseOrderWithIncludes.xsd` + `EnterpriseOrderCommonTypes.xsd` demonstrate this:
the order schema declares `shipTo`/`billTo`/`contact`/`auditInfo` as named types (`AddressType`,
`ContactType`, `AuditInfoType`) that only exist in the separate common-types file. The activity
never dereferences `schemaLocation` itself (SSRF/path-traversal risk) — the referenced file's
content must be supplied via `additionalSchemas`:

```bash
python3 - <<'EOF'
import json
main = open("EnterpriseOrderWithIncludes.xsd").read()
common = open("EnterpriseOrderCommonTypes.xsd").read()
json.dump({"xsd": main, "additionalSchemas": [common]}, open("/tmp/req.json", "w"))
EOF
curl -s -X POST http://localhost:9999/xsd2openapi/v1 -H "Content-Type: application/json" \
  --data @/tmp/req.json | jq -r '.openApiSchemaString' | jq '.components.schemas.RootSchema.properties.PurchaseOrder.properties'
```

Without `additionalSchemas`, `shipTo`/`billTo`/`contact`/`auditInfo` fall back to a generic
`{"type":"string"}` (the named type is unresolvable); with it, each expands to its full object
structure from the common-types file.

## Try it against the running app

```bash
jq -Rs '{xsd:.}' EnterpriseOrder.xsd | curl -s -X POST http://localhost:9999/xsd2openapi/v1 \
  -H "Content-Type: application/json" --data-binary @- | jq -r '.openApiSchemaString' | jq .
```

## Response envelope

The activity always returns the same envelope regardless of `outputFormat`; only the field for
the requested format is populated (others are empty strings):

```json
{
  "jsonSchemaString": "",
  "avroSchemaString": "",
  "openApiSchemaString": "{...OpenAPI document as a JSON-encoded string...}",
  "validationResult": "",
  "conversionStats": "{\"elementsProcessed\":23,\"attributesProcessed\":0,\"complexTypesFound\":5,\"simpleTypesFound\":19,\"choicesFound\":0,\"unionsCreated\":0,\"constraintsApplied\":0,\"namespacesFound\":null,\"typeMapping\":{},\"warnings\":[]}",
  "error": false,
  "errorMessage": ""
}
```

`openApiSchemaString` must be parsed a second time (it's a JSON string, not nested JSON) to get
the actual OpenAPI document. Verified output for `EnterpriseOrder.xsd` (root object only, `openapi: "3.1.0"`):

```jsonc
{
  "openapi": "3.1.0",
  "info": { "title": "RootSchema", "version": "1.0.0" },
  "paths": {},
  "components": {
    "schemas": {
      "RootSchema": {
        "type": "object",
        // the XSD's single top-level element nests one level inside the synthetic root wrapper
        "properties": {
          "PurchaseOrder": {
            "type": "object",
            "properties": {
              "orderId": { "type": "string", "pattern": "ORD-[0-9]{6}" },
              "customer": {
                "type": "object",
                "properties": {
                  "customerId": { "type": "string", "pattern": "CUST-[0-9]{8}" },
                  "name": { "type": "string", "minLength": 1, "maxLength": 120 },
                  "creditLimit": {
                    "type": "number", "format": "decimal",
                    "minimum": 0, "maximum": 1000000, "multipleOf": 0.01,
                    "x-xsdTotalDigits": 10, "x-xsdFractionDigits": 2
                  }
                }
              },
              "items": {
                "type": "array",
                "minItems": 1,
                "items": {
                  "type": "object",
                  "properties": {
                    "sku": { "type": "string", "pattern": "SKU-[A-Z0-9]{8}" },
                    "quantity": { "type": "integer", "minimum": 1, "maximum": 9999 },
                    "unitPrice": {
                      "type": "number", "format": "decimal",
                      "exclusiveMinimum": 0, "multipleOf": 0.01,
                      "x-xsdTotalDigits": 10, "x-xsdFractionDigits": 2
                    }
                  }
                }
              },
              "currency": { "type": "string", "enum": ["USD", "EUR", "GBP"] },
              "status": {
                "type": "string",
                "enum": ["PENDING", "APPROVED", "SHIPPED", "CANCELLED"],
                "default": "PENDING"
              },
              "discountPercent": { "type": "number", "format": "decimal", "exclusiveMinimum": 0, "exclusiveMaximum": 100 },
              "promoCode": { "type": "string", "pattern": "(?:PROMO-[A-Z]{4})|(?:VIP-[0-9]{4})" },
              "notes": { "type": ["string", "null"], "minLength": 0, "maxLength": 500 },
              "tags": { "type": "array", "items": { "type": "string", "minLength": 1, "maxLength": 20 } },
              "auditInfo": { "type": "object", "properties": { "createdBy": { "type": "string" }, "approvedBy": { "type": "string" } } },
              "version": { "type": "string", "enum": ["1.0"], "default": "1.0" },
              "region": { "type": "string", "enum": ["NA", "EMEA", "APAC"] }
            },
            "required": ["version"],
            "oneOf": [
              { "type": "object", "properties": { "standardShipping": { "type": "object", "properties": { "carrier": { "type": "string" }, "estimatedDays": { "type": "integer", "minimum": 3, "maximum": 10 } } } } },
              { "type": "object", "properties": { "expressShipping": { "type": "object", "properties": { "carrier": { "type": "string" }, "guaranteedByHour": { "type": "integer", "minimum": 1, "maximum": 48 }, "surcharge": { "type": "number", "format": "decimal", "minimum": 0, "multipleOf": 0.01, "x-xsdTotalDigits": 8, "x-xsdFractionDigits": 2 } } } } }
            ]
          }
        }
      }
    }
  }
}
```

Notes on the numbers above (`elementsProcessed: 23`, etc.) come from `conversionStats` for this
exact schema and will change if the XSD is edited.


Or via the existing `/xsd2json/v1` endpoint to compare the JSON Schema output for the same input.

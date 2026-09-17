package xsdschematransform

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/project-flogo/core/support/test"
	"github.com/stretchr/testify/assert"
)

// runOpenAPI is a small helper shared by the tests below: it evaluates the activity with
// outputFormat=openapi and returns the parsed OpenAPI document.
func runOpenAPI(t *testing.T, xsd string, extraInputs map[string]interface{}) map[string]interface{} {
	t.Helper()
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())
	tc.SetInput(ivXSDString, xsd)
	tc.SetInput(ivOutputFormat, "openapi")
	for k, v := range extraInputs {
		tc.SetInput(k, v)
	}

	done, err := act.Eval(tc)
	assert.True(t, done)
	assert.NoError(t, err)
	assert.False(t, tc.GetOutput(ovError).(bool), tc.GetOutput(ovErrorMessage))

	var doc map[string]interface{}
	assert.NoError(t, json.Unmarshal([]byte(tc.GetOutput(ovOpenAPISchemaString).(string)), &doc))
	return doc
}

// TestNamedComplexTypeResolution verifies an element referencing a named <xs:complexType> by
// type="..." has that type's own properties expanded, instead of falling back to a generic type.
func TestNamedComplexTypeResolution(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:tns="http://example.com/x" targetNamespace="http://example.com/x">
  <xs:element name="customer" type="tns:CustomerType"/>
  <xs:complexType name="CustomerType">
    <xs:sequence>
      <xs:element name="name" type="xs:string"/>
      <xs:element name="age" type="xs:int"/>
    </xs:sequence>
  </xs:complexType>
</xs:schema>`

	doc := runOpenAPI(t, xsd, nil)
	root := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})["RootSchema"].(map[string]interface{})
	customer := root["properties"].(map[string]interface{})["customer"].(map[string]interface{})
	assert.Equal(t, "object", customer["type"])
	props := customer["properties"].(map[string]interface{})
	assert.Equal(t, "string", props["name"].(map[string]interface{})["type"])
	assert.Equal(t, "integer", props["age"].(map[string]interface{})["type"])
}

// TestNamedSimpleTypeResolution verifies an element referencing a named <xs:simpleType> has its
// restriction facets (here, an enumeration) applied instead of defaulting to a bare string.
func TestNamedSimpleTypeResolution(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:tns="http://example.com/x">
  <xs:element name="status" type="tns:StatusType"/>
  <xs:simpleType name="StatusType">
    <xs:restriction base="xs:string">
      <xs:enumeration value="ACTIVE"/>
      <xs:enumeration value="INACTIVE"/>
    </xs:restriction>
  </xs:simpleType>
</xs:schema>`

	doc := runOpenAPI(t, xsd, nil)
	root := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})["RootSchema"].(map[string]interface{})
	status := root["properties"].(map[string]interface{})["status"].(map[string]interface{})
	assert.ElementsMatch(t, []interface{}{"ACTIVE", "INACTIVE"}, status["enum"])
}

// TestSelfReferentialNamedTypeDoesNotRecurseForever verifies a directly self-referential named
// complexType (e.g. a tree/linked-list node) is truncated at the cycle point with an opaque object
// instead of recursing until the stack overflows, since the universal model has no $ref mechanism.
func TestSelfReferentialNamedTypeDoesNotRecurseForever(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:tns="http://example.com/x">
  <xs:element name="node" type="tns:NodeType"/>
  <xs:complexType name="NodeType">
    <xs:sequence>
      <xs:element name="value" type="xs:string"/>
      <xs:element name="child" type="tns:NodeType" minOccurs="0"/>
    </xs:sequence>
  </xs:complexType>
</xs:schema>`

	doc := runOpenAPI(t, xsd, nil)
	root := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})["RootSchema"].(map[string]interface{})
	node := root["properties"].(map[string]interface{})["node"].(map[string]interface{})
	props := node["properties"].(map[string]interface{})
	assert.Equal(t, "string", props["value"].(map[string]interface{})["type"])

	child := props["child"].(map[string]interface{})
	assert.Equal(t, "object", child["type"])
	assert.Nil(t, child["properties"]) // truncated at the cycle, not expanded again
}

// TestXSDListInlineItemSchemaPreserved verifies xs:list with an inline (anonymous) item simpleType
// keeps its item schema instead of losing it (the previously documented "xs:list drops item schema" gap).
func TestXSDListInlineItemSchemaPreserved(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="tags">
    <xs:simpleType>
      <xs:list>
        <xs:simpleType>
          <xs:restriction base="xs:string">
            <xs:minLength value="1"/>
            <xs:maxLength value="20"/>
          </xs:restriction>
        </xs:simpleType>
      </xs:list>
    </xs:simpleType>
  </xs:element>
</xs:schema>`

	doc := runOpenAPI(t, xsd, nil)
	root := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})["RootSchema"].(map[string]interface{})
	tags := root["properties"].(map[string]interface{})["tags"].(map[string]interface{})
	assert.Equal(t, "array", tags["type"])
	items := tags["items"].(map[string]interface{})
	assert.Equal(t, "string", items["type"])
	assert.Equal(t, float64(1), items["minLength"])
	assert.Equal(t, float64(20), items["maxLength"])
}

// TestXSDUnionInlineMembersPreserved verifies xs:union with inline (anonymous) member simpleTypes
// produces a oneOf branch per member, not just for named memberTypes references.
func TestXSDUnionInlineMembersPreserved(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="tier">
    <xs:simpleType>
      <xs:union>
        <xs:simpleType>
          <xs:restriction base="xs:string">
            <xs:enumeration value="GOLD"/>
            <xs:enumeration value="SILVER"/>
          </xs:restriction>
        </xs:simpleType>
        <xs:simpleType>
          <xs:restriction base="xs:integer">
            <xs:minInclusive value="0"/>
            <xs:maxInclusive value="100"/>
          </xs:restriction>
        </xs:simpleType>
      </xs:union>
    </xs:simpleType>
  </xs:element>
</xs:schema>`

	doc := runOpenAPI(t, xsd, nil)
	root := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})["RootSchema"].(map[string]interface{})
	tier := root["properties"].(map[string]interface{})["tier"].(map[string]interface{})
	oneOf := tier["oneOf"].([]interface{})
	assert.Len(t, oneOf, 2)
	assert.Equal(t, "string", oneOf[0].(map[string]interface{})["type"])
	assert.Equal(t, "integer", oneOf[1].(map[string]interface{})["type"])
}

// TestComplexContentExtensionInheritsBaseProperties verifies xs:complexContent/xs:extension merges
// the base type's own members with the new content, instead of silently dropping the base type
// (the previously documented "simplified implementation" gap).
func TestComplexContentExtensionInheritsBaseProperties(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:tns="http://example.com/x">
  <xs:element name="employee" type="tns:EmployeeType"/>
  <xs:complexType name="PersonType">
    <xs:sequence>
      <xs:element name="name" type="xs:string"/>
    </xs:sequence>
  </xs:complexType>
  <xs:complexType name="EmployeeType">
    <xs:complexContent>
      <xs:extension base="tns:PersonType">
        <xs:sequence>
          <xs:element name="employeeId" type="xs:string"/>
        </xs:sequence>
      </xs:extension>
    </xs:complexContent>
  </xs:complexType>
</xs:schema>`

	doc := runOpenAPI(t, xsd, nil)
	root := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})["RootSchema"].(map[string]interface{})
	employee := root["properties"].(map[string]interface{})["employee"].(map[string]interface{})
	props := employee["properties"].(map[string]interface{})
	assert.Contains(t, props, "name") // inherited from PersonType
	assert.Contains(t, props, "employeeId")
}

// TestStrictAdditionalPropertiesOptIn verifies additionalProperties is left untouched by default
// (existing behavior preserved) and only set to false/true (closed/open) when the caller opts in
// via strictAdditionalProperties.
func TestStrictAdditionalPropertiesOptIn(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="closed">
    <xs:complexType>
      <xs:sequence><xs:element name="a" type="xs:string"/></xs:sequence>
    </xs:complexType>
  </xs:element>
  <xs:element name="open">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="b" type="xs:string"/>
        <xs:any minOccurs="0"/>
      </xs:sequence>
      <xs:anyAttribute/>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	// Default: unchanged, permissive (no additionalProperties key at all).
	doc := runOpenAPI(t, xsd, nil)
	root := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})["RootSchema"].(map[string]interface{})
	props := root["properties"].(map[string]interface{})
	assert.Nil(t, props["closed"].(map[string]interface{})["additionalProperties"])
	assert.Nil(t, props["open"].(map[string]interface{})["additionalProperties"])

	// Opt-in strict mode: closed content -> false, xs:any/xs:anyAttribute wildcard -> true.
	strictDoc := runOpenAPI(t, xsd, map[string]interface{}{ivStrictAdditionalProperties: true})
	strictRoot := strictDoc["components"].(map[string]interface{})["schemas"].(map[string]interface{})["RootSchema"].(map[string]interface{})
	strictProps := strictRoot["properties"].(map[string]interface{})
	assert.Equal(t, false, strictProps["closed"].(map[string]interface{})["additionalProperties"])
	assert.Equal(t, true, strictProps["open"].(map[string]interface{})["additionalProperties"])
}

// TestOpenAPIInfoVersionConfigurable verifies info.version is configurable instead of hardcoded.
func TestOpenAPIInfoVersionConfigurable(t *testing.T) {
	xsd := `<?xml version="1.0"?><xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:element name="x" type="xs:string"/></xs:schema>`
	doc := runOpenAPI(t, xsd, map[string]interface{}{ivOpenAPIInfoVersion: "2.3.4"})
	assert.Equal(t, "2.3.4", doc["info"].(map[string]interface{})["version"])
}

// TestXSDStringSizeLimit verifies oversized XSD input is rejected up front rather than fully parsed,
// guarding against memory/CPU exhaustion from untrusted input.
func TestXSDStringSizeLimit(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	huge := strings.Repeat("a", maxXSDStringLength+1)
	tc.SetInput(ivXSDString, huge)
	tc.SetInput(ivOutputFormat, "jsonschema")

	done, err := act.Eval(tc)
	assert.True(t, done)
	assert.NoError(t, err)
	assert.True(t, tc.GetOutput(ovError).(bool))
	assert.Contains(t, tc.GetOutput(ovErrorMessage).(string), "exceeds maximum allowed size")
}

// TestAdditionalSchemasResolvesIncludedType verifies a type referenced via xs:include/xs:import
// (declared only in a separate schema file) is fully resolved when its content is supplied via
// the additionalSchemas input, instead of degrading to a generic/opaque type.
func TestAdditionalSchemasResolvesIncludedType(t *testing.T) {
	main := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:tns="http://example.com/order" targetNamespace="http://example.com/order">
  <xs:include schemaLocation="common-types.xsd"/>
  <xs:element name="order">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="shipTo" type="tns:AddressType"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	common := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:tns="http://example.com/order" targetNamespace="http://example.com/order">
  <xs:complexType name="AddressType">
    <xs:sequence>
      <xs:element name="street" type="xs:string"/>
      <xs:element name="city" type="xs:string"/>
      <xs:element name="zip" type="xs:string"/>
    </xs:sequence>
  </xs:complexType>
</xs:schema>`

	// Without the included schema supplied, AddressType is unresolvable and SkipUnsupported
	// (always on) falls back to a bare string - the structure is silently lost.
	docMissing := runOpenAPI(t, main, nil)
	rootMissing := docMissing["components"].(map[string]interface{})["schemas"].(map[string]interface{})["RootSchema"].(map[string]interface{})
	shipToMissing := rootMissing["properties"].(map[string]interface{})["order"].(map[string]interface{})["properties"].(map[string]interface{})["shipTo"].(map[string]interface{})
	assert.Equal(t, "string", shipToMissing["type"])

	// With additionalSchemas supplied, AddressType resolves and its real structure is expanded.
	docResolved := runOpenAPI(t, main, map[string]interface{}{ivAdditionalSchemas: []string{common}})
	rootResolved := docResolved["components"].(map[string]interface{})["schemas"].(map[string]interface{})["RootSchema"].(map[string]interface{})
	shipTo := rootResolved["properties"].(map[string]interface{})["order"].(map[string]interface{})["properties"].(map[string]interface{})["shipTo"].(map[string]interface{})
	assert.Equal(t, "object", shipTo["type"])
	addrProps := shipTo["properties"].(map[string]interface{})
	assert.Equal(t, "string", addrProps["street"].(map[string]interface{})["type"])
	assert.Equal(t, "string", addrProps["city"].(map[string]interface{})["type"])
	assert.Equal(t, "string", addrProps["zip"].(map[string]interface{})["type"])
}

// TestAdditionalSchemasCountLimit verifies an excessive additionalSchemas count is rejected.
func TestAdditionalSchemasCountLimit(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	extras := make([]string, maxAdditionalSchemas+1)
	for i := range extras {
		extras[i] = `<?xml version="1.0"?><xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`
	}
	tc.SetInput(ivXSDString, `<?xml version="1.0"?><xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:element name="x" type="xs:string"/></xs:schema>`)
	tc.SetInput(ivAdditionalSchemas, extras)
	tc.SetInput(ivOutputFormat, "jsonschema")

	done, err := act.Eval(tc)
	assert.True(t, done)
	assert.NoError(t, err)
	assert.True(t, tc.GetOutput(ovError).(bool))
	assert.Contains(t, tc.GetOutput(ovErrorMessage).(string), "exceeds maximum allowed count")
}

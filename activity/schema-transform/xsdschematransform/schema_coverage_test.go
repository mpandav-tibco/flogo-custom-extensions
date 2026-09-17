package xsdschematransform

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// This file is a systematic coverage matrix run directly against the Go activity (no live Flogo
// app needed) - one XSD construct per test, so gaps are caught here instead of in production.

// TestCoverage_GroupRefInSequence verifies <xs:group ref="..."/> inside a sequence expands the
// referenced group's elements directly into the containing type, instead of being silently dropped.
func TestCoverage_GroupRefInSequence(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:tns="http://example.com/x">
  <xs:group name="AuditFields">
    <xs:sequence>
      <xs:element name="createdBy" type="xs:string"/>
      <xs:element name="createdDate" type="xs:dateTime"/>
    </xs:sequence>
  </xs:group>
  <xs:element name="record">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="id" type="xs:string"/>
        <xs:group ref="tns:AuditFields"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	doc := runOpenAPI(t, xsd, nil)
	root := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})["RootSchema"].(map[string]interface{})
	record := root["properties"].(map[string]interface{})["record"].(map[string]interface{})
	props := record["properties"].(map[string]interface{})
	assert.Equal(t, "string", props["id"].(map[string]interface{})["type"])
	assert.Equal(t, "string", props["createdBy"].(map[string]interface{})["type"])
	assert.Equal(t, "string", props["createdDate"].(map[string]interface{})["type"])
	assert.Equal(t, "date-time", props["createdDate"].(map[string]interface{})["format"])
}

// TestCoverage_AttributeGroupRef verifies <xs:attributeGroup ref="..."/> expands the referenced
// group's attributes into the containing type.
func TestCoverage_AttributeGroupRef(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:tns="http://example.com/x">
  <xs:attributeGroup name="CommonAttrs">
    <xs:attribute name="version" type="xs:string" use="required"/>
    <xs:attribute name="locale" type="xs:string"/>
  </xs:attributeGroup>
  <xs:element name="record">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="id" type="xs:string"/>
      </xs:sequence>
      <xs:attributeGroup ref="tns:CommonAttrs"/>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	doc := runOpenAPI(t, xsd, nil)
	root := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})["RootSchema"].(map[string]interface{})
	record := root["properties"].(map[string]interface{})["record"].(map[string]interface{})
	props := record["properties"].(map[string]interface{})
	assert.Equal(t, "string", props["version"].(map[string]interface{})["type"])
	assert.Equal(t, "string", props["locale"].(map[string]interface{})["type"])
	required := toStringSlice(record["required"])
	assert.Contains(t, required, "version")
}

// TestCoverage_ElementRef verifies <xs:element ref="..."/> resolves to the referenced global
// element's type instead of falling back to a bare string.
func TestCoverage_ElementRef(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:tns="http://example.com/x">
  <xs:element name="address">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="city" type="xs:string"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
  <xs:element name="order">
    <xs:complexType>
      <xs:sequence>
        <xs:element ref="tns:address"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	doc := runOpenAPI(t, xsd, nil)
	root := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})["RootSchema"].(map[string]interface{})
	order := root["properties"].(map[string]interface{})["order"].(map[string]interface{})
	address := order["properties"].(map[string]interface{})["address"].(map[string]interface{})
	assert.Equal(t, "object", address["type"])
	assert.Equal(t, "string", address["properties"].(map[string]interface{})["city"].(map[string]interface{})["type"])
}

// TestCoverage_AttributeRef verifies <xs:attribute ref="..."/> resolves to the referenced global
// attribute's type instead of falling back to a bare string.
func TestCoverage_AttributeRef(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:tns="http://example.com/x">
  <xs:attribute name="lang" type="xs:string"/>
  <xs:element name="record">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="id" type="xs:string"/>
      </xs:sequence>
      <xs:attribute ref="tns:lang"/>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	doc := runOpenAPI(t, xsd, nil)
	root := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})["RootSchema"].(map[string]interface{})
	record := root["properties"].(map[string]interface{})["record"].(map[string]interface{})
	assert.Equal(t, "string", record["properties"].(map[string]interface{})["lang"].(map[string]interface{})["type"])
}

// TestCoverage_ChoiceWithSequenceBranch verifies a <xs:choice> containing a <xs:sequence> branch
// (a very common "either [A,B] or [C]" pattern) produces a full multi-property oneOf alternative,
// instead of silently dropping the entire branch.
func TestCoverage_ChoiceWithSequenceBranch(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="payment">
    <xs:complexType>
      <xs:choice>
        <xs:sequence>
          <xs:element name="cardNumber" type="xs:string"/>
          <xs:element name="expiry" type="xs:string"/>
        </xs:sequence>
        <xs:element name="bankAccount" type="xs:string"/>
      </xs:choice>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	doc := runOpenAPI(t, xsd, nil)
	root := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})["RootSchema"].(map[string]interface{})
	payment := root["properties"].(map[string]interface{})["payment"].(map[string]interface{})
	oneOf := payment["oneOf"].([]interface{})
	assert.Len(t, oneOf, 2)

	// Find the sequence-branch alternative (has 2 properties) vs the single-element branch.
	var sequenceBranch, singleBranch map[string]interface{}
	for _, alt := range oneOf {
		branch := alt.(map[string]interface{})
		if len(branch["properties"].(map[string]interface{})) == 2 {
			sequenceBranch = branch
		} else {
			singleBranch = branch
		}
	}
	assert.NotNil(t, sequenceBranch, "the xs:sequence branch must not be dropped")
	assert.Equal(t, "string", sequenceBranch["properties"].(map[string]interface{})["cardNumber"].(map[string]interface{})["type"])
	assert.Equal(t, "string", sequenceBranch["properties"].(map[string]interface{})["expiry"].(map[string]interface{})["type"])
	assert.NotNil(t, singleBranch)
	assert.Equal(t, "string", singleBranch["properties"].(map[string]interface{})["bankAccount"].(map[string]interface{})["type"])
}

// TestCoverage_ChoiceWithGroupRefBranch verifies a <xs:group ref="..."/> branch inside a choice
// expands to a full alternative rather than being silently dropped.
func TestCoverage_ChoiceWithGroupRefBranch(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:tns="http://example.com/x">
  <xs:group name="CardDetails">
    <xs:sequence>
      <xs:element name="cardNumber" type="xs:string"/>
      <xs:element name="expiry" type="xs:string"/>
    </xs:sequence>
  </xs:group>
  <xs:element name="payment">
    <xs:complexType>
      <xs:choice>
        <xs:group ref="tns:CardDetails"/>
        <xs:element name="bankAccount" type="xs:string"/>
      </xs:choice>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	doc := runOpenAPI(t, xsd, nil)
	root := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})["RootSchema"].(map[string]interface{})
	payment := root["properties"].(map[string]interface{})["payment"].(map[string]interface{})
	oneOf := payment["oneOf"].([]interface{})
	assert.Len(t, oneOf, 2)

	var groupBranch map[string]interface{}
	for _, alt := range oneOf {
		branch := alt.(map[string]interface{})
		if len(branch["properties"].(map[string]interface{})) == 2 {
			groupBranch = branch
		}
	}
	assert.NotNil(t, groupBranch, "the xs:group ref branch must not be dropped")
	assert.Equal(t, "string", groupBranch["properties"].(map[string]interface{})["cardNumber"].(map[string]interface{})["type"])
}

// TestCoverage_NestedChoiceFlattens verifies a <xs:choice> nested inside another <xs:choice>
// flattens into the parent's alternatives instead of being dropped or double-nested.
func TestCoverage_NestedChoiceFlattens(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="contact">
    <xs:complexType>
      <xs:choice>
        <xs:element name="email" type="xs:string"/>
        <xs:choice>
          <xs:element name="phone" type="xs:string"/>
          <xs:element name="fax" type="xs:string"/>
        </xs:choice>
      </xs:choice>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	doc := runOpenAPI(t, xsd, nil)
	root := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})["RootSchema"].(map[string]interface{})
	contact := root["properties"].(map[string]interface{})["contact"].(map[string]interface{})
	oneOf := contact["oneOf"].([]interface{})
	// Flattened: email, phone, fax as 3 flat alternatives (not 2, with one being a nested oneOf).
	assert.Len(t, oneOf, 3)
	var names []string
	for _, alt := range oneOf {
		for k := range alt.(map[string]interface{})["properties"].(map[string]interface{}) {
			names = append(names, k)
		}
	}
	assert.ElementsMatch(t, []string{"email", "phone", "fax"}, names)
}

// toStringSlice converts a JSON-decoded []interface{} of strings to []string for assertions.
func toStringSlice(v interface{}) []string {
	if v == nil {
		return nil
	}
	arr := v.([]interface{})
	out := make([]string, len(arr))
	for i, e := range arr {
		out[i] = e.(string)
	}
	return out
}

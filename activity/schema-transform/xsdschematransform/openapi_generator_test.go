package xsdschematransform

import (
	"encoding/json"
	"testing"

	"github.com/project-flogo/core/support/test"
	"github.com/stretchr/testify/assert"
)

const restrictedXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="http://example.com/order"
           elementFormDefault="qualified">

    <xs:element name="id">
        <xs:simpleType>
            <xs:restriction base="xs:string">
                <xs:pattern value="^ORD-[0-9]{6}$"/>
            </xs:restriction>
        </xs:simpleType>
    </xs:element>
    <xs:element name="quantity">
        <xs:simpleType>
            <xs:restriction base="xs:int">
                <xs:minInclusive value="1"/>
                <xs:maxInclusive value="100"/>
            </xs:restriction>
        </xs:simpleType>
    </xs:element>
    <xs:element name="status">
        <xs:simpleType>
            <xs:restriction base="xs:string">
                <xs:enumeration value="PENDING"/>
                <xs:enumeration value="SHIPPED"/>
            </xs:restriction>
        </xs:simpleType>
    </xs:element>
    <xs:element name="tag" minOccurs="0" maxOccurs="unbounded" type="xs:string"/>
</xs:schema>`

func TestXSDSchemaTransformActivity_OpenAPIDefault31(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	tc.SetInput(ivXSDString, restrictedXSD)
	tc.SetInput(ivOutputFormat, "openapi")

	done, err := act.Eval(tc)
	assert.True(t, done)
	assert.NoError(t, err)
	assert.False(t, tc.GetOutput(ovError).(bool))

	out := tc.GetOutput(ovOpenAPISchemaString).(string)
	assert.NotEmpty(t, out)

	var doc map[string]interface{}
	assert.NoError(t, json.Unmarshal([]byte(out), &doc))
	assert.Equal(t, "3.1.0", doc["openapi"])

	schemas := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})
	root := schemas["RootSchema"].(map[string]interface{})
	props := root["properties"].(map[string]interface{})

	id := props["id"].(map[string]interface{})
	assert.Equal(t, "^ORD-[0-9]{6}$", id["pattern"])

	quantity := props["quantity"].(map[string]interface{})
	assert.Equal(t, float64(1), quantity["minimum"])
	assert.Equal(t, float64(100), quantity["maximum"])

	status := props["status"].(map[string]interface{})
	assert.ElementsMatch(t, []interface{}{"PENDING", "SHIPPED"}, status["enum"])

	// Repeating element (maxOccurs="unbounded") must become an array with items.
	tag := props["tag"].(map[string]interface{})
	assert.Equal(t, "array", tag["type"])
	assert.NotNil(t, tag["items"])
}

// enterpriseOrderXSD mirrors examples/schema_converter/testdata/EnterpriseOrder.xsd and is kept
// in sync with it. All complex types are inline: testing showed mapXSDTypeToUniversal does not
// resolve named complexType/simpleType references (only built-in xs: types and inline
// complexType/simpleType are handled), so named-type reuse and xs:union are not exercised here.
const enterpriseOrderXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="http://example.com/enterprise/order"
           xmlns:tns="http://example.com/enterprise/order"
           elementFormDefault="qualified">
  <xs:element name="PurchaseOrder">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="orderId">
          <xs:simpleType>
            <xs:restriction base="xs:string">
              <xs:pattern value="ORD-[0-9]{6}"/>
            </xs:restriction>
          </xs:simpleType>
        </xs:element>
        <xs:element name="orderDate" type="xs:date"/>
        <xs:element name="customer">
          <xs:complexType>
            <xs:sequence>
              <xs:element name="customerId">
                <xs:simpleType>
                  <xs:restriction base="xs:string">
                    <xs:pattern value="CUST-[0-9]{8}"/>
                  </xs:restriction>
                </xs:simpleType>
              </xs:element>
              <xs:element name="name">
                <xs:simpleType>
                  <xs:restriction base="xs:string">
                    <xs:minLength value="1"/>
                    <xs:maxLength value="120"/>
                  </xs:restriction>
                </xs:simpleType>
              </xs:element>
              <xs:element name="creditLimit">
                <xs:simpleType>
                  <xs:restriction base="xs:decimal">
                    <xs:totalDigits value="10"/>
                    <xs:fractionDigits value="2"/>
                    <xs:minInclusive value="0"/>
                    <xs:maxInclusive value="1000000"/>
                  </xs:restriction>
                </xs:simpleType>
              </xs:element>
            </xs:sequence>
          </xs:complexType>
        </xs:element>
        <xs:element name="items" minOccurs="1" maxOccurs="unbounded">
          <xs:complexType>
            <xs:sequence>
              <xs:element name="sku">
                <xs:simpleType>
                  <xs:restriction base="xs:string">
                    <xs:pattern value="SKU-[A-Z0-9]{8}"/>
                  </xs:restriction>
                </xs:simpleType>
              </xs:element>
              <xs:element name="quantity">
                <xs:simpleType>
                  <xs:restriction base="xs:int">
                    <xs:minInclusive value="1"/>
                    <xs:maxInclusive value="9999"/>
                  </xs:restriction>
                </xs:simpleType>
              </xs:element>
              <xs:element name="unitPrice">
                <xs:simpleType>
                  <xs:restriction base="xs:decimal">
                    <xs:totalDigits value="10"/>
                    <xs:fractionDigits value="2"/>
                    <xs:minExclusive value="0"/>
                  </xs:restriction>
                </xs:simpleType>
              </xs:element>
            </xs:sequence>
          </xs:complexType>
        </xs:element>
        <xs:element name="totalAmount">
          <xs:simpleType>
            <xs:restriction base="xs:decimal">
              <xs:totalDigits value="12"/>
              <xs:fractionDigits value="2"/>
              <xs:minInclusive value="0"/>
            </xs:restriction>
          </xs:simpleType>
        </xs:element>
        <xs:element name="currency">
          <xs:simpleType>
            <xs:restriction base="xs:string">
              <xs:enumeration value="USD"/>
              <xs:enumeration value="EUR"/>
              <xs:enumeration value="GBP"/>
            </xs:restriction>
          </xs:simpleType>
        </xs:element>
        <xs:element name="status" default="PENDING">
          <xs:simpleType>
            <xs:restriction base="xs:string">
              <xs:enumeration value="PENDING"/>
              <xs:enumeration value="APPROVED"/>
              <xs:enumeration value="SHIPPED"/>
              <xs:enumeration value="CANCELLED"/>
            </xs:restriction>
          </xs:simpleType>
        </xs:element>
        <xs:element name="discountPercent" minOccurs="0">
          <xs:simpleType>
            <xs:restriction base="xs:decimal">
              <xs:minExclusive value="0"/>
              <xs:maxExclusive value="100"/>
            </xs:restriction>
          </xs:simpleType>
        </xs:element>
        <xs:element name="promoCode" minOccurs="0">
          <xs:simpleType>
            <xs:restriction base="xs:string">
              <xs:pattern value="PROMO-[A-Z]{4}"/>
              <xs:pattern value="VIP-[0-9]{4}"/>
            </xs:restriction>
          </xs:simpleType>
        </xs:element>
        <xs:element name="notes" minOccurs="0" nillable="true">
          <xs:simpleType>
            <xs:restriction base="xs:string">
              <xs:minLength value="0"/>
              <xs:maxLength value="500"/>
            </xs:restriction>
          </xs:simpleType>
        </xs:element>
        <xs:element name="tags" minOccurs="0">
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
        <xs:choice>
          <xs:element name="standardShipping">
            <xs:complexType>
              <xs:sequence>
                <xs:element name="carrier" type="xs:string"/>
                <xs:element name="estimatedDays">
                  <xs:simpleType>
                    <xs:restriction base="xs:int">
                      <xs:minInclusive value="3"/>
                      <xs:maxInclusive value="10"/>
                    </xs:restriction>
                  </xs:simpleType>
                </xs:element>
              </xs:sequence>
            </xs:complexType>
          </xs:element>
          <xs:element name="expressShipping">
            <xs:complexType>
              <xs:sequence>
                <xs:element name="carrier" type="xs:string"/>
                <xs:element name="guaranteedByHour">
                  <xs:simpleType>
                    <xs:restriction base="xs:int">
                      <xs:minInclusive value="1"/>
                      <xs:maxInclusive value="48"/>
                    </xs:restriction>
                  </xs:simpleType>
                </xs:element>
                <xs:element name="surcharge">
                  <xs:simpleType>
                    <xs:restriction base="xs:decimal">
                      <xs:totalDigits value="8"/>
                      <xs:fractionDigits value="2"/>
                      <xs:minInclusive value="0"/>
                    </xs:restriction>
                  </xs:simpleType>
                </xs:element>
              </xs:sequence>
            </xs:complexType>
          </xs:element>
        </xs:choice>
        <xs:element name="auditInfo">
          <xs:complexType>
            <xs:all>
              <xs:element name="createdBy" type="xs:string"/>
              <xs:element name="approvedBy" type="xs:string" minOccurs="0"/>
            </xs:all>
          </xs:complexType>
        </xs:element>
      </xs:sequence>
      <xs:attribute name="version" type="xs:string" use="required" fixed="1.0"/>
      <xs:attribute name="region" use="optional">
        <xs:simpleType>
          <xs:restriction base="xs:string">
            <xs:enumeration value="NA"/>
            <xs:enumeration value="EMEA"/>
            <xs:enumeration value="APAC"/>
          </xs:restriction>
        </xs:simpleType>
      </xs:attribute>
    </xs:complexType>
  </xs:element>
</xs:schema>`

// TestXSDSchemaTransformActivity_OpenAPI_EnterpriseOrder validates the full XSD->OpenAPI
// restriction mapping end to end against an enterprise-style purchase-order schema.
func TestXSDSchemaTransformActivity_OpenAPI_EnterpriseOrder(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	tc.SetInput(ivXSDString, enterpriseOrderXSD)
	tc.SetInput(ivOutputFormat, "openapi")
	tc.SetInput(ivOpenAPISchemaName, "PurchaseOrder")

	done, err := act.Eval(tc)
	assert.True(t, done)
	assert.NoError(t, err)
	assert.False(t, tc.GetOutput(ovError).(bool), tc.GetOutput(ovErrorMessage))

	var doc map[string]interface{}
	assert.NoError(t, json.Unmarshal([]byte(tc.GetOutput(ovOpenAPISchemaString).(string)), &doc))
	assert.Equal(t, "3.1.0", doc["openapi"])

	root := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})["PurchaseOrder"].(map[string]interface{})
	// the schema root is a synthetic wrapper; the XSD's single top-level element nests one level in
	po := root["properties"].(map[string]interface{})["PurchaseOrder"].(map[string]interface{})
	props := po["properties"].(map[string]interface{})

	// xs:pattern
	assert.Equal(t, "ORD-[0-9]{6}", props["orderId"].(map[string]interface{})["pattern"])

	// nested inline complex type + xs:totalDigits/fractionDigits + xs:minInclusive/maxInclusive
	customer := props["customer"].(map[string]interface{})
	customerProps := customer["properties"].(map[string]interface{})
	assert.Equal(t, "CUST-[0-9]{8}", customerProps["customerId"].(map[string]interface{})["pattern"])
	assert.Equal(t, float64(1), customerProps["name"].(map[string]interface{})["minLength"])
	assert.Equal(t, float64(120), customerProps["name"].(map[string]interface{})["maxLength"])
	creditLimit := customerProps["creditLimit"].(map[string]interface{})
	assert.Equal(t, float64(0), creditLimit["minimum"])
	assert.Equal(t, float64(1000000), creditLimit["maximum"])
	assert.Equal(t, float64(10), creditLimit["x-xsdTotalDigits"])
	assert.Equal(t, float64(2), creditLimit["x-xsdFractionDigits"])
	assert.InDelta(t, 0.01, creditLimit["multipleOf"].(float64), 0.0000001)

	// minOccurs=1/maxOccurs=unbounded -> array with minItems, no maxItems
	items := props["items"].(map[string]interface{})
	assert.Equal(t, "array", items["type"])
	assert.Equal(t, float64(1), items["minItems"])
	assert.Nil(t, items["maxItems"])
	itemProps := items["items"].(map[string]interface{})["properties"].(map[string]interface{})
	assert.Equal(t, "SKU-[A-Z0-9]{8}", itemProps["sku"].(map[string]interface{})["pattern"])
	// xs:minExclusive with no maxExclusive -> numeric exclusiveMinimum (3.1 dialect)
	assert.Equal(t, float64(0), itemProps["unitPrice"].(map[string]interface{})["exclusiveMinimum"])

	// xs:enumeration
	assert.ElementsMatch(t, []interface{}{"USD", "EUR", "GBP"}, props["currency"].(map[string]interface{})["enum"])

	// xs:enumeration + xs:default
	status := props["status"].(map[string]interface{})
	assert.ElementsMatch(t, []interface{}{"PENDING", "APPROVED", "SHIPPED", "CANCELLED"}, status["enum"])
	assert.Equal(t, "PENDING", status["default"])

	// xs:minExclusive / xs:maxExclusive -> numeric bounds (3.1 dialect)
	discount := props["discountPercent"].(map[string]interface{})
	assert.Equal(t, float64(0), discount["exclusiveMinimum"])
	assert.Equal(t, float64(100), discount["exclusiveMaximum"])
	assert.Nil(t, discount["minimum"])
	assert.Nil(t, discount["maximum"])

	// multiple xs:pattern facets combined as an OR alternation
	assert.Equal(t, "(?:PROMO-[A-Z]{4})|(?:VIP-[0-9]{4})", props["promoCode"].(map[string]interface{})["pattern"])

	// nillable -> type array with "null" (3.1 dialect)
	notes := props["notes"].(map[string]interface{})
	assert.Equal(t, []interface{}{"string", "null"}, notes["type"])
	assert.Equal(t, float64(500), notes["maxLength"])

	// xs:list -> array with its inline item schema preserved
	tags := props["tags"].(map[string]interface{})
	assert.Equal(t, "array", tags["type"])
	tagItems := tags["items"].(map[string]interface{})
	assert.Equal(t, "string", tagItems["type"])
	assert.Equal(t, float64(1), tagItems["minLength"])
	assert.Equal(t, float64(20), tagItems["maxLength"])

	// xs:choice -> oneOf, one branch per choice element
	oneOf := po["oneOf"].([]interface{})
	assert.Len(t, oneOf, 2)
	branchNames := map[string]bool{}
	for _, b := range oneOf {
		for name := range b.(map[string]interface{})["properties"].(map[string]interface{}) {
			branchNames[name] = true
		}
	}
	assert.True(t, branchNames["standardShipping"])
	assert.True(t, branchNames["expressShipping"])

	// xs:all -> plain object properties
	auditProps := props["auditInfo"].(map[string]interface{})["properties"].(map[string]interface{})
	assert.Contains(t, auditProps, "createdBy")
	assert.Contains(t, auditProps, "approvedBy")

	// required attribute (use="required") + xs:fixed -> single-value enum/default
	assert.Equal(t, []interface{}{"version"}, po["required"])
	version := props["version"].(map[string]interface{})
	assert.Equal(t, []interface{}{"1.0"}, version["enum"])
	assert.Equal(t, "1.0", version["default"])

	// optional attribute with inline enumeration restriction
	assert.ElementsMatch(t, []interface{}{"NA", "EMEA", "APAC"}, props["region"].(map[string]interface{})["enum"])
}

func TestXSDSchemaTransformActivity_OpenAPI30ExclusiveBounds(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	xsdSchema := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
    <xs:element name="price">
        <xs:simpleType>
            <xs:restriction base="xs:decimal">
                <xs:minExclusive value="0"/>
                <xs:maxExclusive value="1000"/>
            </xs:restriction>
        </xs:simpleType>
    </xs:element>
</xs:schema>`

	tc.SetInput(ivXSDString, xsdSchema)
	tc.SetInput(ivOutputFormat, "openapi")
	tc.SetInput(ivOpenAPIVersion, "3.0.3")
	tc.SetInput(ivOpenAPISchemaName, "Price")

	done, err := act.Eval(tc)
	assert.True(t, done)
	assert.NoError(t, err)
	assert.False(t, tc.GetOutput(ovError).(bool))

	out := tc.GetOutput(ovOpenAPISchemaString).(string)
	var doc map[string]interface{}
	assert.NoError(t, json.Unmarshal([]byte(out), &doc))
	assert.Equal(t, "3.0.3", doc["openapi"])

	schemas := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})
	root := schemas["Price"].(map[string]interface{})
	props := root["properties"].(map[string]interface{})
	price := props["price"].(map[string]interface{})

	// 3.0.x dialect: minimum/maximum carry the bound, exclusiveMinimum/Maximum are booleans.
	assert.Equal(t, float64(0), price["minimum"])
	assert.Equal(t, true, price["exclusiveMinimum"])
	assert.Equal(t, float64(1000), price["maximum"])
	assert.Equal(t, true, price["exclusiveMaximum"])
}

// TestXSDSchemaTransformActivity_OpenAPIInvalidVersion ensures unsupported/garbage openApiVersion
// values are rejected up front instead of silently falling back to the 3.0.x dialect.
func TestXSDSchemaTransformActivity_OpenAPIInvalidVersion(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	tc.SetInput(ivXSDString, `<?xml version="1.0"?><xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:element name="x" type="xs:string"/></xs:schema>`)
	tc.SetInput(ivOutputFormat, "openapi")
	tc.SetInput(ivOpenAPIVersion, "2.0")

	done, err := act.Eval(tc)
	assert.True(t, done)
	assert.NoError(t, err)
	assert.True(t, tc.GetOutput(ovError).(bool))
	assert.Contains(t, tc.GetOutput(ovErrorMessage).(string), "openApiVersion")
}

// TestXSDSchemaTransformActivity_OpenAPIRootNameCollision ensures the configured root schema name
// always wins over a same-named XSD complexType definition instead of being silently overwritten.
func TestXSDSchemaTransformActivity_OpenAPIRootNameCollision(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	xsdSchema := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
    <xs:element name="widget" type="Widget"/>
    <xs:complexType name="Widget">
        <xs:sequence>
            <xs:element name="id" type="xs:string"/>
        </xs:sequence>
    </xs:complexType>
</xs:schema>`

	tc.SetInput(ivXSDString, xsdSchema)
	tc.SetInput(ivOutputFormat, "openapi")
	tc.SetInput(ivOpenAPISchemaName, "Widget") // deliberately collides with the named complexType

	done, err := act.Eval(tc)
	assert.True(t, done)
	assert.NoError(t, err)
	assert.False(t, tc.GetOutput(ovError).(bool))

	var doc map[string]interface{}
	assert.NoError(t, json.Unmarshal([]byte(tc.GetOutput(ovOpenAPISchemaString).(string)), &doc))

	schemas := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})
	widget := schemas["Widget"].(map[string]interface{})

	// The root wrapper (containing the top-level "widget" element) must win, not the Widget complexType.
	assert.Contains(t, widget["properties"].(map[string]interface{}), "widget")
}

// TestXSDSchemaTransformActivity_OpenAPIRequiredIsSorted verifies the "required" array is emitted
// in deterministic (sorted) order regardless of Go's randomized map iteration.
func TestXSDSchemaTransformActivity_OpenAPIRequiredIsSorted(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	xsdSchema := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
    <xs:element name="record">
        <xs:complexType>
            <xs:sequence>
                <xs:element name="z" type="xs:string"/>
                <xs:element name="a" type="xs:string"/>
                <xs:element name="m" type="xs:string"/>
            </xs:sequence>
            <xs:attribute name="zAttr" type="xs:string" use="required"/>
            <xs:attribute name="aAttr" type="xs:string" use="required"/>
            <xs:attribute name="mAttr" type="xs:string" use="required"/>
        </xs:complexType>
    </xs:element>
</xs:schema>`

	tc.SetInput(ivXSDString, xsdSchema)
	tc.SetInput(ivOutputFormat, "openapi")

	done, err := act.Eval(tc)
	assert.True(t, done)
	assert.NoError(t, err)
	assert.False(t, tc.GetOutput(ovError).(bool))

	var doc map[string]interface{}
	assert.NoError(t, json.Unmarshal([]byte(tc.GetOutput(ovOpenAPISchemaString).(string)), &doc))

	schemas := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})
	root := schemas["RootSchema"].(map[string]interface{})
	record := root["properties"].(map[string]interface{})["record"].(map[string]interface{})

	required := record["required"].([]interface{})
	assert.Equal(t, []interface{}{"aAttr", "mAttr", "zAttr"}, required)
}

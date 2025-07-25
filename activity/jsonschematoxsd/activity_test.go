package jsonschematoxsd

import (
	"strings"
	"testing"

	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/support/test"
	"github.com/stretchr/testify/assert"
)

func TestRegister(t *testing.T) {
	ref := activity.GetRef(&Activity{})
	act := activity.Get(ref)
	assert.NotNil(t, act)
}

func TestEval(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	tests := []struct {
		name           string
		jsonSchema     string
		rootElement    string
		targetNS       string
		expectedOutput string
		expectError    bool
	}{
		{
			name: "Simple Object Schema",
			jsonSchema: `{
				"type": "object",
				"properties": {
					"name": {"type": "string"},
					"age": {"type": "integer"},
					"active": {"type": "boolean"}
				},
				"required": ["name"]
			}`,
			rootElement: "Person",
			targetNS:    "http://example.com/person",
			expectedOutput: `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema elementFormDefault="qualified" targetNamespace="http://example.com/person" xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="Person">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="name" type="xs:string"></xs:element>
        <xs:element name="age" type="xs:integer" minOccurs="0"></xs:element>
        <xs:element name="active" type="xs:boolean" minOccurs="0"></xs:element>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`,
			expectError: false,
		},
		{
			name: "Array Schema",
			jsonSchema: `{
				"type": "object",
				"properties": {
					"items": {
						"type": "array",
						"items": {"type": "string"}
					}
				}
			}`,
			rootElement: "ItemList",
			targetNS:    "",
			expectedOutput: `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema elementFormDefault="qualified" xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="ItemList">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="items" type="xs:string" minOccurs="0" maxOccurs="unbounded"></xs:element>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`,
			expectError: false,
		},
		{
			name: "Nested Object Schema",
			jsonSchema: `{
				"type": "object",
				"properties": {
					"user": {
						"type": "object",
						"properties": {
							"name": {"type": "string"},
							"email": {"type": "string"}
						},
						"required": ["name"]
					}
				}
			}`,
			rootElement: "UserData",
			targetNS:    "",
			expectedOutput: `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema elementFormDefault="qualified" xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="UserData">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="user" minOccurs="0">
          <xs:complexType>
            <xs:sequence>
              <xs:element name="name" type="xs:string"></xs:element>
              <xs:element name="email" type="xs:string" minOccurs="0"></xs:element>
            </xs:sequence>
          </xs:complexType>
        </xs:element>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`,
			expectError: false,
		},
		{
			name: "Invalid JSON Schema",
			jsonSchema: `{
				"type": "invalid_type",
				"properties": {}
			}`,
			rootElement: "Test",
			targetNS:    "",
			expectError: true,
		},
		{
			name: "Non-Object Root",
			jsonSchema: `{
				"type": "string"
			}`,
			rootElement: "StringRoot",
			targetNS:    "",
			expectError: true,
		},
		{
			name: "Array without Items",
			jsonSchema: `{
				"type": "object",
				"properties": {
					"badArray": {
						"type": "array"
					}
				}
			}`,
			rootElement: "BadArrayTest",
			targetNS:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set inputs
			tc.SetInput(ivJSONSchemaString, tt.jsonSchema)
			tc.SetInput(ivRootElementName, tt.rootElement)
			tc.SetInput(ivTargetNamespace, tt.targetNS)

			// Execute activity
			done, err := act.Eval(tc)

			if tt.expectError {
				assert.False(t, done)
				assert.Error(t, err)
				errorOutput := tc.GetOutput(ovError)
				assert.True(t, errorOutput.(bool))
			} else {
				assert.True(t, done)
				assert.NoError(t, err)

				// Check outputs
				xsdOutput := tc.GetOutput(ovXSDString)
				errorOutput := tc.GetOutput(ovError)

				assert.False(t, errorOutput.(bool))
				assert.NotEmpty(t, xsdOutput)

				// For the "Simple Object Schema" test, check specific content due to map ordering
				if tt.name == "Simple Object Schema" {
					xsdString := xsdOutput.(string)
					assert.Contains(t, xsdString, `name="Person"`)
					assert.Contains(t, xsdString, `name="name" type="xs:string"`)
					assert.Contains(t, xsdString, `name="age" type="xs:integer" minOccurs="0"`)
					assert.Contains(t, xsdString, `name="active" type="xs:boolean" minOccurs="0"`)
					assert.Contains(t, xsdString, `targetNamespace="http://example.com/person"`)
				} else {
					// For other tests, compare normalized XML
					expectedNormalized := normalizeXML(tt.expectedOutput)
					actualNormalized := normalizeXML(xsdOutput.(string))
					assert.Equal(t, expectedNormalized, actualNormalized)
				}
			}
		})
	}
}

func TestEvalWithDefaults(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	jsonSchema := `{
		"type": "object",
		"properties": {
			"name": {"type": "string"}
		}
	}`

	// Set only required input
	tc.SetInput(ivJSONSchemaString, jsonSchema)

	done, err := act.Eval(tc)
	assert.True(t, done)
	assert.NoError(t, err)

	// Check that defaults were applied
	xsdOutput := tc.GetOutput(ovXSDString)
	xsdString := xsdOutput.(string)

	// Should contain default root element name
	assert.Contains(t, xsdString, `name="RootElement"`)
	// Should not contain targetNamespace attribute
	assert.NotContains(t, xsdString, "targetNamespace=")
}

// Helper function to normalize XML for comparison
func normalizeXML(xmlStr string) string {
	// Remove extra whitespace and normalize line endings
	lines := strings.Split(xmlStr, "\n")
	var normalizedLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			normalizedLines = append(normalizedLines, trimmed)
		}
	}
	return strings.Join(normalizedLines, "\n")
}

// Benchmark tests
func BenchmarkEval(b *testing.B) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	jsonSchema := `{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"age": {"type": "integer"},
			"address": {
				"type": "object",
				"properties": {
					"street": {"type": "string"},
					"city": {"type": "string"},
					"zipcode": {"type": "string"}
				}
			},
			"hobbies": {
				"type": "array",
				"items": {"type": "string"}
			}
		},
		"required": ["name"]
	}`

	tc.SetInput(ivJSONSchemaString, jsonSchema)
	tc.SetInput(ivRootElementName, "Person")
	tc.SetInput(ivTargetNamespace, "http://example.com/person")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := act.Eval(tc)
		if err != nil {
			b.Fatal(err)
		}
	}
}

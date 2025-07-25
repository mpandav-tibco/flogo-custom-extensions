package xsdtojsonschema

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/project-flogo/core/support/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActivity_Metadata(t *testing.T) {
	act := &Activity{}
	md := act.Metadata()
	assert.NotNil(t, md)
}

func TestNew(t *testing.T) {
	ctx := test.NewActivityInitContext(nil, nil)
	act, err := New(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, act)
	assert.IsType(t, &Activity{}, act)
}

func TestActivity_Eval_Success_SimpleElement(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// Simple XSD with a single string element
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="person" type="xs:string"/>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify JSON Schema structure
	assert.Equal(t, "https://json-schema.org/draft/2020-12/schema", jsonSchema["$schema"])
	assert.Equal(t, "string", jsonSchema["type"])
}

func TestActivity_Eval_Success_ComplexObject(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// Complex XSD with nested elements
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="person">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="name" type="xs:string"/>
        <xs:element name="age" type="xs:integer"/>
        <xs:element name="email" type="xs:string" minOccurs="0"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify JSON Schema structure
	assert.Equal(t, "https://json-schema.org/draft/2020-12/schema", jsonSchema["$schema"])
	assert.Equal(t, "object", jsonSchema["type"])

	properties := jsonSchema["properties"].(map[string]interface{})
	assert.Len(t, properties, 3)

	// Check individual properties
	nameProperty := properties["name"].(map[string]interface{})
	assert.Equal(t, "string", nameProperty["type"])

	ageProperty := properties["age"].(map[string]interface{})
	assert.Equal(t, "integer", ageProperty["type"])

	emailProperty := properties["email"].(map[string]interface{})
	assert.Equal(t, "string", emailProperty["type"])

	// Check required fields (name and age should be required, email should not)
	required := jsonSchema["required"].([]interface{})
	assert.Contains(t, required, "name")
	assert.Contains(t, required, "age")
	assert.NotContains(t, required, "email")
}

func TestActivity_Eval_Success_ArrayType(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with unbounded element (array)
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="items" type="xs:string" maxOccurs="unbounded"/>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify JSON Schema structure for array
	assert.Equal(t, "https://json-schema.org/draft/2020-12/schema", jsonSchema["$schema"])
	assert.Equal(t, "array", jsonSchema["type"])

	items := jsonSchema["items"].(map[string]interface{})
	assert.Equal(t, "string", items["type"])
}

func TestActivity_Eval_Success_VariousDataTypes(t *testing.T) {
	tests := []struct {
		name           string
		xsdType        string
		expectedType   string
		expectedFormat string
	}{
		{"String", "xs:string", "string", ""},
		{"Integer", "xs:integer", "integer", ""},
		{"Boolean", "xs:boolean", "boolean", ""},
		{"Date", "xs:date", "string", "date"},
		{"DateTime", "xs:dateTime", "string", "date-time"},
		{"Decimal", "xs:decimal", "number", ""},
		{"Double", "xs:double", "number", ""},
		{"Float", "xs:float", "number", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			act := &Activity{}
			tc := test.NewActivityContext(act.Metadata())

			xsdInput := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="testElement" type="%s"/>
</xs:schema>`, tt.xsdType)

			input := &Input{
				XSDString: xsdInput,
			}
			tc.SetInputObject(input)

			done, err := act.Eval(tc)
			require.NoError(t, err)
			assert.True(t, done)

			// Verify outputs
			jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
			errorFlag := tc.GetOutput(ovError).(bool)
			errorMsg := tc.GetOutput(ovErrorMessage).(string)

			assert.False(t, errorFlag)
			assert.Empty(t, errorMsg)
			assert.NotEmpty(t, jsonSchemaStr)

			// Parse and validate the JSON Schema
			var jsonSchema map[string]interface{}
			err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
			require.NoError(t, err)

			// Verify type
			assert.Equal(t, tt.expectedType, jsonSchema["type"])

			// Verify format if expected
			if tt.expectedFormat != "" {
				assert.Equal(t, tt.expectedFormat, jsonSchema["format"])
			} else {
				assert.Nil(t, jsonSchema["format"])
			}
		})
	}
}

func TestActivity_Eval_Error_EmptyInput(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	input := &Input{
		XSDString: "",
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.Error(t, err)
	assert.False(t, done)

	// Verify error outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.True(t, errorFlag)
	assert.Empty(t, jsonSchemaStr)
	assert.Contains(t, errorMsg, "INVALID_INPUT_XSDString")
	assert.Contains(t, errorMsg, "required and cannot be empty")
}

func TestActivity_Eval_Error_WhitespaceOnlyInput(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	input := &Input{
		XSDString: "   \n\t   ",
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.Error(t, err)
	assert.False(t, done)

	// Verify error outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.True(t, errorFlag)
	assert.Empty(t, jsonSchemaStr)
	assert.Contains(t, errorMsg, "INVALID_INPUT_XSDString")
}

func TestActivity_Eval_Error_InvalidXML(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	input := &Input{
		XSDString: "invalid xml content",
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.Error(t, err)
	assert.False(t, done)

	// Verify error outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.True(t, errorFlag)
	assert.Empty(t, jsonSchemaStr)
	assert.Contains(t, errorMsg, "XSD_PARSE_ERROR")
}

func TestActivity_Eval_Error_MalformedXSD(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	input := &Input{
		XSDString: `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <!-- Missing element definition -->
</xs:schema>`,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.Error(t, err)
	assert.False(t, done)

	// Verify error outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.True(t, errorFlag)
	assert.Empty(t, jsonSchemaStr)
	assert.Contains(t, errorMsg, "CONVERSION_ERROR")
	assert.Contains(t, errorMsg, "does not contain any root elements")
}

func TestActivity_Eval_Error_InvalidInputObject(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// Don't set any input object to trigger the GetInputObject error
	done, err := act.Eval(tc)
	require.Error(t, err)
	assert.False(t, done)

	// Verify error outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.True(t, errorFlag)
	assert.Empty(t, jsonSchemaStr)
	assert.Contains(t, errorMsg, "INVALID_INPUT")
}

func TestConvertSchema_EmptyElements(t *testing.T) {
	schema := &XSDSchema{
		Elements: []XSDElement{},
	}

	result, err := convertSchema(schema)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "does not contain any root elements")
}

func TestMapXsdTypeToJsonSchemaType(t *testing.T) {
	tests := []struct {
		xsdType        string
		expectedType   string
		expectedFormat string
	}{
		// String types
		{"string", "string", ""},
		{"xs:string", "string", ""},
		{"normalizedString", "string", ""},
		{"token", "string", ""},
		{"NMTOKEN", "string", ""},

		// Date/Time types
		{"date", "string", "date"},
		{"dateTime", "string", "date-time"},
		{"time", "string", "time"},
		{"duration", "string", "uri"},
		{"anyURI", "string", "uri"},

		// Binary types
		{"base64Binary", "string", "byte"},

		// Boolean type
		{"boolean", "boolean", ""},

		// Numeric types
		{"decimal", "number", ""},
		{"double", "number", ""},
		{"float", "number", ""},

		// Integer types
		{"integer", "integer", ""},
		{"positiveInteger", "integer", ""},
		{"negativeInteger", "integer", ""},
		{"nonPositiveInteger", "integer", ""},
		{"nonNegativeInteger", "integer", ""},
		{"long", "integer", ""},
		{"int", "integer", ""},
		{"short", "integer", ""},
		{"byte", "integer", ""},
		{"unsignedLong", "integer", ""},
		{"unsignedInt", "integer", ""},
		{"unsignedShort", "integer", ""},
		{"unsignedByte", "integer", ""},

		// Unknown type (defaults to string)
		{"unknownType", "string", ""},
		{"customType", "string", ""},
	}

	for _, tt := range tests {
		t.Run(tt.xsdType, func(t *testing.T) {
			actualType, actualFormat := mapXsdTypeToJsonSchemaType(tt.xsdType)
			assert.Equal(t, tt.expectedType, actualType)
			assert.Equal(t, tt.expectedFormat, actualFormat)
		})
	}
}

func TestSetErrorOutputs(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	setErrorOutputs(tc, "Test error message", "TEST_ERROR")

	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.Empty(t, jsonSchemaStr)
	assert.True(t, errorFlag)
	assert.Equal(t, "[TEST_ERROR] Test error message", errorMsg)
}

func TestInput_FromMap(t *testing.T) {
	input := &Input{}
	values := map[string]interface{}{
		"xsdString": "test xsd content",
	}

	err := input.FromMap(values)
	assert.NoError(t, err)
	assert.Equal(t, "test xsd content", input.XSDString)
}

func TestInput_ToMap(t *testing.T) {
	input := &Input{
		XSDString: "test xsd content",
	}

	result := input.ToMap()
	expected := map[string]interface{}{
		"xsdString": "test xsd content",
	}

	assert.Equal(t, expected, result)
}

func TestOutput_ToMap(t *testing.T) {
	output := &Output{
		JSONSchemaString: "test json schema",
		Error:            true,
		ErrorMessage:     "test error",
	}

	result := output.ToMap()
	expected := map[string]interface{}{
		"jsonSchemaString": "test json schema",
		"error":            true,
		"errorMessage":     "test error",
	}

	assert.Equal(t, expected, result)
}

// --- XSD Choice Feature Tests ---

func TestActivity_Eval_Success_SimpleChoice(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with simple choice between two elements
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="contact">
    <xs:complexType>
      <xs:choice>
        <xs:element name="email" type="xs:string"/>
        <xs:element name="phone" type="xs:string"/>
      </xs:choice>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify JSON Schema structure for choice
	assert.Equal(t, "https://json-schema.org/draft/2020-12/schema", jsonSchema["$schema"])

	oneOf := jsonSchema["oneOf"].([]interface{})
	assert.Len(t, oneOf, 2)

	// Check first choice option (email)
	emailOption := oneOf[0].(map[string]interface{})
	assert.Equal(t, "object", emailOption["type"])
	assert.Equal(t, false, emailOption["additionalProperties"])

	emailProps := emailOption["properties"].(map[string]interface{})
	emailProp := emailProps["email"].(map[string]interface{})
	assert.Equal(t, "string", emailProp["type"])

	emailRequired := emailOption["required"].([]interface{})
	assert.Contains(t, emailRequired, "email")

	// Check second choice option (phone)
	phoneOption := oneOf[1].(map[string]interface{})
	assert.Equal(t, "object", phoneOption["type"])
	assert.Equal(t, false, phoneOption["additionalProperties"])

	phoneProps := phoneOption["properties"].(map[string]interface{})
	phoneProp := phoneProps["phone"].(map[string]interface{})
	assert.Equal(t, "string", phoneProp["type"])

	phoneRequired := phoneOption["required"].([]interface{})
	assert.Contains(t, phoneRequired, "phone")
}

func TestActivity_Eval_Success_ComplexChoice(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with choice between different data types
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="notification">
    <xs:complexType>
      <xs:choice>
        <xs:element name="email" type="xs:string"/>
        <xs:element name="sms" type="xs:string"/>
        <xs:element name="push" type="xs:boolean"/>
        <xs:element name="priority" type="xs:integer"/>
      </xs:choice>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify JSON Schema structure for complex choice
	oneOf := jsonSchema["oneOf"].([]interface{})
	assert.Len(t, oneOf, 4)

	// Verify different data types in choices
	expectedTypes := map[string]string{
		"email":    "string",
		"sms":      "string",
		"push":     "boolean",
		"priority": "integer",
	}

	for _, option := range oneOf {
		optionMap := option.(map[string]interface{})
		properties := optionMap["properties"].(map[string]interface{})

		// Should have exactly one property
		assert.Len(t, properties, 1)

		// Get the property name and type
		for propName, propDef := range properties {
			propDefMap := propDef.(map[string]interface{})
			expectedType, exists := expectedTypes[propName]
			assert.True(t, exists, "Unexpected property name: %s", propName)
			assert.Equal(t, expectedType, propDefMap["type"], "Wrong type for property %s", propName)
		}
	}
}

func TestActivity_Eval_Success_ChoiceArray(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with choice that can occur multiple times (array of choices)
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="messages">
    <xs:complexType>
      <xs:choice maxOccurs="unbounded">
        <xs:element name="text" type="xs:string"/>
        <xs:element name="image" type="xs:string"/>
      </xs:choice>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify JSON Schema structure for choice array
	assert.Equal(t, "array", jsonSchema["type"])

	items := jsonSchema["items"].(map[string]interface{})
	oneOf := items["oneOf"].([]interface{})
	assert.Len(t, oneOf, 2)

	// Check array item choices
	textOption := oneOf[0].(map[string]interface{})
	assert.Equal(t, "object", textOption["type"])
	textProps := textOption["properties"].(map[string]interface{})
	textProp := textProps["text"].(map[string]interface{})
	assert.Equal(t, "string", textProp["type"])

	imageOption := oneOf[1].(map[string]interface{})
	assert.Equal(t, "object", imageOption["type"])
	imageProps := imageOption["properties"].(map[string]interface{})
	imageProp := imageProps["image"].(map[string]interface{})
	assert.Equal(t, "string", imageProp["type"])
}

func TestActivity_Eval_Error_EmptyChoice(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with empty choice (should cause error)
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="empty">
    <xs:complexType>
      <xs:choice>
        <!-- No elements defined -->
      </xs:choice>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.Error(t, err)
	assert.False(t, done)

	// Verify error outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.True(t, errorFlag)
	assert.Empty(t, jsonSchemaStr)
	assert.Contains(t, errorMsg, "CONVERSION_ERROR")
	assert.Contains(t, errorMsg, "choice must contain at least one element")
}

func TestConvertChoice_EmptyElements(t *testing.T) {
	choice := &XSDChoice{
		Elements: []XSDElement{},
	}

	result, err := convertChoice(choice)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "choice must contain at least one element")
}

func TestCreateChoiceSchema_Success(t *testing.T) {
	elements := []XSDElement{
		{Name: "email", Type: "xs:string"},
		{Name: "phone", Type: "xs:string"},
	}

	result, err := createChoiceSchema(elements)
	require.NoError(t, err)
	assert.NotNil(t, result)

	oneOf := result["oneOf"].([]map[string]interface{})
	assert.Len(t, oneOf, 2)

	// Check first option
	emailOption := oneOf[0]
	assert.Equal(t, "object", emailOption["type"])
	assert.Equal(t, false, emailOption["additionalProperties"])

	emailProps := emailOption["properties"].(map[string]interface{})
	emailProp := emailProps["email"].(map[string]interface{})
	assert.Equal(t, "string", emailProp["type"])

	emailRequired := emailOption["required"].([]string)
	assert.Contains(t, emailRequired, "email")

	// Check second option
	phoneOption := oneOf[1]
	assert.Equal(t, "object", phoneOption["type"])
	assert.Equal(t, false, phoneOption["additionalProperties"])

	phoneProps := phoneOption["properties"].(map[string]interface{})
	phoneProp := phoneProps["phone"].(map[string]interface{})
	assert.Equal(t, "string", phoneProp["type"])

	phoneRequired := phoneOption["required"].([]string)
	assert.Contains(t, phoneRequired, "phone")
}

// --- XSD All Feature Tests ---

func TestActivity_Eval_Success_SimpleAll(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with simple all (all elements can appear in any order)
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="person">
    <xs:complexType>
      <xs:all>
        <xs:element name="name" type="xs:string"/>
        <xs:element name="age" type="xs:integer"/>
        <xs:element name="email" type="xs:string"/>
      </xs:all>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify JSON Schema structure for all
	assert.Equal(t, "https://json-schema.org/draft/2020-12/schema", jsonSchema["$schema"])
	assert.Equal(t, "object", jsonSchema["type"])
	assert.Equal(t, false, jsonSchema["additionalProperties"])

	properties := jsonSchema["properties"].(map[string]interface{})
	assert.Len(t, properties, 3)

	// Check individual properties
	nameProperty := properties["name"].(map[string]interface{})
	assert.Equal(t, "string", nameProperty["type"])

	ageProperty := properties["age"].(map[string]interface{})
	assert.Equal(t, "integer", ageProperty["type"])

	emailProperty := properties["email"].(map[string]interface{})
	assert.Equal(t, "string", emailProperty["type"])

	// All elements should be required by default in xs:all
	required := jsonSchema["required"].([]interface{})
	assert.Contains(t, required, "name")
	assert.Contains(t, required, "age")
	assert.Contains(t, required, "email")
}

func TestActivity_Eval_Success_AllWithOptionalElements(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with all elements where some are optional (minOccurs="0")
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="contact">
    <xs:complexType>
      <xs:all>
        <xs:element name="name" type="xs:string"/>
        <xs:element name="email" type="xs:string" minOccurs="0"/>
        <xs:element name="phone" type="xs:string" minOccurs="0"/>
        <xs:element name="address" type="xs:string"/>
      </xs:all>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify JSON Schema structure
	assert.Equal(t, "object", jsonSchema["type"])
	assert.Equal(t, false, jsonSchema["additionalProperties"])

	properties := jsonSchema["properties"].(map[string]interface{})
	assert.Len(t, properties, 4)

	// Check required fields (only name and address should be required)
	required := jsonSchema["required"].([]interface{})
	assert.Contains(t, required, "name")
	assert.Contains(t, required, "address")
	assert.NotContains(t, required, "email")
	assert.NotContains(t, required, "phone")
	assert.Len(t, required, 2)
}

func TestActivity_Eval_Success_AllWithDifferentTypes(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with all elements of different data types
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="product">
    <xs:complexType>
      <xs:all>
        <xs:element name="name" type="xs:string"/>
        <xs:element name="price" type="xs:decimal"/>
        <xs:element name="inStock" type="xs:boolean"/>
        <xs:element name="quantity" type="xs:integer"/>
        <xs:element name="manufactureDate" type="xs:date"/>
      </xs:all>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify different data types in all construct
	properties := jsonSchema["properties"].(map[string]interface{})
	assert.Len(t, properties, 5)

	// Verify data types
	nameProperty := properties["name"].(map[string]interface{})
	assert.Equal(t, "string", nameProperty["type"])

	priceProperty := properties["price"].(map[string]interface{})
	assert.Equal(t, "number", priceProperty["type"])

	inStockProperty := properties["inStock"].(map[string]interface{})
	assert.Equal(t, "boolean", inStockProperty["type"])

	quantityProperty := properties["quantity"].(map[string]interface{})
	assert.Equal(t, "integer", quantityProperty["type"])

	manufactureDateProperty := properties["manufactureDate"].(map[string]interface{})
	assert.Equal(t, "string", manufactureDateProperty["type"])
	assert.Equal(t, "date", manufactureDateProperty["format"])
}

func TestActivity_Eval_Error_EmptyAll(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with empty all (should cause error)
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="empty">
    <xs:complexType>
      <xs:all>
        <!-- No elements defined -->
      </xs:all>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.Error(t, err)
	assert.False(t, done)

	// Verify error outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.True(t, errorFlag)
	assert.Empty(t, jsonSchemaStr)
	assert.Contains(t, errorMsg, "CONVERSION_ERROR")
	assert.Contains(t, errorMsg, "all must contain at least one element")
}

func TestConvertAll_EmptyElements(t *testing.T) {
	all := &XSDAll{
		Elements: []XSDElement{},
	}

	result, err := convertAll(all)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "all must contain at least one element")
}

func TestConvertAll_Success(t *testing.T) {
	all := &XSDAll{
		Elements: []XSDElement{
			{Name: "name", Type: "xs:string"},
			{Name: "age", Type: "xs:integer"},
			{Name: "email", Type: "xs:string", MinOccurs: "0"}, // Optional element
		},
	}

	result, err := convertAll(all)
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify object type
	assert.Equal(t, "object", result["type"])
	assert.Equal(t, false, result["additionalProperties"])

	// Verify properties
	properties := result["properties"].(map[string]interface{})
	assert.Len(t, properties, 3)

	nameProperty := properties["name"].(map[string]interface{})
	assert.Equal(t, "string", nameProperty["type"])

	ageProperty := properties["age"].(map[string]interface{})
	assert.Equal(t, "integer", ageProperty["type"])

	emailProperty := properties["email"].(map[string]interface{})
	assert.Equal(t, "string", emailProperty["type"])

	// Verify required fields (name and age should be required, email should not)
	required := result["required"].([]string)
	assert.Contains(t, required, "name")
	assert.Contains(t, required, "age")
	assert.NotContains(t, required, "email")
	assert.Len(t, required, 2)
}

func TestActivity_Eval_Success_MixedConstructs(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with mixed constructs: sequence containing all and choice
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
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
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify root structure
	assert.Equal(t, "https://json-schema.org/draft/2020-12/schema", jsonSchema["$schema"])
	assert.Equal(t, "object", jsonSchema["type"])

	properties := jsonSchema["properties"].(map[string]interface{})
	assert.Len(t, properties, 3) // personalInfo, contact, skills

	// Verify personalInfo (all construct)
	personalInfo := properties["personalInfo"].(map[string]interface{})
	assert.Equal(t, "object", personalInfo["type"])
	assert.Equal(t, false, personalInfo["additionalProperties"])

	personalInfoProps := personalInfo["properties"].(map[string]interface{})
	assert.Len(t, personalInfoProps, 3)
	assert.Contains(t, personalInfoProps, "firstName")
	assert.Contains(t, personalInfoProps, "lastName")
	assert.Contains(t, personalInfoProps, "middleName")

	personalInfoRequired := personalInfo["required"].([]interface{})
	assert.Contains(t, personalInfoRequired, "firstName")
	assert.Contains(t, personalInfoRequired, "lastName")
	assert.NotContains(t, personalInfoRequired, "middleName")

	// Verify contact (choice construct)
	contact := properties["contact"].(map[string]interface{})
	oneOf := contact["oneOf"].([]interface{})
	assert.Len(t, oneOf, 2)

	// Verify skills (array)
	skills := properties["skills"].(map[string]interface{})
	assert.Equal(t, "array", skills["type"])
	skillsItems := skills["items"].(map[string]interface{})
	assert.Equal(t, "string", skillsItems["type"])
}

// --- XSD Group Feature Tests ---

func TestActivity_Eval_Success_SimpleGroup(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with named group definition and group reference
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:group name="personGroup">
    <xs:sequence>
      <xs:element name="firstName" type="xs:string"/>
      <xs:element name="lastName" type="xs:string"/>
    </xs:sequence>
  </xs:group>
  
  <xs:element name="employee">
    <xs:complexType>
      <xs:sequence>
        <xs:group ref="personGroup"/>
        <xs:element name="employeeId" type="xs:integer"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify root structure
	assert.Equal(t, "https://json-schema.org/draft/2020-12/schema", jsonSchema["$schema"])
	assert.Equal(t, "object", jsonSchema["type"])

	properties := jsonSchema["properties"].(map[string]interface{})
	assert.Len(t, properties, 3) // firstName, lastName (from group), employeeId

	// Verify group elements were flattened into the sequence
	assert.Contains(t, properties, "firstName")
	assert.Contains(t, properties, "lastName")
	assert.Contains(t, properties, "employeeId")

	// Verify types
	firstName := properties["firstName"].(map[string]interface{})
	assert.Equal(t, "string", firstName["type"])

	lastName := properties["lastName"].(map[string]interface{})
	assert.Equal(t, "string", lastName["type"])

	employeeId := properties["employeeId"].(map[string]interface{})
	assert.Equal(t, "integer", employeeId["type"])

	// All elements should be required
	required := jsonSchema["required"].([]interface{})
	assert.Contains(t, required, "firstName")
	assert.Contains(t, required, "lastName")
	assert.Contains(t, required, "employeeId")
}

func TestActivity_Eval_Success_GroupWithChoice(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with group containing choice
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:group name="contactGroup">
    <xs:choice>
      <xs:element name="email" type="xs:string"/>
      <xs:element name="phone" type="xs:string"/>
      <xs:element name="address" type="xs:string"/>
    </xs:choice>
  </xs:group>
  
  <xs:element name="person">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="name" type="xs:string"/>
        <xs:group ref="contactGroup"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify root structure
	assert.Equal(t, "object", jsonSchema["type"])

	properties := jsonSchema["properties"].(map[string]interface{})
	assert.Len(t, properties, 2) // name and group_choice_contactGroup

	// Verify name element
	assert.Contains(t, properties, "name")
	nameProperty := properties["name"].(map[string]interface{})
	assert.Equal(t, "string", nameProperty["type"])

	// Verify the group choice element
	groupChoiceKey := "group_choice_contactGroup"
	assert.Contains(t, properties, groupChoiceKey)
	groupChoice := properties[groupChoiceKey].(map[string]interface{})

	// The group choice should be a oneOf construct
	oneOf := groupChoice["oneOf"].([]interface{})
	assert.Len(t, oneOf, 3) // email, phone, address

	// Verify choice options
	expectedOptions := map[string]string{
		"email":   "string",
		"phone":   "string",
		"address": "string",
	}

	for _, option := range oneOf {
		optionMap := option.(map[string]interface{})
		assert.Equal(t, "object", optionMap["type"])
		assert.Equal(t, false, optionMap["additionalProperties"])

		optionProps := optionMap["properties"].(map[string]interface{})
		assert.Len(t, optionProps, 1)

		// Find which option this is
		for propName, propDef := range optionProps {
			propDefMap := propDef.(map[string]interface{})
			expectedType, exists := expectedOptions[propName]
			assert.True(t, exists, "Unexpected property: %s", propName)
			assert.Equal(t, expectedType, propDefMap["type"])
		}
	}
}

func TestActivity_Eval_Success_NestedGroups(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with nested group references
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:group name="nameGroup">
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
        <xs:group ref="nameGroup"/>
        <xs:group ref="addressGroup"/>
        <xs:element name="customerId" type="xs:integer"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify root structure
	assert.Equal(t, "object", jsonSchema["type"])

	properties := jsonSchema["properties"].(map[string]interface{})
	assert.Len(t, properties, 5) // firstName, lastName, street, city, customerId

	// Verify all elements from both groups are flattened
	expectedElements := map[string]string{
		"firstName":  "string",
		"lastName":   "string",
		"street":     "string",
		"city":       "string",
		"customerId": "integer",
	}

	for elemName, expectedType := range expectedElements {
		assert.Contains(t, properties, elemName)
		element := properties[elemName].(map[string]interface{})
		assert.Equal(t, expectedType, element["type"])
	}

	// All elements should be required
	required := jsonSchema["required"].([]interface{})
	assert.Len(t, required, 5)
	for elemName := range expectedElements {
		assert.Contains(t, required, elemName)
	}
}

func TestActivity_Eval_Error_GroupReferenceNotFound(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with group reference to non-existent group
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="person">
    <xs:complexType>
      <xs:sequence>
        <xs:group ref="nonExistentGroup"/>
        <xs:element name="name" type="xs:string"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.Error(t, err)
	assert.False(t, done)

	// Verify error outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.True(t, errorFlag)
	assert.Empty(t, jsonSchemaStr)
	assert.Contains(t, errorMsg, "CONVERSION_ERROR")
	assert.Contains(t, errorMsg, "group reference 'nonExistentGroup' not found")
}

func TestActivity_Eval_Success_GroupWithMinMaxOccurs(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with group reference having minOccurs and maxOccurs
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:group name="itemGroup">
    <xs:sequence>
      <xs:element name="name" type="xs:string"/>
      <xs:element name="value" type="xs:integer"/>
    </xs:sequence>
  </xs:group>
  
  <xs:element name="container">
    <xs:complexType>
      <xs:sequence>
        <xs:group ref="itemGroup" minOccurs="0" maxOccurs="unbounded"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// For this test, we expect the group elements to be flattened into the sequence
	// The implementation may vary based on how we handle group cardinality
	assert.Equal(t, "object", jsonSchema["type"])

	// The exact structure depends on implementation details,
	// but we should have some properties from the group
	properties := jsonSchema["properties"].(map[string]interface{})
	assert.True(t, len(properties) > 0, "Should have some properties from group resolution")
}

// Unit tests for group resolution functions

func TestResolveGroupRef_Success(t *testing.T) {
	schema := &XSDSchema{
		Groups: []XSDGroup{
			{
				Name: "testGroup",
				Sequence: &XSDSequence{
					Elements: []XSDElement{
						{Name: "element1", Type: "xs:string"},
					},
				},
			},
		},
	}

	groupRef := &XSDGroupRef{Ref: "testGroup"}

	group, err := resolveGroupRef(groupRef, schema)
	assert.NoError(t, err)
	assert.NotNil(t, group)
	assert.Equal(t, "testGroup", group.Name)
	assert.NotNil(t, group.Sequence)
	assert.Len(t, group.Sequence.Elements, 1)
	assert.Equal(t, "element1", group.Sequence.Elements[0].Name)
}

func TestResolveGroupRef_NotFound(t *testing.T) {
	schema := &XSDSchema{
		Groups: []XSDGroup{},
	}

	groupRef := &XSDGroupRef{Ref: "nonExistentGroup"}

	group, err := resolveGroupRef(groupRef, schema)
	assert.Error(t, err)
	assert.Nil(t, group)
	assert.Contains(t, err.Error(), "group reference 'nonExistentGroup' not found")
}

func TestResolveGroupRef_EmptyRef(t *testing.T) {
	schema := &XSDSchema{
		Groups: []XSDGroup{},
	}

	groupRef := &XSDGroupRef{Ref: ""}

	group, err := resolveGroupRef(groupRef, schema)
	assert.Error(t, err)
	assert.Nil(t, group)
	assert.Contains(t, err.Error(), "group reference must have a 'ref' attribute")
}

func TestFlattenGroup_WithSequence(t *testing.T) {
	schema := &XSDSchema{}

	group := &XSDGroup{
		Name: "testGroup",
		Sequence: &XSDSequence{
			Elements: []XSDElement{
				{Name: "element1", Type: "xs:string"},
				{Name: "element2", Type: "xs:integer"},
			},
		},
	}

	groupRef := &XSDGroupRef{Ref: "testGroup"}

	elements, err := flattenGroup(group, groupRef, schema)
	assert.NoError(t, err)
	assert.Len(t, elements, 2)
	assert.Equal(t, "element1", elements[0].Name)
	assert.Equal(t, "element2", elements[1].Name)
}

func TestFlattenGroup_WithChoice(t *testing.T) {
	schema := &XSDSchema{}

	group := &XSDGroup{
		Name: "choiceGroup",
		Choice: &XSDChoice{
			Elements: []XSDElement{
				{Name: "option1", Type: "xs:string"},
				{Name: "option2", Type: "xs:integer"},
			},
		},
	}

	groupRef := &XSDGroupRef{Ref: "choiceGroup"}

	elements, err := flattenGroup(group, groupRef, schema)
	assert.NoError(t, err)
	assert.Len(t, elements, 1) // Should create a wrapper element for the choice
	assert.Equal(t, "group_choice_choiceGroup", elements[0].Name)
	assert.NotNil(t, elements[0].ComplexType)
	assert.NotNil(t, elements[0].ComplexType.Choice)
}

// --- XSD Simple Restrictions Feature Tests ---

func TestActivity_Eval_Success_StringPatternRestriction(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with string pattern restriction (email regex)
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="email">
    <xs:simpleType>
      <xs:restriction base="xs:string">
        <xs:pattern value="[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}"/>
      </xs:restriction>
    </xs:simpleType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify JSON Schema structure for pattern restriction
	assert.Equal(t, "https://json-schema.org/draft/2020-12/schema", jsonSchema["$schema"])
	assert.Equal(t, "string", jsonSchema["type"])
	assert.Equal(t, "[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}", jsonSchema["pattern"])
}

func TestActivity_Eval_Success_StringLengthRestrictions(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with string length restrictions
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="username">
    <xs:simpleType>
      <xs:restriction base="xs:string">
        <xs:minLength value="3"/>
        <xs:maxLength value="20"/>
      </xs:restriction>
    </xs:simpleType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify JSON Schema structure for length restrictions
	assert.Equal(t, "string", jsonSchema["type"])
	assert.Equal(t, float64(3), jsonSchema["minLength"]) // JSON unmarshals numbers as float64
	assert.Equal(t, float64(20), jsonSchema["maxLength"])
}

func TestActivity_Eval_Success_StringEnumerationRestriction(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with string enumeration restriction
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="color">
    <xs:simpleType>
      <xs:restriction base="xs:string">
        <xs:enumeration value="red"/>
        <xs:enumeration value="green"/>
        <xs:enumeration value="blue"/>
      </xs:restriction>
    </xs:simpleType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify JSON Schema structure for enumeration restriction
	assert.Equal(t, "string", jsonSchema["type"])
	enumValues := jsonSchema["enum"].([]interface{})
	assert.Len(t, enumValues, 3)
	assert.Contains(t, enumValues, "red")
	assert.Contains(t, enumValues, "green")
	assert.Contains(t, enumValues, "blue")
}

func TestActivity_Eval_Success_IntegerEnumerationRestriction(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with integer enumeration restriction
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="priority">
    <xs:simpleType>
      <xs:restriction base="xs:integer">
        <xs:enumeration value="1"/>
        <xs:enumeration value="2"/>
        <xs:enumeration value="3"/>
      </xs:restriction>
    </xs:simpleType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify JSON Schema structure for integer enumeration restriction
	assert.Equal(t, "integer", jsonSchema["type"])
	enumValues := jsonSchema["enum"].([]interface{})
	assert.Len(t, enumValues, 3)
	// Check that values are converted to integers
	assert.Contains(t, enumValues, float64(1)) // JSON unmarshals numbers as float64
	assert.Contains(t, enumValues, float64(2))
	assert.Contains(t, enumValues, float64(3))
}

func TestActivity_Eval_Success_NumericRangeRestrictions(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with numeric range restrictions
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="age">
    <xs:simpleType>
      <xs:restriction base="xs:integer">
        <xs:minInclusive value="0"/>
        <xs:maxInclusive value="120"/>
      </xs:restriction>
    </xs:simpleType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify JSON Schema structure for numeric range restrictions
	assert.Equal(t, "integer", jsonSchema["type"])
	assert.Equal(t, float64(0), jsonSchema["minimum"])
	assert.Equal(t, float64(120), jsonSchema["maximum"])
}

func TestActivity_Eval_Success_NumericExclusiveRestrictions(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with exclusive numeric restrictions
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="temperature">
    <xs:simpleType>
      <xs:restriction base="xs:decimal">
        <xs:minExclusive value="-273.15"/>
        <xs:maxExclusive value="1000.0"/>
      </xs:restriction>
    </xs:simpleType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify JSON Schema structure for exclusive numeric restrictions
	assert.Equal(t, "number", jsonSchema["type"])
	assert.Equal(t, -273.15, jsonSchema["exclusiveMinimum"])
	assert.Equal(t, 1000.0, jsonSchema["exclusiveMaximum"])
}

func TestActivity_Eval_Success_DigitRestrictions(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with totalDigits and fractionDigits restrictions
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="price">
    <xs:simpleType>
      <xs:restriction base="xs:decimal">
        <xs:totalDigits value="8"/>
        <xs:fractionDigits value="2"/>
      </xs:restriction>
    </xs:simpleType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify JSON Schema structure for digit restrictions (using custom properties)
	assert.Equal(t, "number", jsonSchema["type"])
	assert.Equal(t, float64(8), jsonSchema["x-totalDigits"])
	assert.Equal(t, float64(2), jsonSchema["x-fractionDigits"])
}

func TestActivity_Eval_Success_MultiplePatternRestrictions(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with multiple pattern restrictions (should be combined with alternation)
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="phoneOrEmail">
    <xs:simpleType>
      <xs:restriction base="xs:string">
        <xs:pattern value="[0-9]{3}-[0-9]{3}-[0-9]{4}"/>
        <xs:pattern value="[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}"/>
      </xs:restriction>
    </xs:simpleType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify JSON Schema structure for multiple patterns (combined with alternation)
	assert.Equal(t, "string", jsonSchema["type"])
	expectedPattern := "([0-9]{3}-[0-9]{3}-[0-9]{4})|([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,})"
	assert.Equal(t, expectedPattern, jsonSchema["pattern"])
}

func TestActivity_Eval_Success_CombinedRestrictions(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with multiple types of restrictions combined
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="productCode">
    <xs:simpleType>
      <xs:restriction base="xs:string">
        <xs:pattern value="[A-Z]{2}[0-9]{4}"/>
        <xs:minLength value="6"/>
        <xs:maxLength value="6"/>
      </xs:restriction>
    </xs:simpleType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify JSON Schema structure with combined restrictions
	assert.Equal(t, "string", jsonSchema["type"])
	assert.Equal(t, "[A-Z]{2}[0-9]{4}", jsonSchema["pattern"])
	assert.Equal(t, float64(6), jsonSchema["minLength"])
	assert.Equal(t, float64(6), jsonSchema["maxLength"])
}

func TestActivity_Eval_Success_BooleanEnumerationRestriction(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with boolean enumeration restriction
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="enabled">
    <xs:simpleType>
      <xs:restriction base="xs:boolean">
        <xs:enumeration value="true"/>
        <xs:enumeration value="false"/>
      </xs:restriction>
    </xs:simpleType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify JSON Schema structure for boolean enumeration restriction
	assert.Equal(t, "boolean", jsonSchema["type"])
	enumValues := jsonSchema["enum"].([]interface{})
	assert.Len(t, enumValues, 2)
	assert.Contains(t, enumValues, true)
	assert.Contains(t, enumValues, false)
}

func TestActivity_Eval_Error_InvalidRestrictionValues(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with invalid minLength value (non-numeric)
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="test">
    <xs:simpleType>
      <xs:restriction base="xs:string">
        <xs:minLength value="invalid"/>
      </xs:restriction>
    </xs:simpleType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.Error(t, err)
	assert.False(t, done)

	// Verify error outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.True(t, errorFlag)
	assert.Empty(t, jsonSchemaStr)
	assert.Contains(t, errorMsg, "CONVERSION_ERROR")
	assert.Contains(t, errorMsg, "failed to apply restrictions")
	assert.Contains(t, errorMsg, "invalid minLength value")
}

func TestActivity_Eval_Success_ComplexTypeWithRestrictions(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with complex type containing elements with restrictions
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="user">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="username">
          <xs:simpleType>
            <xs:restriction base="xs:string">
              <xs:minLength value="3"/>
              <xs:maxLength value="20"/>
              <xs:pattern value="[a-zA-Z0-9_]+"/>
            </xs:restriction>
          </xs:simpleType>
        </xs:element>
        <xs:element name="age">
          <xs:simpleType>
            <xs:restriction base="xs:integer">
              <xs:minInclusive value="13"/>
              <xs:maxInclusive value="100"/>
            </xs:restriction>
          </xs:simpleType>
        </xs:element>
        <xs:element name="role">
          <xs:simpleType>
            <xs:restriction base="xs:string">
              <xs:enumeration value="admin"/>
              <xs:enumeration value="user"/>
              <xs:enumeration value="guest"/>
            </xs:restriction>
          </xs:simpleType>
        </xs:element>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse and validate the JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify JSON Schema structure for complex type with restrictions
	assert.Equal(t, "object", jsonSchema["type"])

	properties := jsonSchema["properties"].(map[string]interface{})
	assert.Len(t, properties, 3)

	// Check username property with restrictions
	username := properties["username"].(map[string]interface{})
	assert.Equal(t, "string", username["type"])
	assert.Equal(t, float64(3), username["minLength"])
	assert.Equal(t, float64(20), username["maxLength"])
	assert.Equal(t, "[a-zA-Z0-9_]+", username["pattern"])

	// Check age property with numeric restrictions
	age := properties["age"].(map[string]interface{})
	assert.Equal(t, "integer", age["type"])
	assert.Equal(t, float64(13), age["minimum"])
	assert.Equal(t, float64(100), age["maximum"])

	// Check role property with enumeration restriction
	role := properties["role"].(map[string]interface{})
	assert.Equal(t, "string", role["type"])
	enumValues := role["enum"].([]interface{})
	assert.Len(t, enumValues, 3)
	assert.Contains(t, enumValues, "admin")
	assert.Contains(t, enumValues, "user")
	assert.Contains(t, enumValues, "guest")
}

// Unit tests for restriction helper functions

func TestApplyRestrictions_Pattern(t *testing.T) {
	prop := make(map[string]interface{})
	prop["type"] = "string"

	restriction := &XSDRestriction{
		Patterns: []XSDPattern{
			{Value: "[0-9]+"},
		},
	}

	err := applyRestrictions(prop, restriction)
	assert.NoError(t, err)
	assert.Equal(t, "[0-9]+", prop["pattern"])
}

func TestApplyRestrictions_MultiplePatterns(t *testing.T) {
	prop := make(map[string]interface{})
	prop["type"] = "string"

	restriction := &XSDRestriction{
		Patterns: []XSDPattern{
			{Value: "[0-9]+"},
			{Value: "[a-zA-Z]+"},
		},
	}

	err := applyRestrictions(prop, restriction)
	assert.NoError(t, err)
	assert.Equal(t, "([0-9]+)|([a-zA-Z]+)", prop["pattern"])
}

func TestApplyRestrictions_Enumeration(t *testing.T) {
	prop := make(map[string]interface{})
	prop["type"] = "string"

	restriction := &XSDRestriction{
		Enumerations: []XSDEnumeration{
			{Value: "red"},
			{Value: "green"},
			{Value: "blue"},
		},
	}

	err := applyRestrictions(prop, restriction)
	assert.NoError(t, err)

	enumValues := prop["enum"].([]interface{})
	assert.Len(t, enumValues, 3)
	assert.Contains(t, enumValues, "red")
	assert.Contains(t, enumValues, "green")
	assert.Contains(t, enumValues, "blue")
}

func TestApplyRestrictions_LengthRestrictions(t *testing.T) {
	prop := make(map[string]interface{})
	prop["type"] = "string"

	restriction := &XSDRestriction{
		MinLength: &XSDFacet{Value: "5"},
		MaxLength: &XSDFacet{Value: "10"},
	}

	err := applyRestrictions(prop, restriction)
	assert.NoError(t, err)
	assert.Equal(t, 5, prop["minLength"])
	assert.Equal(t, 10, prop["maxLength"])
}

func TestApplyRestrictions_NumericRestrictions(t *testing.T) {
	prop := make(map[string]interface{})
	prop["type"] = "integer"

	restriction := &XSDRestriction{
		MinInclusive: &XSDFacet{Value: "0"},
		MaxInclusive: &XSDFacet{Value: "100"},
	}

	err := applyRestrictions(prop, restriction)
	assert.NoError(t, err)
	assert.Equal(t, 0, prop["minimum"])
	assert.Equal(t, 100, prop["maximum"])
}

func TestApplyRestrictions_ExclusiveRestrictions(t *testing.T) {
	prop := make(map[string]interface{})
	prop["type"] = "number"

	restriction := &XSDRestriction{
		MinExclusive: &XSDFacet{Value: "0.0"},
		MaxExclusive: &XSDFacet{Value: "100.5"},
	}

	err := applyRestrictions(prop, restriction)
	assert.NoError(t, err)
	assert.Equal(t, 0.0, prop["exclusiveMinimum"])
	assert.Equal(t, 100.5, prop["exclusiveMaximum"])
}

func TestApplyRestrictions_DigitRestrictions(t *testing.T) {
	prop := make(map[string]interface{})
	prop["type"] = "number"

	restriction := &XSDRestriction{
		TotalDigits:    &XSDFacet{Value: "8"},
		FractionDigits: &XSDFacet{Value: "2"},
	}

	err := applyRestrictions(prop, restriction)
	assert.NoError(t, err)
	assert.Equal(t, 8, prop["x-totalDigits"])
	assert.Equal(t, 2, prop["x-fractionDigits"])
}

func TestApplyRestrictions_InvalidMinLength(t *testing.T) {
	prop := make(map[string]interface{})
	prop["type"] = "string"

	restriction := &XSDRestriction{
		MinLength: &XSDFacet{Value: "invalid"},
	}

	err := applyRestrictions(prop, restriction)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid minLength value")
}

func TestConvertEnumValueToType_Integer(t *testing.T) {
	result := convertEnumValueToType("42", "integer")
	assert.Equal(t, 42, result)

	// Invalid integer should default to string
	result = convertEnumValueToType("invalid", "integer")
	assert.Equal(t, "invalid", result)
}

func TestConvertEnumValueToType_Number(t *testing.T) {
	result := convertEnumValueToType("42.5", "number")
	assert.Equal(t, 42.5, result)

	// Invalid number should default to string
	result = convertEnumValueToType("invalid", "number")
	assert.Equal(t, "invalid", result)
}

func TestConvertEnumValueToType_Boolean(t *testing.T) {
	result := convertEnumValueToType("true", "boolean")
	assert.Equal(t, true, result)

	result = convertEnumValueToType("false", "boolean")
	assert.Equal(t, false, result)

	// Invalid boolean should default to string
	result = convertEnumValueToType("invalid", "boolean")
	assert.Equal(t, "invalid", result)
}

func TestConvertNumericValue_Integer(t *testing.T) {
	result, err := convertNumericValue("42")
	assert.NoError(t, err)
	assert.Equal(t, 42, result)
}

func TestConvertNumericValue_Float(t *testing.T) {
	result, err := convertNumericValue("42.5")
	assert.NoError(t, err)
	assert.Equal(t, 42.5, result)
}

func TestConvertNumericValue_Invalid(t *testing.T) {
	result, err := convertNumericValue("invalid")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "invalid numeric value")
}

// Phase 5: Complex Restrictions Tests

func TestActivity_Eval_Success_Phase5_NamedSimpleType(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" 
           targetNamespace="http://example.com/test" 
           xmlns:tns="http://example.com/test">
    
    <xs:simpleType name="ProductCodeType">
        <xs:restriction base="xs:string">
            <xs:pattern value="[A-Z]{3}-[0-9]{4}"/>
            <xs:minLength value="8"/>
            <xs:maxLength value="8"/>
        </xs:restriction>
    </xs:simpleType>
    
    <xs:element name="product">
        <xs:complexType>
            <xs:sequence>
                <xs:element name="code" type="tns:ProductCodeType"/>
                <xs:element name="name" type="xs:string"/>
            </xs:sequence>
        </xs:complexType>
    </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Verify the JSON Schema structure
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	assert.NoError(t, err)

	// Check that the named type restrictions are applied
	// The element "product" becomes the root object, so we look directly at properties
	properties, ok := jsonSchema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("properties is not a map[string]interface{}, got: %T", jsonSchema["properties"])
	}

	// Look for the "code" field directly (since product becomes the root)
	codeField, ok := properties["code"].(map[string]interface{})
	if !ok {
		t.Fatalf("code field is not a map[string]interface{}, got: %T", properties["code"])
	}

	assert.Equal(t, "string", codeField["type"])
	assert.Equal(t, "[A-Z]{3}-[0-9]{4}", codeField["pattern"])
	assert.Equal(t, float64(8), codeField["minLength"])
	assert.Equal(t, float64(8), codeField["maxLength"])
}

func TestActivity_Eval_Success_Phase5_NamedComplexType(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" 
           targetNamespace="http://example.com/test" 
           xmlns:tns="http://example.com/test">
    
    <xs:complexType name="AddressType">
        <xs:sequence>
            <xs:element name="street" type="xs:string"/>
            <xs:element name="city" type="xs:string"/>
            <xs:element name="zipCode" type="xs:string"/>
        </xs:sequence>
    </xs:complexType>
    
    <xs:element name="customer">
        <xs:complexType>
            <xs:sequence>
                <xs:element name="name" type="xs:string"/>
                <xs:element name="address" type="tns:AddressType"/>
            </xs:sequence>
        </xs:complexType>
    </xs:element>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Verify the JSON Schema structure
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	assert.NoError(t, err)

	// Check that the named complex type is properly referenced
	// The element "customer" becomes the root object, so we look directly at properties
	properties := jsonSchema["properties"].(map[string]interface{})
	addressField := properties["address"].(map[string]interface{})
	addressProps := addressField["properties"].(map[string]interface{})

	assert.Equal(t, "object", addressField["type"])
	assert.Contains(t, addressProps, "street")
	assert.Contains(t, addressProps, "city")
	assert.Contains(t, addressProps, "zipCode")
}

func TestActivity_Eval_Success_Phase5_UnionType(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" 
           targetNamespace="http://example.com/test" 
           xmlns:tns="http://example.com/test">
    
    <xs:simpleType name="StringOrNumberType">
        <xs:union memberTypes="xs:string xs:integer"/>
    </xs:simpleType>
    
    <xs:element name="value" type="tns:StringOrNumberType"/>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Verify the JSON Schema structure
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	assert.NoError(t, err)

	// Check that union type is converted to oneOf (not anyOf as I originally expected)
	// The union type becomes the root schema, not a property
	oneOf, exists := jsonSchema["oneOf"]
	assert.True(t, exists)
	oneOfSlice := oneOf.([]interface{})
	assert.Len(t, oneOfSlice, 2)

	// Check the union members
	member1 := oneOfSlice[0].(map[string]interface{})
	member2 := oneOfSlice[1].(map[string]interface{})

	assert.Equal(t, "string", member1["type"])
	assert.Equal(t, "integer", member2["type"])
}

func TestActivity_Eval_Success_Phase5_ListType(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" 
           targetNamespace="http://example.com/test" 
           xmlns:tns="http://example.com/test">
    
    <xs:simpleType name="NumberListType">
        <xs:list itemType="xs:integer"/>
    </xs:simpleType>
    
    <xs:element name="numbers" type="tns:NumberListType"/>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Verify the JSON Schema structure
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	assert.NoError(t, err)

	// Check that list type is converted to array at root level
	// The list type becomes the root schema, not a property
	assert.Equal(t, "array", jsonSchema["type"])
	items := jsonSchema["items"].(map[string]interface{})
	assert.Equal(t, "integer", items["type"])
}

func TestActivity_Eval_Success_Phase5_ComplexExtension(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" 
           targetNamespace="http://example.com/test" 
           xmlns:tns="http://example.com/test">
    
    <xs:complexType name="PersonType">
        <xs:sequence>
            <xs:element name="name" type="xs:string"/>
            <xs:element name="age" type="xs:integer"/>
        </xs:sequence>
    </xs:complexType>
    
    <xs:complexType name="EmployeeType">
        <xs:complexContent>
            <xs:extension base="tns:PersonType">
                <xs:sequence>
                    <xs:element name="employeeId" type="xs:string"/>
                    <xs:element name="department" type="xs:string"/>
                </xs:sequence>
            </xs:extension>
        </xs:complexContent>
    </xs:complexType>
    
    <xs:element name="employee" type="tns:EmployeeType"/>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Verify the JSON Schema structure
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	assert.NoError(t, err)

	// Check that extension includes both base and extended properties
	// The element "employee" becomes the root object, so we look directly at properties
	properties := jsonSchema["properties"].(map[string]interface{})

	assert.Equal(t, "object", jsonSchema["type"])
	// Base type properties
	assert.Contains(t, properties, "name")
	assert.Contains(t, properties, "age")
	// Extended properties
	assert.Contains(t, properties, "employeeId")
	assert.Contains(t, properties, "department")
}

func TestActivity_Eval_Success_Phase5_SimpleContentExtension(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" 
           targetNamespace="http://example.com/test" 
           xmlns:tns="http://example.com/test">
    
    <xs:complexType name="PriceType">
        <xs:simpleContent>
            <xs:extension base="xs:decimal">
                <xs:attribute name="currency" type="xs:string"/>
                <xs:attribute name="taxIncluded" type="xs:boolean"/>
            </xs:extension>
        </xs:simpleContent>
    </xs:complexType>
    
    <xs:element name="price" type="tns:PriceType"/>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Verify the JSON Schema structure
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	assert.NoError(t, err)

	// Check that simple content extension creates object with value and attributes
	// The element "price" becomes the root object, so we look directly at properties
	properties := jsonSchema["properties"].(map[string]interface{})

	assert.Equal(t, "object", jsonSchema["type"])

	// Check value property
	valueField := properties["value"].(map[string]interface{})
	assert.Equal(t, "number", valueField["type"])

	// Check attribute properties
	assert.Contains(t, properties, "currency")
	assert.Contains(t, properties, "taxIncluded")

	currencyField := properties["currency"].(map[string]interface{})
	taxField := properties["taxIncluded"].(map[string]interface{})
	assert.Equal(t, "string", currencyField["type"])
	assert.Equal(t, "boolean", taxField["type"])
}

func TestActivity_Eval_Error_Phase5_MissingBaseType(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" 
           targetNamespace="http://example.com/test" 
           xmlns:tns="http://example.com/test">
    
    <xs:complexType name="ExtendedType">
        <xs:complexContent>
            <xs:extension base="tns:NonExistentType">
                <xs:sequence>
                    <xs:element name="newField" type="xs:string"/>
                </xs:sequence>
            </xs:extension>
        </xs:complexContent>
    </xs:complexType>
    
    <xs:element name="extended" type="tns:ExtendedType"/>
</xs:schema>`

	input := &Input{
		XSDString: xsdInput,
	}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.Error(t, err)
	assert.False(t, done)

	// Verify error message content
	assert.Contains(t, err.Error(), "failed to resolve base type")
}

// Test default and fixed values support
func TestActivity_Eval_Success_DefaultAndFixedValues(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with default and fixed values
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="product">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="name" type="xs:string" default="Unknown Product"/>
        <xs:element name="price" type="xs:decimal" default="0.00"/>
        <xs:element name="inStock" type="xs:boolean" default="true"/>
        <xs:element name="category" type="xs:string" fixed="electronics"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	input := &Input{XSDString: xsdInput}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify schema structure
	assert.Equal(t, "https://json-schema.org/draft/2020-12/schema", jsonSchema["$schema"])
	assert.Equal(t, "object", jsonSchema["type"])

	properties := jsonSchema["properties"].(map[string]interface{})

	// Check name element with default value
	nameSchema := properties["name"].(map[string]interface{})
	assert.Equal(t, "string", nameSchema["type"])
	assert.Equal(t, "Unknown Product", nameSchema["default"])

	// Check price element with default value
	priceSchema := properties["price"].(map[string]interface{})
	assert.Equal(t, "number", priceSchema["type"])
	assert.Equal(t, 0.0, priceSchema["default"])

	// Check inStock element with boolean default
	inStockSchema := properties["inStock"].(map[string]interface{})
	assert.Equal(t, "boolean", inStockSchema["type"])
	assert.Equal(t, true, inStockSchema["default"])

	// Check category element with fixed value (should have both const and default)
	categorySchema := properties["category"].(map[string]interface{})
	assert.Equal(t, "string", categorySchema["type"])
	assert.Equal(t, "electronics", categorySchema["const"])
	assert.Equal(t, "electronics", categorySchema["default"])
}

// Test enhanced attribute support
func TestActivity_Eval_Success_EnhancedAttributes(t *testing.T) {
	act := &Activity{}
	tc := test.NewActivityContext(act.Metadata())

	// XSD with attributes having default values and required attributes
	xsdInput := `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="measurement">
    <xs:complexType>
      <xs:simpleContent>
        <xs:extension base="xs:decimal">
          <xs:attribute name="unit" type="xs:string" use="required"/>
          <xs:attribute name="precision" type="xs:integer" default="2"/>
          <xs:attribute name="system" type="xs:string" fixed="metric"/>
        </xs:extension>
      </xs:simpleContent>
    </xs:complexType>
  </xs:element>
</xs:schema>`

	input := &Input{XSDString: xsdInput}
	tc.SetInputObject(input)

	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

	// Verify outputs
	jsonSchemaStr := tc.GetOutput(ovJSONSchemaString).(string)
	errorFlag := tc.GetOutput(ovError).(bool)
	errorMsg := tc.GetOutput(ovErrorMessage).(string)

	assert.False(t, errorFlag)
	assert.Empty(t, errorMsg)
	assert.NotEmpty(t, jsonSchemaStr)

	// Parse JSON Schema
	var jsonSchema map[string]interface{}
	err = json.Unmarshal([]byte(jsonSchemaStr), &jsonSchema)
	require.NoError(t, err)

	// Verify schema structure
	properties := jsonSchema["properties"].(map[string]interface{})
	required := jsonSchema["required"].([]interface{})

	// Check that value is required (from simple content extension)
	assert.Contains(t, required, "value")

	// Check that unit attribute is required
	assert.Contains(t, required, "unit")

	// Check that precision and system are not required (have defaults)
	assert.NotContains(t, required, "precision")
	assert.NotContains(t, required, "system")

	// Check precision attribute with default
	precisionSchema := properties["precision"].(map[string]interface{})
	assert.Equal(t, "integer", precisionSchema["type"])
	assert.Equal(t, float64(2), precisionSchema["default"]) // JSON unmarshaling gives float64

	// Check system attribute with fixed value
	systemSchema := properties["system"].(map[string]interface{})
	assert.Equal(t, "string", systemSchema["type"])
	assert.Equal(t, "metric", systemSchema["const"])
	assert.Equal(t, "metric", systemSchema["default"])
}

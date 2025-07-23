package jsonschematoxsd

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/invopop/jsonschema"
	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/coerce"
)

// Constants for identifying inputs and outputs
const (
	ivJsonSchemaString = "jsonSchemaString"
	ivRootElementName  = "rootElementName"
	ivTargetNamespace  = "targetNamespace"
	ovXsdString        = "xsdString"
	ovError            = "error"
	ovErrorMessage     = "errorMessage"
)

// Activity is the structure for your activity. It's now empty as there are no static settings.
type Activity struct{}

// Ensure the Flogo framework can discover and register this activity
func init() {
	_ = activity.Register(&Activity{}, New)
}

// Metadata returns the activity's metadata.
func (a *Activity) Metadata() *activity.Metadata {
	return activity.ToMetadata(&Input{}, &Output{})
}

// New creates a new instance of the Activity.
func New(ctx activity.InitContext) (activity.Activity, error) {
	ctx.Logger().Debugf("Creating New JSON Schema to XSD Transformer Activity")
	// No settings to initialize, so we just return a new instance.
	return &Activity{}, nil
}

// Eval executes the main logic of the Activity.
func (a *Activity) Eval(ctx activity.Context) (done bool, err error) {
	logger := ctx.Logger()
	logger.Debugf("Executing JSON Schema to XSD Transformer Eval")

	// --- 1. Get All Inputs ---
	input, err := coerceAndValidateInputs(ctx)
	if err != nil {
		setErrorOutputs(ctx, err.Error(), "INVALID_INPUT")
		return true, nil
	}

	// --- 2. Perform Transformation Logic ---
	logger.Debug("Attempting to convert JSON Schema to XSD")

	var schema jsonschema.Schema
	if err := json.Unmarshal([]byte(input.JsonSchemaString), &schema); err != nil {
		logger.Errorf("Failed to parse input JSON Schema: %v", err)
		setErrorOutputs(ctx, fmt.Sprintf("Invalid JSON Schema provided: %v", err), "SCHEMA_PARSE_ERROR")
		return true, nil
	}

	xsdString, err := generateXSD(input, &schema)
	if err != nil {
		logger.Errorf("Failed to generate XSD from schema: %v", err)
		setErrorOutputs(ctx, fmt.Sprintf("Could not convert to XSD: %v", err), "XSD_CONVERSION_ERROR")
		return true, nil
	}

	// --- 3. Set Success Outputs ---
	logger.Info("Successfully converted JSON Schema to XSD.")
	ctx.SetOutput(ovXsdString, xsdString)
	ctx.SetOutput(ovError, false)
	ctx.SetOutput(ovErrorMessage, "")

	return true, nil
}

// coerceAndValidateInputs reads all inputs from the context and validates them.
func coerceAndValidateInputs(ctx activity.Context) (*Input, error) {
	input := &Input{}
	var err error

	input.JsonSchemaString, err = coerce.ToString(ctx.GetInput(ivJsonSchemaString))
	if err != nil || strings.TrimSpace(input.JsonSchemaString) == "" {
		return nil, fmt.Errorf("input 'jsonSchemaString' is required and cannot be empty")
	}

	input.RootElementName, err = coerce.ToString(ctx.GetInput(ivRootElementName))
	if err != nil || strings.TrimSpace(input.RootElementName) == "" {
		return nil, fmt.Errorf("input 'rootElementName' is required and cannot be empty")
	}

	// TargetNamespace is optional
	input.TargetNamespace, _ = coerce.ToString(ctx.GetInput(ivTargetNamespace))

	return input, nil
}

// --- XSD Generation Logic ---

// XSDElement represents an <xs:element>
type XSDElement struct {
	XMLName     xml.Name        `xml:"xs:element"`
	Name        string          `xml:"name,attr"`
	Type        string          `xml:"type,attr,omitempty"`
	MinOccurs   string          `xml:"minOccurs,attr,omitempty"`
	MaxOccurs   string          `xml:"maxOccurs,attr,omitempty"`
	ComplexType *XSDComplexType `xml:",omitempty"`
}

// XSDSequence represents an <xs:sequence>
type XSDSequence struct {
	XMLName  xml.Name     `xml:"xs:sequence"`
	Elements []XSDElement `xml:"xs:element"`
}

// XSDComplexType represents an <xs:complexType>
type XSDComplexType struct {
	XMLName  xml.Name     `xml:"xs:complexType"`
	Sequence *XSDSequence `xml:",omitempty"`
}

// XSDSchema represents the root <xs:schema> element
type XSDSchema struct {
	XMLName            xml.Name     `xml:"xs:schema"`
	ElementFormDefault string       `xml:"elementFormDefault,attr"`
	TargetNamespace    string       `xml:"targetNamespace,attr,omitempty"`
	XmlnsXs            string       `xml:"xmlns:xs,attr"`
	Elements           []XSDElement `xml:"xs:element"`
}

// generateXSD converts a parsed JSON schema into an XSD string
func generateXSD(input *Input, schema *jsonschema.Schema) (string, error) {
	if schema.Type != "object" {
		return "", fmt.Errorf("root of JSON schema must be of type 'object'")
	}

	rootElement, err := jsonSchemaToXSDElement(input.RootElementName, schema, schema.Required, true)
	if err != nil {
		return "", err
	}

	xsd := XSDSchema{
		ElementFormDefault: "qualified",
		TargetNamespace:    input.TargetNamespace,
		XmlnsXs:            "http://www.w3.org/2001/XMLSchema",
		Elements:           []XSDElement{*rootElement},
	}

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	encoder := xml.NewEncoder(&buf)
	encoder.Indent("", "  ")
	if err := encoder.Encode(xsd); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// jsonSchemaToXSDElement recursively converts a JSON schema property to an XSD element
func jsonSchemaToXSDElement(name string, schema *jsonschema.Schema, requiredProps []string, isRoot bool) (*XSDElement, error) {
	element := &XSDElement{Name: name}

	// The root element is always required (minOccurs=1), child elements depend on the 'required' array.
	if !isRoot {
		isOptional := true
		for _, req := range requiredProps {
			if req == name {
				isOptional = false
				break
			}
		}
		if isOptional {
			element.MinOccurs = "0"
		}
	}

	switch schema.Type {
	case "object":
		var childElements []XSDElement
		for pair := schema.Properties.Oldest(); pair != nil; pair = pair.Next() {
			propName := pair.Key
			propSchema := pair.Value
			// Child elements are never the root, so pass 'false'
			child, err := jsonSchemaToXSDElement(propName, propSchema, schema.Required, false)
			if err != nil {
				return nil, err
			}
			childElements = append(childElements, *child)
		}
		element.ComplexType = &XSDComplexType{
			Sequence: &XSDSequence{
				Elements: childElements,
			},
		}
	case "array":
		element.MaxOccurs = "unbounded"
		if schema.Items == nil {
			return nil, fmt.Errorf("array '%s' must have an 'items' definition", name)
		}
		// Convert the item type, but keep the parent element name.
		// The item itself is not the root, so pass 'false'.
		itemElement, err := jsonSchemaToXSDElement(name, schema.Items, nil, false)
		if err != nil {
			return nil, err
		}
		element.Type = itemElement.Type
		element.ComplexType = itemElement.ComplexType
	case "string":
		element.Type = "xs:string"
	case "number":
		element.Type = "xs:decimal"
	case "integer":
		element.Type = "xs:integer"
	case "boolean":
		element.Type = "xs:boolean"
	default:
		return nil, fmt.Errorf("unsupported JSON schema type: %s for property %s", schema.Type, name)
	}

	return element, nil
}

// setErrorOutputs is a helper function to set all error-related outputs at once.
func setErrorOutputs(ctx activity.Context, message, code string) {
	ctx.SetOutput(ovXsdString, "")
	ctx.SetOutput(ovError, true)
	ctx.SetOutput(ovErrorMessage, fmt.Sprintf("[%s] %s", code, message))
}

// --- Supporting Structs ---

// Input struct now holds all dynamic inputs
type Input struct {
	JsonSchemaString string `md:"jsonSchemaString,required"`
	RootElementName  string `md:"rootElementName,required"`
	TargetNamespace  string `md:"targetNamespace"`
}

// Output struct remains the same
type Output struct {
	XsdString    string `md:"xsdString"`
	Error        bool   `md:"error"`
	ErrorMessage string `md:"errorMessage"`
}

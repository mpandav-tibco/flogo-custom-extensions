package xsdtojsonschema

import (
    "encoding/json"
    "encoding/xml"
    "fmt"
    "strings"

    "github.com/project-flogo/core/activity"
    "github.com/project-flogo/core/data/coerce"
)

// Constants for identifying inputs and outputs
const (
    ivXsdString        = "xsdString"
    ovJsonSchemaString = "jsonSchemaString"
    ovError            = "error"
    ovErrorMessage     = "errorMessage"
)

// activityMd is the metadata for the activity.
var activityMd = activity.ToMetadata(&Input{}, &Output{})

// Activity is the structure for your activity.
type Activity struct{}

func init() {
    _ = activity.Register(&Activity{}, New)
}

// Metadata returns the activity's metadata.
func (a *Activity) Metadata() *activity.Metadata {
    return activityMd
}

// New creates a new instance of the Activity.
func New(ctx activity.InitContext) (activity.Activity, error) {
    return &Activity{}, nil
}

// Eval executes the main logic of the Activity.
func (a *Activity) Eval(ctx activity.Context) (done bool, err error) {
    logger := ctx.Logger()

    input := &Input{}
    err = ctx.GetInputObject(input)
    if err != nil {
        setErrorOutputs(ctx, err.Error(), "INVALID_INPUT")
        return true, nil
    }

    if strings.TrimSpace(input.XsdString) == "" {
        setErrorOutputs(ctx, "Input 'xsdString' is required and cannot be empty", "INVALID_INPUT_XsdString")
        return true, nil
    }

    logger.Debug("Attempting to convert XSD to JSON Schema")

    // Parse the XSD string using Go's standard XML library
    var schema XSDSchema
    err = xml.Unmarshal([]byte(input.XsdString), &schema)
    if err != nil {
        logger.Errorf("Failed to parse input XSD: %v", err)
        setErrorOutputs(ctx, fmt.Sprintf("Invalid XSD provided: %v", err), "XSD_PARSE_ERROR")
        return true, nil
    }

    // Convert the parsed XSD structure to a JSON Schema map
    jsonSchema, err := convertSchema(&schema)
    if err != nil {
        logger.Errorf("Failed to convert XSD structure to JSON Schema: %v", err)
        setErrorOutputs(ctx, fmt.Sprintf("Conversion failed: %v", err), "CONVERSION_ERROR")
        return true, nil
    }

    // Marshal the map into a pretty-printed JSON string
    jsonBytes, err := json.MarshalIndent(jsonSchema, "", "  ")
    if err != nil {
        logger.Errorf("Failed to marshal JSON Schema to string: %v", err)
        setErrorOutputs(ctx, fmt.Sprintf("Failed to generate JSON string: %v", err), "JSON_MARSHAL_ERROR")
        return true, nil
    }

    // --- Set Success Outputs ---
    logger.Info("Successfully converted XSD to JSON Schema.")
    ctx.SetOutput(ovJsonSchemaString, string(jsonBytes))
    ctx.SetOutput(ovError, false)
    ctx.SetOutput(ovErrorMessage, "")

    return true, nil
}

// --- XSD Structs for Parsing ---

type XSDSchema struct {
    XMLName  xml.Name     `xml:"schema"`
    Elements []XSDElement `xml:"element"`
}

type XSDElement struct {
    Name        string          `xml:"name,attr"`
    Type        string          `xml:"type,attr"`
    MinOccurs   string          `xml:"minOccurs,attr"`
    MaxOccurs   string          `xml:"maxOccurs,attr"`
    ComplexType *XSDComplexType `xml:"complexType"`
}

type XSDComplexType struct {
    Sequence *XSDSequence `xml:"sequence"`
}

type XSDSequence struct {
    Elements []XSDElement `xml:"element"`
}

// --- Conversion Logic ---

// convertSchema is the main conversion function.
func convertSchema(s *XSDSchema) (map[string]interface{}, error) {
    if len(s.Elements) == 0 {
        return nil, fmt.Errorf("XSD does not contain any root elements")
    }
    rootElement := s.Elements[0]
    
    // Convert the root element to get the main schema structure
    jsonSchema, err := convertElement(&rootElement)
    if err != nil {
        return nil, err
    }

    // FIX: Add the $schema keyword to the root of the generated object.
    jsonSchema["$schema"] = "http://json-schema.org/draft-07/schema#"

    return jsonSchema, nil
}

// convertElement recursively converts an XSD element to a JSON Schema property.
func convertElement(el *XSDElement) (map[string]interface{}, error) {
    prop := make(map[string]interface{})

    // Handle simple types
    if el.Type != "" {
        jsonType, format := mapXsdTypeToJsonSchemaType(el.Type)
        prop["type"] = jsonType
        if format != "" {
            prop["format"] = format
        }
    }

    // Handle complex types (nested objects or sequences)
    if el.ComplexType != nil {
        return convertComplexType(el.ComplexType)
    }

    // Handle arrays
    if el.MaxOccurs == "unbounded" {
        arrayProp := make(map[string]interface{})
        arrayProp["type"] = "array"

        itemEl := *el
        itemEl.MaxOccurs = "" // Reset for item definition
        items, err := convertElement(&itemEl)
        if err != nil {
            return nil, err
        }
        arrayProp["items"] = items
        return arrayProp, nil
    }

    return prop, nil
}

// convertComplexType converts an XSD complexType to a JSON Schema object.
func convertComplexType(ct *XSDComplexType) (map[string]interface{}, error) {
    obj := make(map[string]interface{})
    obj["type"] = "object"
    properties := make(map[string]interface{})
    var required []string

    if ct.Sequence != nil {
        for _, element := range ct.Sequence.Elements {
            // Create a copy for the recursive call to avoid pointer issues
            elemCopy := element
            prop, err := convertElement(&elemCopy)
            if err != nil {
                return nil, err
            }
            properties[element.Name] = prop
            if element.MinOccurs != "0" {
                required = append(required, element.Name)
            }
        }
    }

    if len(properties) > 0 {
        obj["properties"] = properties
    }
    if len(required) > 0 {
        obj["required"] = required
    }

    return obj, nil
}

// mapXsdTypeToJsonSchemaType converts XSD built-in types to JSON Schema types and formats.
func mapXsdTypeToJsonSchemaType(xsdType string) (string, string) {
    parts := strings.Split(xsdType, ":")
    if len(parts) == 2 {
        xsdType = parts[1]
    }

    switch xsdType {
    case "string", "normalizedString", "token", "NMTOKEN":
        return "string", ""
    case "date":
        return "string", "date"
    case "dateTime":
        return "string", "date-time"
    case "time":
        return "string", "time"
    case "duration", "anyURI":
        return "string", "uri"
    case "base64Binary":
        return "string", "byte"
    case "boolean":
        return "boolean", ""
    case "decimal", "double", "float":
        return "number", ""
    case "integer", "positiveInteger", "negativeInteger", "nonPositiveInteger", "nonNegativeInteger",
        "long", "int", "short", "byte", "unsignedLong", "unsignedInt", "unsignedShort", "unsignedByte":
        return "integer", ""
    default:
        return "string", ""
    }
}

// setErrorOutputs is a helper function to set all error-related outputs at once.
func setErrorOutputs(ctx activity.Context, message, code string) {
    ctx.SetOutput(ovJsonSchemaString, "")
    ctx.SetOutput(ovError, true)
    ctx.SetOutput(ovErrorMessage, fmt.Sprintf("[%s] %s", code, message))
}

// --- Supporting Structs ---

type Input struct {
    XsdString string `md:"xsdString"`
}

func (i *Input) FromMap(values map[string]interface{}) error {
    i.XsdString, _ = coerce.ToString(values[ivXsdString])
    return nil
}

func (i *Input) ToMap() map[string]interface{} {
    return map[string]interface{}{
        ivXsdString: i.XsdString,
    }
}

type Output struct {
    JsonSchemaString string `md:"jsonSchemaString"`
    Error            bool   `md:"error"`
    ErrorMessage     string `md:"errorMessage"`
}

func (o *Output) ToMap() map[string]interface{} {
    return map[string]interface{}{
        ovJsonSchemaString: o.JsonSchemaString,
        ovError:            o.Error,
        ovErrorMessage:     o.ErrorMessage,
    }
}

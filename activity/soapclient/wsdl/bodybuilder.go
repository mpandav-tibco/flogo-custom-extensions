package wsdl

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// XML skeleton builder
// ---------------------------------------------------------------------------

// BuildXMLBody generates a SOAP body XML skeleton for the given operation's
// input message. The skeleton contains placeholder text for each leaf field
// so developers know exactly what to fill in.
//
// Example output (document/literal, GetWeather operation):
//
//	<GetWeather xmlns="http://www.example.com/weather">
//	  <CityName><!-- xs:string --></CityName>
//	  <CountryName><!-- xs:string --></CountryName>
//	</GetWeather>
func BuildXMLBody(op *Operation, targetNamespace string) string {
	if op.InputMsg == nil || len(op.InputMsg.Parts) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, part := range op.InputMsg.Parts {
		rootName := part.ElementName
		if rootName == "" {
			rootName = part.Name // RPC-style fallback
			if rootName == "" {
				rootName = op.Name // last-resort
			}
		}

		ns := targetNamespace
		if ns != "" {
			fmt.Fprintf(&sb, `<%s xmlns="%s">`, rootName, ns)
		} else {
			fmt.Fprintf(&sb, `<%s>`, rootName)
		}
		sb.WriteByte('\n')

		writeXMLFields(&sb, part.Fields, "  ")

		fmt.Fprintf(&sb, "</%s>\n", rootName)
	}
	return sb.String()
}

func writeXMLFields(sb *strings.Builder, fields []FieldDef, indent string) {
	for _, f := range fields {
		if f.IsComplex && len(f.Children) > 0 {
			fmt.Fprintf(sb, "%s<%s>\n", indent, f.Name)
			writeXMLFields(sb, f.Children, indent+"  ")
			fmt.Fprintf(sb, "%s</%s>\n", indent, f.Name)
		} else {
			xsdType := f.XSDType
			if xsdType == "" {
				xsdType = "xs:string"
			}
			optional := ""
			if f.MinOccurs == "0" {
				optional = " (optional)"
			}
			fmt.Fprintf(sb, "%s<%s><!-- %s%s --></%s>\n",
				indent, f.Name, xsdType, optional, f.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// JSON schema builder
// ---------------------------------------------------------------------------

// JSONSchemaDoc is a minimal JSON Schema (draft-07 compatible) document.
type JSONSchemaDoc struct {
	Schema      string                    `json:"$schema"`
	Type        string                    `json:"type"`
	Title       string                    `json:"title,omitempty"`
	Description string                    `json:"description,omitempty"`
	Properties  map[string]*JSONSchemaDoc `json:"properties,omitempty"`
	Items       *JSONSchemaDoc            `json:"items,omitempty"`
	Required    []string                  `json:"required,omitempty"`
}

// BuildJSONSchema generates a JSON Schema for the operation's input message.
// This can be used with Flogo's "Coerce with Schema" feature to get typed
// mapper support for the soapRequestBody field.
func BuildJSONSchema(op *Operation) string {
	if op.InputMsg == nil || len(op.InputMsg.Parts) == 0 {
		return "{}"
	}

	// For document-style with a single "parameters" part, unwrap one level.
	if len(op.InputMsg.Parts) == 1 {
		part := op.InputMsg.Parts[0]
		doc := &JSONSchemaDoc{
			Schema: "http://json-schema.org/draft-07/schema#",
			Title:  part.ElementName,
			Type:   "object",
		}
		populateJSONSchemaProps(doc, part.Fields)
		b, _ := json.MarshalIndent(doc, "", "  ")
		return string(b)
	}

	// Multiple parts (RPC-style): wrap in an outer object.
	doc := &JSONSchemaDoc{
		Schema:     "http://json-schema.org/draft-07/schema#",
		Title:      op.Name,
		Type:       "object",
		Properties: make(map[string]*JSONSchemaDoc),
	}
	for _, part := range op.InputMsg.Parts {
		child := &JSONSchemaDoc{Type: "object"}
		populateJSONSchemaProps(child, part.Fields)
		doc.Properties[part.Name] = child
		if part.Name != "" {
			doc.Required = append(doc.Required, part.Name)
		}
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	return string(b)
}

func populateJSONSchemaProps(doc *JSONSchemaDoc, fields []FieldDef) {
	if len(fields) == 0 {
		return
	}
	doc.Properties = make(map[string]*JSONSchemaDoc, len(fields))
	for _, f := range fields {
		child := fieldToJSONSchema(f)
		doc.Properties[f.Name] = child
		if f.MinOccurs != "0" {
			doc.Required = append(doc.Required, f.Name)
		}
	}
}

func fieldToJSONSchema(f FieldDef) *JSONSchemaDoc {
	if f.MaxOccurs == "unbounded" {
		items := singleFieldSchema(f)
		return &JSONSchemaDoc{Type: "array", Items: items}
	}
	return singleFieldSchema(f)
}

func singleFieldSchema(f FieldDef) *JSONSchemaDoc {
	if f.IsComplex {
		doc := &JSONSchemaDoc{Type: "object"}
		populateJSONSchemaProps(doc, f.Children)
		return doc
	}
	return &JSONSchemaDoc{Type: xsdToJSONType(f.XSDType)}
}

// xsdToJSONType maps common XSD primitive types to JSON Schema types.
func xsdToJSONType(xsdType string) string {
	t := strings.ToLower(localName(xsdType))
	switch t {
	case "int", "integer", "long", "short", "byte",
		"nonnegativeinteger", "positiveinteger",
		"nonpositiveinteger", "negativeinteger",
		"unsignedlong", "unsignedint", "unsignedshort", "unsignedbyte":
		return "integer"
	case "float", "double", "decimal":
		return "number"
	case "boolean":
		return "boolean"
	case "base64binary", "hexbinary":
		return "string" // encoded binary stays as string in JSON
	default:
		return "string"
	}
}

// ---------------------------------------------------------------------------
// Response body builders — mirrors of the request builders for OutputMsg
// ---------------------------------------------------------------------------

// BuildResponseXMLBody generates an XML skeleton for the operation's output
// message (the response body shape). Useful for documentation and testing.
func BuildResponseXMLBody(op *Operation, targetNamespace string) string {
	if op.OutputMsg == nil || len(op.OutputMsg.Parts) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, part := range op.OutputMsg.Parts {
		rootName := part.ElementName
		if rootName == "" {
			rootName = part.Name
			if rootName == "" {
				rootName = op.Name + "Response"
			}
		}

		ns := targetNamespace
		if ns != "" {
			fmt.Fprintf(&sb, `<%s xmlns="%s">`, rootName, ns)
		} else {
			fmt.Fprintf(&sb, `<%s>`, rootName)
		}
		sb.WriteByte('\n')

		writeXMLFields(&sb, part.Fields, "  ")

		fmt.Fprintf(&sb, "</%s>\n", rootName)
	}
	return sb.String()
}

// BuildResponseJSONSchema generates a JSON Schema (draft-07) for the operation's
// output message. Stored in wsdlResponseJSONSchema so the Flogo mapper can
// provide typed field navigation for downstream activities consuming the response.
func BuildResponseJSONSchema(op *Operation) string {
	if op.OutputMsg == nil || len(op.OutputMsg.Parts) == 0 {
		return "{}"
	}

	if len(op.OutputMsg.Parts) == 1 {
		part := op.OutputMsg.Parts[0]
		doc := &JSONSchemaDoc{
			Schema: "http://json-schema.org/draft-07/schema#",
			Title:  part.ElementName,
			Type:   "object",
		}
		populateJSONSchemaProps(doc, part.Fields)
		b, _ := json.MarshalIndent(doc, "", "  ")
		return string(b)
	}

	doc := &JSONSchemaDoc{
		Schema:     "http://json-schema.org/draft-07/schema#",
		Title:      op.Name + "Response",
		Type:       "object",
		Properties: make(map[string]*JSONSchemaDoc),
	}
	for _, part := range op.OutputMsg.Parts {
		child := &JSONSchemaDoc{Type: "object"}
		populateJSONSchemaProps(child, part.Fields)
		doc.Properties[part.Name] = child
		if part.Name != "" {
			doc.Required = append(doc.Required, part.Name)
		}
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	return string(b)
}

// BuildSOAP11FaultJSONSchema returns a fixed JSON Schema describing the SOAP 1.1
// Fault structure. SOAP 1.2 faults have a different element structure; however
// since all faults are surfaced via the same soapResponseFault output field
// regardless of version, using the SOAP 1.1 shape is the practical convention.
// The schema is a constant — it does not depend on WSDL operation definitions.
func BuildSOAP11FaultJSONSchema() string {
	doc := &JSONSchemaDoc{
		Schema:      "http://json-schema.org/draft-07/schema#",
		Title:       "SOAPFault",
		Description: "SOAP 1.1 Fault structure. Present when isFault=true.",
		Type:        "object",
		Properties: map[string]*JSONSchemaDoc{
			"faultcode":   {Type: "string", Description: "Fault code, e.g. soap:Server or soap:Client"},
			"faultstring": {Type: "string", Description: "Human-readable description of the fault"},
			"faultactor":  {Type: "string", Description: "URI of the node that generated the fault (optional)"},
			"detail":      {Type: "object", Description: "Service-specific fault detail element (optional)"},
		},
		Required: []string{"faultcode", "faultstring"},
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	return string(b)
}

// ---------------------------------------------------------------------------
// OperationSummary — lightweight list for UI dropdowns
// ---------------------------------------------------------------------------

// OperationSummary is a compact view of an operation for display in
// configuration UIs (e.g. a dropdown of available operations).
type OperationSummary struct {
	Name       string `json:"name"`
	SOAPAction string `json:"soapAction"`
	Style      string `json:"style"`
}

// ListOperations returns a slice of OperationSummary for all operations in
// the WSDLInfo, suitable for serialising to JSON for a UI dropdown.
func ListOperations(info *WSDLInfo) []OperationSummary {
	result := make([]OperationSummary, 0, len(info.Operations))
	for _, op := range info.Operations {
		result = append(result, OperationSummary{
			Name:       op.Name,
			SOAPAction: op.SOAPAction,
			Style:      op.Style,
		})
	}
	return result
}

// FindOperation returns the Operation with the given name, preferring the
// variant whose SOAPVersion matches versionHint ("1.1" or "1.2").
// When versionHint is empty or no version-matched entry exists, the first
// name-matched operation is returned. Returns nil when no operation with that
// name exists at all.
func FindOperation(info *WSDLInfo, name, versionHint string) *Operation {
	var first *Operation
	for i := range info.Operations {
		op := &info.Operations[i]
		if op.Name != name {
			continue
		}
		if first == nil {
			first = op
		}
		if versionHint != "" && op.SOAPVersion == versionHint {
			return op // exact version match
		}
	}
	return first // best-effort: first by name; nil if no match
}

package xsdtojsonschema

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/coerce"
)

// Constants for identifying inputs and outputs
const (
	ivXSDString        = "xsdString"
	ovJSONSchemaString = "jsonSchemaString"
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
		return false, fmt.Errorf("failed to get input object: %w", err)
	}

	if strings.TrimSpace(input.XSDString) == "" {
		setErrorOutputs(ctx, "Input 'xsdString' is required and cannot be empty", "INVALID_INPUT_XSDString")
		return false, fmt.Errorf("input 'xsdString' is required and cannot be empty")
	}

	logger.Debug("Attempting to convert XSD to JSON Schema")

	// Parse the XSD string using Go's standard XML library
	var schema XSDSchema
	err = xml.Unmarshal([]byte(input.XSDString), &schema)
	if err != nil {
		logger.Errorf("Failed to parse input XSD: %v", err)
		setErrorOutputs(ctx, fmt.Sprintf("Invalid XSD provided: %v", err), "XSD_PARSE_ERROR")
		return false, fmt.Errorf("failed to parse XSD: %w", err)
	}

	// Convert the parsed XSD structure to a JSON Schema map
	jsonSchema, err := convertSchema(&schema)
	if err != nil {
		logger.Errorf("Failed to convert XSD structure to JSON Schema: %v", err)
		setErrorOutputs(ctx, fmt.Sprintf("Conversion failed: %v", err), "CONVERSION_ERROR")
		return false, fmt.Errorf("conversion failed: %w", err)
	}

	// Marshal the map into a pretty-printed JSON string
	jsonBytes, err := json.MarshalIndent(jsonSchema, "", "  ")
	if err != nil {
		logger.Errorf("Failed to marshal JSON Schema to string: %v", err)
		setErrorOutputs(ctx, fmt.Sprintf("Failed to generate JSON string: %v", err), "JSON_MARSHAL_ERROR")
		return false, fmt.Errorf("failed to marshal JSON Schema: %w", err)
	}

	// --- Set Success Outputs ---
	logger.Info("Successfully converted XSD to JSON Schema.")
	ctx.SetOutput(ovJSONSchemaString, string(jsonBytes))
	ctx.SetOutput(ovError, false)
	ctx.SetOutput(ovErrorMessage, "")

	return true, nil
}

// --- XSD Structs for Parsing ---

type XSDSchema struct {
	XMLName      xml.Name         `xml:"schema"`
	Elements     []XSDElement     `xml:"element"`
	Groups       []XSDGroup       `xml:"group"`       // Named group definitions
	SimpleTypes  []XSDSimpleType  `xml:"simpleType"`  // Named simple type definitions
	ComplexTypes []XSDComplexType `xml:"complexType"` // Named complex type definitions
	Notations    []XSDNotation    `xml:"notation"`    // Notation declarations
}

type XSDElement struct {
	Name        string          `xml:"name,attr"`
	Type        string          `xml:"type,attr"`
	MinOccurs   string          `xml:"minOccurs,attr"`
	MaxOccurs   string          `xml:"maxOccurs,attr"`
	Default     string          `xml:"default,attr"`
	Fixed       string          `xml:"fixed,attr"`
	ComplexType *XSDComplexType `xml:"complexType"`
	SimpleType  *XSDSimpleType  `xml:"simpleType"`
}

type XSDComplexType struct {
	Name           string             `xml:"name,attr"` // For named type definitions
	Sequence       *XSDSequence       `xml:"sequence"`
	Choice         *XSDChoice         `xml:"choice"`
	All            *XSDAll            `xml:"all"`
	SimpleContent  *XSDSimpleContent  `xml:"simpleContent"`  // For extending simple types
	ComplexContent *XSDComplexContent `xml:"complexContent"` // For extending complex types
}

type XSDSequence struct {
	Elements  []XSDElement  `xml:"element"`
	GroupRefs []XSDGroupRef `xml:"group"`
	Sequences []XSDSequence `xml:"sequence"`
	Choices   []XSDChoice   `xml:"choice"`
	Alls      []XSDAll      `xml:"all"`
}

type XSDChoice struct {
	Elements  []XSDElement  `xml:"element"`
	GroupRefs []XSDGroupRef `xml:"group"`
	Sequences []XSDSequence `xml:"sequence"`
	Choices   []XSDChoice   `xml:"choice"`
	Alls      []XSDAll      `xml:"all"`
	MinOccurs string        `xml:"minOccurs,attr"`
	MaxOccurs string        `xml:"maxOccurs,attr"`
}

type XSDAll struct {
	Elements  []XSDElement  `xml:"element"`
	GroupRefs []XSDGroupRef `xml:"group"`
	MinOccurs string        `xml:"minOccurs,attr"`
	MaxOccurs string        `xml:"maxOccurs,attr"`
}

// XSDGroup represents a named group definition (xs:group with name attribute)
type XSDGroup struct {
	Name     string       `xml:"name,attr"`
	Sequence *XSDSequence `xml:"sequence"`
	Choice   *XSDChoice   `xml:"choice"`
	All      *XSDAll      `xml:"all"`
}

// XSDGroupRef represents a group reference (xs:group with ref attribute)
type XSDGroupRef struct {
	Ref       string `xml:"ref,attr"`
	MinOccurs string `xml:"minOccurs,attr"`
	MaxOccurs string `xml:"maxOccurs,attr"`
}

// XSDSimpleType represents a simple type definition with restrictions, unions, or lists
type XSDSimpleType struct {
	Name        string          `xml:"name,attr"`   // For named type definitions
	Restriction *XSDRestriction `xml:"restriction"` // Restriction-based type
	Union       *XSDUnion       `xml:"union"`       // Union of multiple types
	List        *XSDList        `xml:"list"`        // List type definition
}

// XSDRestriction represents XSD restrictions (xs:restriction element)
type XSDRestriction struct {
	Base           string           `xml:"base,attr"`
	Patterns       []XSDPattern     `xml:"pattern"`
	Enumerations   []XSDEnumeration `xml:"enumeration"`
	MinLength      *XSDFacet        `xml:"minLength"`
	MaxLength      *XSDFacet        `xml:"maxLength"`
	MinInclusive   *XSDFacet        `xml:"minInclusive"`
	MaxInclusive   *XSDFacet        `xml:"maxInclusive"`
	MinExclusive   *XSDFacet        `xml:"minExclusive"`
	MaxExclusive   *XSDFacet        `xml:"maxExclusive"`
	TotalDigits    *XSDFacet        `xml:"totalDigits"`
	FractionDigits *XSDFacet        `xml:"fractionDigits"`
	// Content model for complex content restrictions
	Sequence *XSDSequence `xml:"sequence"`
	Choice   *XSDChoice   `xml:"choice"`
	All      *XSDAll      `xml:"all"`
}

// XSDPattern represents xs:pattern facet
type XSDPattern struct {
	Value string `xml:"value,attr"`
}

// XSDEnumeration represents xs:enumeration facet
type XSDEnumeration struct {
	Value string `xml:"value,attr"`
}

// XSDFacet represents generic XSD facets with a value attribute
type XSDFacet struct {
	Value string `xml:"value,attr"`
}

// XSDUnion represents xs:union for multiple type alternatives
type XSDUnion struct {
	MemberTypes string          `xml:"memberTypes,attr"` // Space-separated list of types
	SimpleTypes []XSDSimpleType `xml:"simpleType"`       // Inline simple type definitions
}

// XSDList represents xs:list for space-separated values
type XSDList struct {
	ItemType   string         `xml:"itemType,attr"` // Type of list items
	SimpleType *XSDSimpleType `xml:"simpleType"`    // Inline simple type definition
}

// XSDSimpleContent represents xs:simpleContent for extending simple types
type XSDSimpleContent struct {
	Extension   *XSDExtension   `xml:"extension"`
	Restriction *XSDRestriction `xml:"restriction"`
}

// XSDComplexContent represents xs:complexContent for extending complex types
type XSDComplexContent struct {
	Extension   *XSDExtension   `xml:"extension"`
	Restriction *XSDRestriction `xml:"restriction"`
}

// XSDExtension represents xs:extension for type inheritance
type XSDExtension struct {
	Base       string         `xml:"base,attr"`
	Sequence   *XSDSequence   `xml:"sequence"`
	Choice     *XSDChoice     `xml:"choice"`
	All        *XSDAll        `xml:"all"`
	Attributes []XSDAttribute `xml:"attribute"`
}

// XSDAttribute represents xs:attribute
type XSDAttribute struct {
	Name    string `xml:"name,attr"`
	Type    string `xml:"type,attr"`
	Use     string `xml:"use,attr"`
	Default string `xml:"default,attr"`
	Fixed   string `xml:"fixed,attr"`
}

// XSDNotation represents xs:notation
type XSDNotation struct {
	Name   string `xml:"name,attr"`
	Public string `xml:"public,attr"`
	System string `xml:"system,attr"`
}

// --- Conversion Logic ---

// convertSchema is the main conversion function.
func convertSchema(s *XSDSchema) (map[string]interface{}, error) {
	if len(s.Elements) == 0 {
		return nil, fmt.Errorf("XSD does not contain any root elements")
	}
	rootElement := s.Elements[0]

	// Convert the root element to get the main schema structure
	jsonSchema, err := convertElementWithSchema(&rootElement, s)
	if err != nil {
		return nil, err
	}

	// Add the $schema keyword to the root of the generated object.
	jsonSchema["$schema"] = "https://json-schema.org/draft/2020-12/schema"

	return jsonSchema, nil
}

// convertElement recursively converts an XSD element to a JSON Schema property.
func convertElement(el *XSDElement) (map[string]interface{}, error) {
	return convertElementWithSchema(el, nil)
}

// convertElementWithSchema recursively converts an XSD element to a JSON Schema property with schema context for group resolution.
func convertElementWithSchema(el *XSDElement, schema *XSDSchema) (map[string]interface{}, error) {
	prop := make(map[string]interface{})

	// Handle type references to named types
	if el.Type != "" {
		// Check if it's a reference to a named simple type
		if schema != nil {
			if namedType := findNamedSimpleType(el.Type, schema); namedType != nil {
				return convertNamedSimpleType(namedType, schema)
			}
			if namedType := findNamedComplexType(el.Type, schema); namedType != nil {
				return convertComplexTypeWithSchema(namedType, schema)
			}
		}

		// Fall back to built-in type mapping
		jsonType, format := mapXsdTypeToJsonSchemaType(el.Type)
		prop["type"] = jsonType
		if format != "" {
			prop["format"] = format
		}
	}

	// Handle simple type with complex restrictions (restriction, union, list)
	if el.SimpleType != nil {
		return convertSimpleTypeWithSchema(el.SimpleType, schema)
	}

	// Handle complex types (nested objects or sequences)
	if el.ComplexType != nil {
		return convertComplexTypeWithSchema(el.ComplexType, schema)
	}

	// Handle arrays
	if el.MaxOccurs == "unbounded" {
		arrayProp := make(map[string]interface{})
		arrayProp["type"] = "array"

		itemEl := *el
		itemEl.MaxOccurs = "" // Reset for item definition
		items, err := convertElementWithSchema(&itemEl, schema)
		if err != nil {
			return nil, err
		}
		arrayProp["items"] = items
		return arrayProp, nil
	}

	// Add default or fixed values
	if el.Default != "" {
		defaultValue, err := parseDefaultValue(el.Default, prop)
		if err == nil {
			prop["default"] = defaultValue
		}
	} else if el.Fixed != "" {
		// Fixed values become both default and const in JSON Schema
		fixedValue, err := parseDefaultValue(el.Fixed, prop)
		if err == nil {
			prop["const"] = fixedValue
			prop["default"] = fixedValue
		}
	}

	return prop, nil
}

// convertComplexType converts an XSD complexType to a JSON Schema object.
func convertComplexType(ct *XSDComplexType) (map[string]interface{}, error) {
	return convertComplexTypeWithSchema(ct, nil)
}

// convertComplexTypeWithSchema converts an XSD complexType to a JSON Schema object with schema context for group resolution.
func convertComplexTypeWithSchema(ct *XSDComplexType, schema *XSDSchema) (map[string]interface{}, error) {
	obj := make(map[string]interface{})
	obj["type"] = "object"

	// Handle complex content extensions
	if ct.ComplexContent != nil {
		if ct.ComplexContent.Extension != nil {
			// Extend from base type
			baseType := findNamedComplexType(ct.ComplexContent.Extension.Base, schema)
			if baseType == nil {
				return nil, fmt.Errorf("failed to resolve base type %s", ct.ComplexContent.Extension.Base)
			}

			// Start with base type
			baseObj, err := convertComplexTypeWithSchema(baseType, schema)
			if err != nil {
				return nil, err
			}

			// Copy base properties
			for k, v := range baseObj {
				obj[k] = v
			}

			// Merge extension properties
			if ct.ComplexContent.Extension.Sequence != nil {
				properties, ok := obj["properties"].(map[string]interface{})
				if !ok {
					properties = make(map[string]interface{})
				}

				var required []string
				if reqArr, ok := obj["required"].([]string); ok {
					required = reqArr
				}

				// Flatten the extension sequence
				var elements []XSDElement
				if schema != nil {
					flattenedElements, err := flattenSequence(ct.ComplexContent.Extension.Sequence, schema)
					if err != nil {
						return nil, err
					}
					elements = flattenedElements
				} else {
					elements = ct.ComplexContent.Extension.Sequence.Elements
				}

				for _, element := range elements {
					elemCopy := element
					prop, err := convertElementWithSchema(&elemCopy, schema)
					if err != nil {
						return nil, err
					}
					properties[element.Name] = prop
					if element.MinOccurs != "0" {
						required = append(required, element.Name)
					}
				}

				obj["properties"] = properties
				if len(required) > 0 {
					obj["required"] = required
				}
			}

			return obj, nil
		}

		if ct.ComplexContent.Restriction != nil {
			// Restrict base type (for now, just convert the restriction content)
			// In a full implementation, we would validate that the restriction is compatible with the base
			if ct.ComplexContent.Restriction.Sequence != nil {
				return convertSequenceToObject(ct.ComplexContent.Restriction.Sequence, schema)
			}
			if ct.ComplexContent.Restriction.Choice != nil {
				return convertChoiceWithSchema(ct.ComplexContent.Restriction.Choice, schema)
			}
			if ct.ComplexContent.Restriction.All != nil {
				return convertAllWithSchema(ct.ComplexContent.Restriction.All, schema)
			}
		}
	}

	// Handle simple content extensions
	if ct.SimpleContent != nil {
		if ct.SimpleContent.Extension != nil {
			// Simple content with attributes - convert to object with value property
			obj["type"] = "object"
			properties := make(map[string]interface{})

			// Add value property based on base type
			baseType := ct.SimpleContent.Extension.Base
			valueSchema, err := convertXSDTypeToJSONSchema(baseType)
			if err != nil {
				return nil, err
			}
			properties["value"] = valueSchema

			// Add attributes as properties with enhanced support
			required := []string{"value"} // Start with value as required for simple content extensions
			for _, attr := range ct.SimpleContent.Extension.Attributes {
				attrSchema, err := convertXSDTypeToJSONSchema(attr.Type)
				if err != nil {
					return nil, err
				}

				// Add default or fixed values for attributes
				if attr.Default != "" {
					if defaultValue, err := parseDefaultValue(attr.Default, attrSchema); err == nil {
						attrSchema["default"] = defaultValue
					}
				} else if attr.Fixed != "" {
					if fixedValue, err := parseDefaultValue(attr.Fixed, attrSchema); err == nil {
						attrSchema["const"] = fixedValue
						attrSchema["default"] = fixedValue
					}
				}

				properties[attr.Name] = attrSchema

				// Check if attribute is required
				if attr.Use == "required" {
					required = append(required, attr.Name)
				}
			}

			obj["properties"] = properties
			obj["required"] = required
			return obj, nil
		}

		if ct.SimpleContent.Restriction != nil {
			// Simple content restriction - similar to extension but with restrictions
			obj["type"] = "object"
			properties := make(map[string]interface{})

			baseType := ct.SimpleContent.Restriction.Base
			valueSchema, err := convertXSDTypeToJSONSchema(baseType)
			if err != nil {
				return nil, err
			}
			properties["value"] = valueSchema

			obj["properties"] = properties
			obj["required"] = []string{"value"}
			return obj, nil
		}
	}

	// Handle sequence elements (updated logic with group support)
	if ct.Sequence != nil {
		return convertSequenceToObject(ct.Sequence, schema)
	}

	// Handle choice elements (updated logic with group support)
	if ct.Choice != nil {
		return convertChoiceWithSchema(ct.Choice, schema)
	}

	// Handle all elements (updated logic with group support)
	if ct.All != nil {
		return convertAllWithSchema(ct.All, schema)
	}

	return obj, nil
}

// Helper function to convert sequence to object
func convertSequenceToObject(seq *XSDSequence, schema *XSDSchema) (map[string]interface{}, error) {
	obj := make(map[string]interface{})
	obj["type"] = "object"
	properties := make(map[string]interface{})
	var required []string

	// Flatten the sequence to resolve group references
	var elements []XSDElement
	if schema != nil {
		flattenedElements, err := flattenSequence(seq, schema)
		if err != nil {
			return nil, err
		}
		elements = flattenedElements
	} else {
		// Fallback to direct elements if no schema context
		elements = seq.Elements
	}

	for _, element := range elements {
		// Create a copy for the recursive call to avoid pointer issues
		elemCopy := element
		prop, err := convertElementWithSchema(&elemCopy, schema)
		if err != nil {
			return nil, err
		}
		properties[element.Name] = prop
		if element.MinOccurs != "0" {
			required = append(required, element.Name)
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

// convertChoice converts an XSD choice to a JSON Schema oneOf construct.
func convertChoice(choice *XSDChoice) (map[string]interface{}, error) {
	return convertChoiceWithSchema(choice, nil)
}

// convertChoiceWithSchema converts an XSD choice to a JSON Schema oneOf construct with schema context for group resolution.
func convertChoiceWithSchema(choice *XSDChoice, schema *XSDSchema) (map[string]interface{}, error) {
	// Flatten the choice to resolve group references
	var elements []XSDElement
	if schema != nil {
		flattenedElements, err := flattenChoice(choice, schema)
		if err != nil {
			return nil, err
		}
		elements = flattenedElements
	} else {
		// Fallback to direct elements if no schema context
		elements = choice.Elements
	}

	if len(elements) == 0 {
		return nil, fmt.Errorf("XSD choice must contain at least one element")
	}

	// Handle choice with multiple occurrences (arrays of choices)
	if choice.MaxOccurs == "unbounded" {
		arraySchema := make(map[string]interface{})
		arraySchema["type"] = "array"

		// Create choice schema for array items
		choiceSchema, err := createChoiceSchemaWithSchema(elements, schema)
		if err != nil {
			return nil, err
		}
		arraySchema["items"] = choiceSchema
		return arraySchema, nil
	}

	// Handle single choice occurrence
	return createChoiceSchemaWithSchema(elements, schema)
}

// createChoiceSchema creates the oneOf schema structure for choice elements.
func createChoiceSchema(elements []XSDElement) (map[string]interface{}, error) {
	return createChoiceSchemaWithSchema(elements, nil)
}

// createChoiceSchemaWithSchema creates the oneOf schema structure for choice elements with schema context for group resolution.
func createChoiceSchemaWithSchema(elements []XSDElement, schema *XSDSchema) (map[string]interface{}, error) {
	oneOfOptions := make([]map[string]interface{}, 0, len(elements))

	for _, element := range elements {
		// Create a copy for the recursive call
		elemCopy := element
		elementSchema, err := convertElementWithSchema(&elemCopy, schema)
		if err != nil {
			return nil, err
		}

		// Create an object schema with this single property
		option := map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				element.Name: elementSchema,
			},
			"required": []string{element.Name},
		}

		// Add additionalProperties: false to ensure only one choice is selected
		option["additionalProperties"] = false

		oneOfOptions = append(oneOfOptions, option)
	}

	return map[string]interface{}{
		"oneOf": oneOfOptions,
	}, nil
}

// convertAll converts an XSD all to a JSON Schema object where all elements are optional but can appear in any order.
func convertAll(all *XSDAll) (map[string]interface{}, error) {
	return convertAllWithSchema(all, nil)
}

// convertAllWithSchema converts an XSD all to a JSON Schema object with schema context for group resolution.
func convertAllWithSchema(all *XSDAll, schema *XSDSchema) (map[string]interface{}, error) {
	// Flatten the all construct to resolve group references
	var elements []XSDElement
	if schema != nil {
		flattenedElements, err := flattenAll(all, schema)
		if err != nil {
			return nil, err
		}
		elements = flattenedElements
	} else {
		// Fallback to direct elements if no schema context
		elements = all.Elements
	}

	if len(elements) == 0 {
		return nil, fmt.Errorf("XSD all must contain at least one element")
	}

	// XSD all elements are typically optional and can appear in any order
	// We convert this to a JSON Schema object with all properties optional by default
	obj := make(map[string]interface{})
	obj["type"] = "object"
	properties := make(map[string]interface{})
	var required []string

	for _, element := range elements {
		// Create a copy for the recursive call to avoid pointer issues
		elemCopy := element
		prop, err := convertElementWithSchema(&elemCopy, schema)
		if err != nil {
			return nil, err
		}
		properties[element.Name] = prop

		// In XSD all, elements are required by default unless minOccurs="0"
		if element.MinOccurs != "0" {
			required = append(required, element.Name)
		}
	}

	if len(properties) > 0 {
		obj["properties"] = properties
	}
	if len(required) > 0 {
		obj["required"] = required
	}

	// Add additionalProperties: false to ensure only defined elements are allowed
	obj["additionalProperties"] = false

	return obj, nil
}

// resolveGroupRef resolves a group reference to its actual content.
func resolveGroupRef(groupRef *XSDGroupRef, schema *XSDSchema) (*XSDGroup, error) {
	if groupRef.Ref == "" {
		return nil, fmt.Errorf("group reference must have a 'ref' attribute")
	}

	// Find the named group in the schema
	for _, group := range schema.Groups {
		if group.Name == groupRef.Ref {
			return &group, nil
		}
	}

	return nil, fmt.Errorf("group reference '%s' not found in schema", groupRef.Ref)
}

// flattenGroup converts a group definition to its constituent elements.
func flattenGroup(group *XSDGroup, groupRef *XSDGroupRef, schema *XSDSchema) ([]XSDElement, error) {
	var elements []XSDElement

	// Apply minOccurs/maxOccurs from the group reference to the group content
	minOccurs := groupRef.MinOccurs
	maxOccurs := groupRef.MaxOccurs
	if minOccurs == "" {
		minOccurs = "1"
	}
	if maxOccurs == "" {
		maxOccurs = "1"
	}

	// Handle sequence within group
	if group.Sequence != nil {
		seqElements, err := flattenSequence(group.Sequence, schema)
		if err != nil {
			return nil, err
		}
		elements = append(elements, seqElements...)
	}

	// Handle choice within group
	if group.Choice != nil {
		// For choice within group, we need to create a wrapper element
		// that represents the choice construct
		choiceElement := XSDElement{
			Name:        "group_choice_" + group.Name,
			MinOccurs:   minOccurs,
			MaxOccurs:   maxOccurs,
			ComplexType: &XSDComplexType{Choice: group.Choice},
		}
		elements = append(elements, choiceElement)
	}

	// Handle all within group
	if group.All != nil {
		// For all within group, we need to create a wrapper element
		// that represents the all construct
		allElement := XSDElement{
			Name:        "group_all_" + group.Name,
			MinOccurs:   minOccurs,
			MaxOccurs:   maxOccurs,
			ComplexType: &XSDComplexType{All: group.All},
		}
		elements = append(elements, allElement)
	}

	return elements, nil
}

// flattenSequence recursively flattens a sequence, resolving any group references.
func flattenSequence(seq *XSDSequence, schema *XSDSchema) ([]XSDElement, error) {
	var elements []XSDElement

	// Add direct elements
	elements = append(elements, seq.Elements...)

	// Resolve and flatten group references
	for _, groupRef := range seq.GroupRefs {
		group, err := resolveGroupRef(&groupRef, schema)
		if err != nil {
			return nil, err
		}
		groupElements, err := flattenGroup(group, &groupRef, schema)
		if err != nil {
			return nil, err
		}
		elements = append(elements, groupElements...)
	}

	// Handle nested sequences (rare but possible)
	for _, nestedSeq := range seq.Sequences {
		nestedElements, err := flattenSequence(&nestedSeq, schema)
		if err != nil {
			return nil, err
		}
		elements = append(elements, nestedElements...)
	}

	// Handle nested choices
	for _, nestedChoice := range seq.Choices {
		choiceElement := XSDElement{
			Name:        "sequence_choice",
			ComplexType: &XSDComplexType{Choice: &nestedChoice},
		}
		elements = append(elements, choiceElement)
	}

	// Handle nested alls
	for _, nestedAll := range seq.Alls {
		allElement := XSDElement{
			Name:        "sequence_all",
			ComplexType: &XSDComplexType{All: &nestedAll},
		}
		elements = append(elements, allElement)
	}

	return elements, nil
}

// flattenChoice recursively flattens a choice, resolving any group references.
func flattenChoice(choice *XSDChoice, schema *XSDSchema) ([]XSDElement, error) {
	var elements []XSDElement

	// Add direct elements
	elements = append(elements, choice.Elements...)

	// Resolve and flatten group references
	for _, groupRef := range choice.GroupRefs {
		group, err := resolveGroupRef(&groupRef, schema)
		if err != nil {
			return nil, err
		}
		groupElements, err := flattenGroup(group, &groupRef, schema)
		if err != nil {
			return nil, err
		}
		elements = append(elements, groupElements...)
	}

	// Handle nested sequences
	for _, nestedSeq := range choice.Sequences {
		seqElement := XSDElement{
			Name:        "choice_sequence",
			ComplexType: &XSDComplexType{Sequence: &nestedSeq},
		}
		elements = append(elements, seqElement)
	}

	// Handle nested choices
	for _, nestedChoice := range choice.Choices {
		choiceElement := XSDElement{
			Name:        "choice_nested",
			ComplexType: &XSDComplexType{Choice: &nestedChoice},
		}
		elements = append(elements, choiceElement)
	}

	// Handle nested alls
	for _, nestedAll := range choice.Alls {
		allElement := XSDElement{
			Name:        "choice_all",
			ComplexType: &XSDComplexType{All: &nestedAll},
		}
		elements = append(elements, allElement)
	}

	return elements, nil
}

// flattenAll recursively flattens an all construct, resolving any group references.
func flattenAll(all *XSDAll, schema *XSDSchema) ([]XSDElement, error) {
	var elements []XSDElement

	// Add direct elements
	elements = append(elements, all.Elements...)

	// Resolve and flatten group references
	for _, groupRef := range all.GroupRefs {
		group, err := resolveGroupRef(&groupRef, schema)
		if err != nil {
			return nil, err
		}
		groupElements, err := flattenGroup(group, &groupRef, schema)
		if err != nil {
			return nil, err
		}
		elements = append(elements, groupElements...)
	}

	return elements, nil
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

// convertXSDTypeToJSONSchema converts an XSD type string to a JSON Schema object
func convertXSDTypeToJSONSchema(xsdType string) (map[string]interface{}, error) {
	schema := make(map[string]interface{})

	jsonType, format := mapXsdTypeToJsonSchemaType(xsdType)
	schema["type"] = jsonType

	if format != "" {
		schema["format"] = format
	}

	return schema, nil
}

// applyRestrictions applies XSD restrictions to a JSON Schema property
func applyRestrictions(prop map[string]interface{}, restriction *XSDRestriction) error {
	// Handle pattern restrictions (regex)
	if len(restriction.Patterns) > 0 {
		if len(restriction.Patterns) == 1 {
			// Single pattern
			prop["pattern"] = restriction.Patterns[0].Value
		} else {
			// Multiple patterns - combine with alternation
			patterns := make([]string, len(restriction.Patterns))
			for i, pattern := range restriction.Patterns {
				patterns[i] = pattern.Value
			}
			prop["pattern"] = "(" + strings.Join(patterns, ")|(") + ")"
		}
	}

	// Handle enumeration restrictions
	if len(restriction.Enumerations) > 0 {
		enumValues := make([]interface{}, len(restriction.Enumerations))
		for i, enum := range restriction.Enumerations {
			// Convert enum value to appropriate type based on JSON Schema type
			jsonType, _ := prop["type"].(string)
			enumValues[i] = convertEnumValueToType(enum.Value, jsonType)
		}
		prop["enum"] = enumValues
	}

	// Handle string length restrictions
	if restriction.MinLength != nil {
		minLen, err := strconv.Atoi(restriction.MinLength.Value)
		if err != nil {
			return fmt.Errorf("invalid minLength value: %s", restriction.MinLength.Value)
		}
		prop["minLength"] = minLen
	}

	if restriction.MaxLength != nil {
		maxLen, err := strconv.Atoi(restriction.MaxLength.Value)
		if err != nil {
			return fmt.Errorf("invalid maxLength value: %s", restriction.MaxLength.Value)
		}
		prop["maxLength"] = maxLen
	}

	// Handle numeric restrictions
	if restriction.MinInclusive != nil {
		minVal, err := convertNumericValue(restriction.MinInclusive.Value)
		if err != nil {
			return fmt.Errorf("invalid minInclusive value: %s", restriction.MinInclusive.Value)
		}
		prop["minimum"] = minVal
	}

	if restriction.MaxInclusive != nil {
		maxVal, err := convertNumericValue(restriction.MaxInclusive.Value)
		if err != nil {
			return fmt.Errorf("invalid maxInclusive value: %s", restriction.MaxInclusive.Value)
		}
		prop["maximum"] = maxVal
	}

	if restriction.MinExclusive != nil {
		minVal, err := convertNumericValue(restriction.MinExclusive.Value)
		if err != nil {
			return fmt.Errorf("invalid minExclusive value: %s", restriction.MinExclusive.Value)
		}
		prop["exclusiveMinimum"] = minVal
	}

	if restriction.MaxExclusive != nil {
		maxVal, err := convertNumericValue(restriction.MaxExclusive.Value)
		if err != nil {
			return fmt.Errorf("invalid maxExclusive value: %s", restriction.MaxExclusive.Value)
		}
		prop["exclusiveMaximum"] = maxVal
	}

	// Handle totalDigits and fractionDigits (custom constraints)
	if restriction.TotalDigits != nil {
		totalDigits, err := strconv.Atoi(restriction.TotalDigits.Value)
		if err != nil {
			return fmt.Errorf("invalid totalDigits value: %s", restriction.TotalDigits.Value)
		}
		// Use a custom property as JSON Schema doesn't have direct totalDigits support
		prop["x-totalDigits"] = totalDigits
	}

	if restriction.FractionDigits != nil {
		fractionDigits, err := strconv.Atoi(restriction.FractionDigits.Value)
		if err != nil {
			return fmt.Errorf("invalid fractionDigits value: %s", restriction.FractionDigits.Value)
		}
		// Use a custom property as JSON Schema doesn't have direct fractionDigits support
		prop["x-fractionDigits"] = fractionDigits
	}

	return nil
}

// convertEnumValueToType converts string enum values to appropriate JSON types
func convertEnumValueToType(value, jsonType string) interface{} {
	switch jsonType {
	case "integer":
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	case "number":
		if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
			return floatVal
		}
	case "boolean":
		if boolVal, err := strconv.ParseBool(value); err == nil {
			return boolVal
		}
	}
	// Default to string
	return value
}

// convertNumericValue converts string numeric values to appropriate JSON types
func convertNumericValue(value string) (interface{}, error) {
	// Try integer first
	if intVal, err := strconv.Atoi(value); err == nil {
		return intVal, nil
	}
	// Try float
	if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
		return floatVal, nil
	}
	return nil, fmt.Errorf("invalid numeric value: %s", value)
}

// --- Phase 5: Complex Restrictions Support Functions ---

// findNamedSimpleType searches for a named simple type definition in the schema
func findNamedSimpleType(typeName string, schema *XSDSchema) *XSDSimpleType {
	// Remove namespace prefix if present
	localName := typeName
	if colonIndex := strings.LastIndex(typeName, ":"); colonIndex != -1 {
		localName = typeName[colonIndex+1:]
	}

	for i := range schema.SimpleTypes {
		if schema.SimpleTypes[i].Name == localName {
			return &schema.SimpleTypes[i]
		}
	}
	return nil
}

// findNamedComplexType searches for a named complex type definition in the schema
func findNamedComplexType(typeName string, schema *XSDSchema) *XSDComplexType {
	// Remove namespace prefix if present
	localName := typeName
	if colonIndex := strings.LastIndex(typeName, ":"); colonIndex != -1 {
		localName = typeName[colonIndex+1:]
	}

	for i := range schema.ComplexTypes {
		if schema.ComplexTypes[i].Name == localName {
			return &schema.ComplexTypes[i]
		}
	}
	return nil
}

// convertNamedSimpleType converts a named simple type to JSON Schema
func convertNamedSimpleType(simpleType *XSDSimpleType, schema *XSDSchema) (map[string]interface{}, error) {
	return convertSimpleTypeWithSchema(simpleType, schema)
}

// convertSimpleTypeWithSchema converts an XSD simple type (restriction, union, or list) to JSON Schema
func convertSimpleTypeWithSchema(simpleType *XSDSimpleType, schema *XSDSchema) (map[string]interface{}, error) {
	prop := make(map[string]interface{})

	// Handle restriction-based simple types
	if simpleType.Restriction != nil {
		restriction := simpleType.Restriction
		if restriction.Base != "" {
			// Check if base is a named type
			if schema != nil {
				if baseType := findNamedSimpleType(restriction.Base, schema); baseType != nil {
					// Recursively resolve the base type
					baseProp, err := convertNamedSimpleType(baseType, schema)
					if err != nil {
						return nil, fmt.Errorf("failed to resolve base type %s: %w", restriction.Base, err)
					}
					// Merge base properties
					for k, v := range baseProp {
						prop[k] = v
					}
				} else {
					// Built-in type
					jsonType, format := mapXsdTypeToJsonSchemaType(restriction.Base)
					prop["type"] = jsonType
					if format != "" {
						prop["format"] = format
					}
				}
			} else {
				// No schema context, assume built-in type
				jsonType, format := mapXsdTypeToJsonSchemaType(restriction.Base)
				prop["type"] = jsonType
				if format != "" {
					prop["format"] = format
				}
			}
		}

		// Apply restrictions to JSON Schema
		if err := applyRestrictions(prop, restriction); err != nil {
			return nil, fmt.Errorf("failed to apply restrictions: %w", err)
		}
	}

	// Handle union types
	if simpleType.Union != nil {
		return convertUnionType(simpleType.Union, schema)
	}

	// Handle list types
	if simpleType.List != nil {
		return convertListType(simpleType.List, schema)
	}

	return prop, nil
}

// convertUnionType converts an XSD union type to JSON Schema oneOf
func convertUnionType(union *XSDUnion, schema *XSDSchema) (map[string]interface{}, error) {
	var alternatives []map[string]interface{}

	// Handle memberTypes (space-separated list of type names)
	if union.MemberTypes != "" {
		typeNames := strings.Fields(union.MemberTypes)
		for _, typeName := range typeNames {
			var altProp map[string]interface{}
			var err error

			// Check if it's a named type
			if schema != nil {
				if namedType := findNamedSimpleType(typeName, schema); namedType != nil {
					altProp, err = convertNamedSimpleType(namedType, schema)
				} else {
					// Built-in type
					altProp = make(map[string]interface{})
					jsonType, format := mapXsdTypeToJsonSchemaType(typeName)
					altProp["type"] = jsonType
					if format != "" {
						altProp["format"] = format
					}
				}
			} else {
				// No schema context, assume built-in type
				altProp = make(map[string]interface{})
				jsonType, format := mapXsdTypeToJsonSchemaType(typeName)
				altProp["type"] = jsonType
				if format != "" {
					altProp["format"] = format
				}
			}

			if err != nil {
				return nil, fmt.Errorf("failed to convert union member type %s: %w", typeName, err)
			}
			alternatives = append(alternatives, altProp)
		}
	}

	// Handle inline simple type definitions
	for _, inlineType := range union.SimpleTypes {
		altProp, err := convertSimpleTypeWithSchema(&inlineType, schema)
		if err != nil {
			return nil, fmt.Errorf("failed to convert inline union type: %w", err)
		}
		alternatives = append(alternatives, altProp)
	}

	if len(alternatives) == 0 {
		return nil, fmt.Errorf("union type must contain at least one member type")
	}

	return map[string]interface{}{
		"oneOf": alternatives,
	}, nil
}

// convertListType converts an XSD list type to JSON Schema array
func convertListType(list *XSDList, schema *XSDSchema) (map[string]interface{}, error) {
	arrayProp := make(map[string]interface{})
	arrayProp["type"] = "array"

	var itemsProp map[string]interface{}
	var err error

	// Handle itemType attribute
	if list.ItemType != "" {
		// Check if it's a named type
		if schema != nil {
			if namedType := findNamedSimpleType(list.ItemType, schema); namedType != nil {
				itemsProp, err = convertNamedSimpleType(namedType, schema)
			} else {
				// Built-in type
				itemsProp = make(map[string]interface{})
				jsonType, format := mapXsdTypeToJsonSchemaType(list.ItemType)
				itemsProp["type"] = jsonType
				if format != "" {
					itemsProp["format"] = format
				}
			}
		} else {
			// No schema context, assume built-in type
			itemsProp = make(map[string]interface{})
			jsonType, format := mapXsdTypeToJsonSchemaType(list.ItemType)
			itemsProp["type"] = jsonType
			if format != "" {
				itemsProp["format"] = format
			}
		}
	}

	// Handle inline simple type definition
	if list.SimpleType != nil {
		itemsProp, err = convertSimpleTypeWithSchema(list.SimpleType, schema)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to convert list item type: %w", err)
	}

	if itemsProp == nil {
		return nil, fmt.Errorf("list type must specify an item type")
	}

	arrayProp["items"] = itemsProp
	return arrayProp, nil
}

// setErrorOutputs is a helper function to set all error-related outputs at once.
func setErrorOutputs(ctx activity.Context, message, code string) {
	ctx.SetOutput(ovJSONSchemaString, "")
	ctx.SetOutput(ovError, true)
	ctx.SetOutput(ovErrorMessage, fmt.Sprintf("[%s] %s", code, message))
}

// --- Supporting Structs ---

type Input struct {
	XSDString string `md:"xsdString"`
}

func (i *Input) FromMap(values map[string]interface{}) error {
	i.XSDString, _ = coerce.ToString(values[ivXSDString])
	return nil
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		ivXSDString: i.XSDString,
	}
}

// parseDefaultValue converts a string default value to the appropriate type based on the JSON Schema property
func parseDefaultValue(value string, prop map[string]interface{}) (interface{}, error) {
	if value == "" {
		return nil, fmt.Errorf("empty default value")
	}

	// Get the type from the property
	propType, ok := prop["type"].(string)
	if !ok {
		// If no type specified, return as string
		return value, nil
	}

	switch propType {
	case "boolean":
		if value == "true" || value == "1" {
			return true, nil
		} else if value == "false" || value == "0" {
			return false, nil
		}
		return nil, fmt.Errorf("invalid boolean value: %s", value)
	case "integer":
		return strconv.Atoi(value)
	case "number":
		return strconv.ParseFloat(value, 64)
	case "string":
		return value, nil
	default:
		// For unknown types, return as string
		return value, nil
	}
}

type Output struct {
	JSONSchemaString string `md:"jsonSchemaString"`
	Error            bool   `md:"error"`
	ErrorMessage     string `md:"errorMessage"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		ovJSONSchemaString: o.JSONSchemaString,
		ovError:            o.Error,
		ovErrorMessage:     o.ErrorMessage,
	}
}

package xsdschematransform

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// openAPIVersionPattern restricts openApiVersion to well-formed 3.0.x / 3.1.x values so typos or
// unsupported majors (e.g. Swagger "2.0") fail fast instead of silently picking a dialect.
var openAPIVersionPattern = regexp.MustCompile(`^3\.[01]\.\d+$`)

// isValidOpenAPIVersion reports whether version is a supported 3.0.x/3.1.x OpenAPI version string.
func isValidOpenAPIVersion(version string) bool {
	return openAPIVersionPattern.MatchString(version)
}

// --- OpenAPI Document Types (Schema Object per OpenAPI 3.0.x / 3.1.x) ---

// OpenAPIDocument is a minimal, valid OpenAPI document wrapping the converted schema(s)
// under components.schemas so the result can be dropped straight into a larger spec.
type OpenAPIDocument struct {
	OpenAPI    string                 `json:"openapi"`
	Info       *OpenAPIInfo           `json:"info"`
	Paths      map[string]interface{} `json:"paths"`
	Components *OpenAPIComponents     `json:"components,omitempty"`
}

// OpenAPIInfo is the required OpenAPI "info" object.
type OpenAPIInfo struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

// OpenAPIComponents holds the reusable schema definitions.
type OpenAPIComponents struct {
	Schemas map[string]*OpenAPISchema `json:"schemas,omitempty"`
}

// OpenAPISchema represents an OpenAPI Schema Object. It supports both dialects:
//   - 3.0.x: JSON-Schema-like subset (boolean exclusiveMinimum/Maximum, "nullable" flag)
//   - 3.1.x: full JSON Schema 2020-12 (numeric exclusiveMinimum/Maximum, type arrays for null, const)
//
// The generator picks the correct shape for each field based on the requested openApiVersion.
type OpenAPISchema struct {
	Type                 interface{}               `json:"type,omitempty"` // string, or []string (3.1 nullable)
	Format               string                    `json:"format,omitempty"`
	Title                string                    `json:"title,omitempty"`
	Description          string                    `json:"description,omitempty"`
	Properties           map[string]*OpenAPISchema `json:"properties,omitempty"`
	Required             []string                  `json:"required,omitempty"`
	Items                *OpenAPISchema            `json:"items,omitempty"`
	Enum                 []interface{}             `json:"enum,omitempty"`
	OneOf                []*OpenAPISchema          `json:"oneOf,omitempty"`
	AnyOf                []*OpenAPISchema          `json:"anyOf,omitempty"`
	AllOf                []*OpenAPISchema          `json:"allOf,omitempty"`
	Nullable             *bool                     `json:"nullable,omitempty"` // 3.0 only
	Default              interface{}               `json:"default,omitempty"`
	Const                interface{}               `json:"const,omitempty"` // 3.1 only
	Pattern              string                    `json:"pattern,omitempty"`
	MinLength            *int                      `json:"minLength,omitempty"`
	MaxLength            *int                      `json:"maxLength,omitempty"`
	Minimum              *float64                  `json:"minimum,omitempty"`
	Maximum              *float64                  `json:"maximum,omitempty"`
	ExclusiveMinimum     interface{}               `json:"exclusiveMinimum,omitempty"` // bool(3.0) or number(3.1)
	ExclusiveMaximum     interface{}               `json:"exclusiveMaximum,omitempty"` // bool(3.0) or number(3.1)
	MultipleOf           *float64                  `json:"multipleOf,omitempty"`
	MinItems             *int                      `json:"minItems,omitempty"`
	MaxItems             *int                      `json:"maxItems,omitempty"`
	UniqueItems          *bool                     `json:"uniqueItems,omitempty"`
	MinProperties        *int                      `json:"minProperties,omitempty"`
	MaxProperties        *int                      `json:"maxProperties,omitempty"`
	AdditionalProperties interface{}               `json:"additionalProperties,omitempty"`
	ReadOnly             *bool                     `json:"readOnly,omitempty"`
	WriteOnly            *bool                     `json:"writeOnly,omitempty"`
	Deprecated           *bool                     `json:"deprecated,omitempty"`
	XSDTotalDigits       *int                      `json:"x-xsdTotalDigits,omitempty"`    // no OpenAPI equivalent
	XSDFractionDigits    *int                      `json:"x-xsdFractionDigits,omitempty"` // no OpenAPI equivalent
}

// generateOpenAPISchema converts the universal schema into a complete OpenAPI document
// (as a JSON string) with all XSD restriction facets mapped to their closest OpenAPI equivalent.
func generateOpenAPISchema(universalSchema *UniversalSchema, input *Input) (string, error) {
	version := strings.TrimSpace(input.OpenAPIVersion)
	if version == "" {
		version = "3.1.0"
	}

	rootName := strings.TrimSpace(input.OpenAPISchemaName)
	if rootName == "" {
		rootName = "RootSchema"
	}

	title := strings.TrimSpace(input.OpenAPITitle)
	if title == "" {
		title = rootName
	}

	infoVersion := strings.TrimSpace(input.OpenAPIInfoVersion)
	if infoVersion == "" {
		infoVersion = "1.0.0"
	}

	rootSchema := &OpenAPISchema{}
	if err := convertUniversalToOpenAPISchema(universalSchema, rootSchema, version); err != nil {
		return "", err
	}

	doc := &OpenAPIDocument{
		OpenAPI: version,
		Info: &OpenAPIInfo{
			Title:   title,
			Version: infoVersion,
		},
		Paths: map[string]interface{}{},
		Components: &OpenAPIComponents{
			Schemas: map[string]*OpenAPISchema{},
		},
	}

	// Promote named XSD complex/simple types to reusable components.schemas entries.
	for defName, defSchema := range universalSchema.Definitions {
		converted := &OpenAPISchema{}
		if err := convertUniversalToOpenAPISchema(defSchema, converted, version); err != nil {
			return "", err
		}
		doc.Components.Schemas[defName] = converted
	}

	// Assigned last so openApiSchemaName always wins over a same-named XSD type definition.
	doc.Components.Schemas[rootName] = rootSchema

	jsonBytes, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal OpenAPI schema: %w", err)
	}

	return string(jsonBytes), nil
}

// convertUniversalToOpenAPISchema converts a universal schema (object/array/root level) to an OpenAPI Schema Object.
func convertUniversalToOpenAPISchema(universal *UniversalSchema, schema *OpenAPISchema, version string) error {
	if universal.Type != "" {
		schema.Type = universal.Type
	}
	if universal.Title != "" {
		schema.Title = universal.Title
	}
	if universal.Description != "" {
		schema.Description = universal.Description
	}
	if universal.Format != "" {
		schema.Format = universal.Format
	}

	if len(universal.Properties) > 0 {
		schema.Properties = make(map[string]*OpenAPISchema, len(universal.Properties))
		var required []string
		for name, prop := range universal.Properties {
			propSchema := &OpenAPISchema{}
			if err := convertUniversalPropertyToOpenAPISchema(prop, propSchema, version); err != nil {
				return err
			}
			schema.Properties[name] = propSchema
			if prop.Required != nil && *prop.Required {
				required = append(required, name)
			}
		}
		if len(required) > 0 {
			sort.Strings(required) // deterministic output regardless of map iteration order
			schema.Required = required
		}
	}

	if universal.Items != nil {
		itemSchema := &OpenAPISchema{}
		if err := convertUniversalToOpenAPISchema(universal.Items, itemSchema, version); err != nil {
			return err
		}
		schema.Items = itemSchema
	}

	if len(universal.EnumValues) > 0 {
		schema.Enum = universal.EnumValues
	}

	if err := convertCombinators(universal, schema, version); err != nil {
		return err
	}

	if universal.Constraints != nil {
		applyConstraintsToOpenAPISchema(universal.Constraints, schema, version)
	}

	if universal.AdditionalProperties != nil {
		schema.AdditionalProperties = universal.AdditionalProperties
	}

	return nil
}

// convertUniversalPropertyToOpenAPISchema converts a universal property (object field) to an OpenAPI Schema Object.
// It also applies the XSD minOccurs/maxOccurs occurrence restriction, which has no direct property in JSON Schema
// but is the equivalent of wrapping the field in an array with minItems/maxItems.
func convertUniversalPropertyToOpenAPISchema(prop *UniversalProperty, schema *OpenAPISchema, version string) error {
	base := &OpenAPISchema{}

	if prop.Type != "" {
		base.Type = prop.Type
	}
	if prop.Format != "" {
		base.Format = prop.Format
	}
	if prop.Description != "" {
		base.Description = prop.Description
	}
	if prop.Default != nil {
		base.Default = prop.Default
	}
	if len(prop.EnumValues) > 0 {
		base.Enum = prop.EnumValues
	}
	if prop.Fixed != nil {
		// xs:fixed has no OpenAPI equivalent; the closest lossless mapping is a single-value
		// enum plus a matching default, which forces the same "only this value is valid" behavior.
		base.Enum = []interface{}{prop.Fixed}
		base.Default = prop.Fixed
	}

	// xs:list -> array of the declared item type (named or inline)
	if prop.Items != nil {
		itemSchema := &OpenAPISchema{}
		if err := convertUniversalToOpenAPISchema(prop.Items, itemSchema, version); err != nil {
			return err
		}
		base.Items = itemSchema
	}

	if len(prop.Properties) > 0 {
		base.Properties = make(map[string]*OpenAPISchema, len(prop.Properties))
		var required []string
		for name, nested := range prop.Properties {
			nestedSchema := &OpenAPISchema{}
			if err := convertUniversalPropertyToOpenAPISchema(nested, nestedSchema, version); err != nil {
				return err
			}
			base.Properties[name] = nestedSchema
			if nested.Required != nil && *nested.Required {
				required = append(required, name)
			}
		}
		if len(required) > 0 {
			sort.Strings(required) // deterministic output regardless of map iteration order
			base.Required = required
		}
	}

	if prop.AdditionalProperties != nil {
		base.AdditionalProperties = prop.AdditionalProperties
	}

	// xs:choice/union nested in an inline complex type
	if len(prop.OneOf) > 0 {
		base.OneOf = make([]*OpenAPISchema, len(prop.OneOf))
		for i, s := range prop.OneOf {
			branchSchema := &OpenAPISchema{}
			if err := convertUniversalToOpenAPISchema(s, branchSchema, version); err != nil {
				return err
			}
			base.OneOf[i] = branchSchema
		}
	}
	if len(prop.AnyOf) > 0 {
		base.AnyOf = make([]*OpenAPISchema, len(prop.AnyOf))
		for i, s := range prop.AnyOf {
			branchSchema := &OpenAPISchema{}
			if err := convertUniversalToOpenAPISchema(s, branchSchema, version); err != nil {
				return err
			}
			base.AnyOf[i] = branchSchema
		}
	}
	if len(prop.AllOf) > 0 {
		base.AllOf = make([]*OpenAPISchema, len(prop.AllOf))
		for i, s := range prop.AllOf {
			branchSchema := &OpenAPISchema{}
			if err := convertUniversalToOpenAPISchema(s, branchSchema, version); err != nil {
				return err
			}
			base.AllOf[i] = branchSchema
		}
	}

	if prop.Constraints != nil {
		applyConstraintsToOpenAPISchema(prop.Constraints, base, version)
	}

	applyNullable(base, prop.Nullable, version)

	// xs:minOccurs/maxOccurs > 1 (or "unbounded") means the element repeats -> JSON/OpenAPI array.
	if isRepeating(prop) {
		schema.Type = "array"
		schema.Items = base
		if prop.MinOccurs != nil && *prop.MinOccurs > 0 {
			minItems := *prop.MinOccurs
			schema.MinItems = &minItems
		}
		if prop.MaxOccurs != nil && *prop.MaxOccurs >= 0 {
			maxItems := *prop.MaxOccurs
			schema.MaxItems = &maxItems
		}
		return nil
	}

	*schema = *base
	return nil
}

// isRepeating reports whether an XSD element's occurrence constraints require array semantics.
func isRepeating(prop *UniversalProperty) bool {
	if prop.MaxOccurs != nil && *prop.MaxOccurs != 1 {
		return true // unbounded (-1) or explicit maxOccurs > 1
	}
	if prop.MinOccurs != nil && *prop.MinOccurs > 1 {
		return true
	}
	return false
}

// applyNullable maps xs:nillable to the correct OpenAPI dialect representation.
func applyNullable(schema *OpenAPISchema, nullable bool, version string) {
	if !nullable {
		return
	}
	if isOpenAPI31(version) {
		// 3.1 dropped "nullable"; a null-able value is represented via a type array.
		if typeStr, ok := schema.Type.(string); ok && typeStr != "" {
			schema.Type = []string{typeStr, "null"}
		}
		return
	}
	t := true
	schema.Nullable = &t
}

// convertCombinators converts oneOf/anyOf/allOf (used for xs:choice and xs:union).
func convertCombinators(universal *UniversalSchema, schema *OpenAPISchema, version string) error {
	if len(universal.OneOf) > 0 {
		schema.OneOf = make([]*OpenAPISchema, len(universal.OneOf))
		for i, s := range universal.OneOf {
			converted := &OpenAPISchema{}
			if err := convertUniversalToOpenAPISchema(s, converted, version); err != nil {
				return err
			}
			schema.OneOf[i] = converted
		}
	}
	if len(universal.AnyOf) > 0 {
		schema.AnyOf = make([]*OpenAPISchema, len(universal.AnyOf))
		for i, s := range universal.AnyOf {
			converted := &OpenAPISchema{}
			if err := convertUniversalToOpenAPISchema(s, converted, version); err != nil {
				return err
			}
			schema.AnyOf[i] = converted
		}
	}
	if len(universal.AllOf) > 0 {
		schema.AllOf = make([]*OpenAPISchema, len(universal.AllOf))
		for i, s := range universal.AllOf {
			converted := &OpenAPISchema{}
			if err := convertUniversalToOpenAPISchema(s, converted, version); err != nil {
				return err
			}
			schema.AllOf[i] = converted
		}
	}
	return nil
}

// applyConstraintsToOpenAPISchema maps every XSD-derived restriction facet to its OpenAPI equivalent,
// accounting for the differences between the 3.0.x and 3.1.x Schema Object dialects.
func applyConstraintsToOpenAPISchema(c *UniversalConstraints, schema *OpenAPISchema, version string) {
	// --- String restrictions: xs:minLength / xs:maxLength / xs:pattern ---
	if c.MinLength != nil {
		schema.MinLength = c.MinLength
	}
	if c.MaxLength != nil {
		schema.MaxLength = c.MaxLength
	}
	if len(c.Pattern) == 1 {
		schema.Pattern = c.Pattern[0]
	} else if len(c.Pattern) > 1 {
		// XSD unions multiple xs:pattern facets with OR semantics; JSON Schema/OpenAPI only
		// support a single pattern, so approximate the union via regex alternation.
		parts := make([]string, len(c.Pattern))
		for i, p := range c.Pattern {
			parts[i] = "(?:" + p + ")"
		}
		schema.Pattern = strings.Join(parts, "|")
	}

	// --- Numeric restrictions: xs:minInclusive / maxInclusive / minExclusive / maxExclusive ---
	exclusiveMin := c.ExclusiveMinimum != nil && *c.ExclusiveMinimum
	exclusiveMax := c.ExclusiveMaximum != nil && *c.ExclusiveMaximum

	if isOpenAPI31(version) {
		// 3.1 / JSON Schema 2020-12: exclusiveMinimum/Maximum carry the numeric bound directly.
		if c.Minimum != nil {
			if exclusiveMin {
				schema.ExclusiveMinimum = *c.Minimum
			} else {
				schema.Minimum = c.Minimum
			}
		}
		if c.Maximum != nil {
			if exclusiveMax {
				schema.ExclusiveMaximum = *c.Maximum
			} else {
				schema.Maximum = c.Maximum
			}
		}
	} else {
		// 3.0.x: minimum/maximum always carry the bound; exclusiveMinimum/Maximum are booleans.
		if c.Minimum != nil {
			schema.Minimum = c.Minimum
			if exclusiveMin {
				schema.ExclusiveMinimum = true
			}
		}
		if c.Maximum != nil {
			schema.Maximum = c.Maximum
			if exclusiveMax {
				schema.ExclusiveMaximum = true
			}
		}
	}

	if c.MultipleOf != nil {
		schema.MultipleOf = c.MultipleOf
	}

	// xs:totalDigits / xs:fractionDigits have no OpenAPI equivalent. Preserve them as vendor
	// extensions so the original precision is not silently lost, and derive a multipleOf from
	// fractionDigits (decimal-place precision) when one was not already supplied by the XSD.
	if c.TotalDigits != nil {
		schema.XSDTotalDigits = c.TotalDigits
	}
	if c.FractionDigits != nil {
		schema.XSDFractionDigits = c.FractionDigits
		if schema.MultipleOf == nil {
			step := math.Pow(10, -float64(*c.FractionDigits))
			schema.MultipleOf = &step
		}
	}

	// --- Array restrictions: xs:minLength/maxLength on xs:list, or wrapped repeating elements ---
	if c.MinItems != nil {
		schema.MinItems = c.MinItems
	}
	if c.MaxItems != nil {
		schema.MaxItems = c.MaxItems
	}
	if c.UniqueItems != nil {
		schema.UniqueItems = c.UniqueItems
	}

	// --- Object restrictions ---
	if c.MinProperties != nil {
		schema.MinProperties = c.MinProperties
	}
	if c.MaxProperties != nil {
		schema.MaxProperties = c.MaxProperties
	}

	if c.Const != nil && isOpenAPI31(version) {
		schema.Const = c.Const
	} else if c.Const != nil {
		// 3.0 has no "const"; a single-value enum is the equivalent restriction.
		schema.Enum = []interface{}{c.Const}
	}

	if c.Default != nil && schema.Default == nil {
		schema.Default = c.Default
	}
}

// isOpenAPI31 reports whether the requested OpenAPI version uses the 3.1.x (JSON Schema 2020-12) dialect.
func isOpenAPI31(version string) bool {
	return strings.HasPrefix(version, "3.1")
}

package xsdschematransform

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

// convertXSDToUniversal converts XSD schema to universal schema representation
func convertXSDToUniversal(xsdData string, options ConversionOptions) (*UniversalSchema, error) {
	var xsdSchema XSDSchema

	// Parse XML
	if err := xml.Unmarshal([]byte(xsdData), &xsdSchema); err != nil {
		return nil, fmt.Errorf("failed to parse XSD: %w", err)
	}

	// Shared across every by-value copy of options made while threading it through the
	// converter call tree; see the ConversionOptions.resolving doc comment.
	options.resolving = make(map[string]bool)

	// Parse any additional (xs:include/xs:import-referenced) schemas the caller supplied, so
	// named types declared only in those files can be resolved. See ConversionOptions doc.
	for i, extra := range options.AdditionalSchemas {
		var extraSchema XSDSchema
		if err := xml.Unmarshal([]byte(extra), &extraSchema); err != nil {
			return nil, fmt.Errorf("failed to parse additionalSchemas[%d]: %w", i, err)
		}
		options.parsedAdditionalSchemas = append(options.parsedAdditionalSchemas, &extraSchema)
	}

	universal := &UniversalSchema{
		Type:        "object",
		Properties:  make(map[string]*UniversalProperty),
		Definitions: make(map[string]*UniversalSchema),
	}

	// Set schema metadata
	if xsdSchema.TargetNamespace != "" {
		universal.Namespace = xsdSchema.TargetNamespace
	}

	// Process global elements
	for _, element := range xsdSchema.Elements {
		prop, err := convertXSDElement(&element, &xsdSchema, options)
		if err != nil {
			if options.SkipUnsupported {
				continue
			}
			return nil, err
		}
		universal.Properties[elementPropertyName(&element)] = prop
	}

	// Process global complex types
	for _, complexType := range xsdSchema.ComplexTypes {
		schema, err := convertXSDComplexTypeDef(&complexType, &xsdSchema, options)
		if err != nil {
			if options.SkipUnsupported {
				continue
			}
			return nil, err
		}
		universal.Definitions[complexType.Name] = schema
	}

	// Process global simple types
	for _, simpleType := range xsdSchema.SimpleTypes {
		schema, err := convertXSDSimpleTypeDef(&simpleType, &xsdSchema, options)
		if err != nil {
			if options.SkipUnsupported {
				continue
			}
			return nil, err
		}
		universal.Definitions[simpleType.Name] = schema
	}

	return universal, nil
}

// convertXSDElement converts an XSD element to universal property
func convertXSDElement(element *XSDElement, schema *XSDSchema, options ConversionOptions) (*UniversalProperty, error) {
	prop := &UniversalProperty{}

	// Handle occurrence constraints
	if element.MinOccurs != "" {
		if minOccurs, err := strconv.Atoi(element.MinOccurs); err == nil {
			prop.MinOccurs = &minOccurs
		}
	}

	if element.MaxOccurs != "" {
		if element.MaxOccurs == "unbounded" {
			unlimited := -1
			prop.MaxOccurs = &unlimited
		} else if maxOccurs, err := strconv.Atoi(element.MaxOccurs); err == nil {
			prop.MaxOccurs = &maxOccurs
		}
	}

	// Handle nillable
	if element.Nillable == "true" {
		prop.Nullable = true
	}

	// Handle default and fixed values
	if element.Default != "" {
		prop.Default = element.Default
	}
	if element.Fixed != "" {
		prop.Fixed = element.Fixed
	}

	// Handle documentation
	if element.Annotation != nil {
		for _, doc := range element.Annotation.Documentation {
			if prop.Description == "" {
				prop.Description = doc.Content
			} else {
				prop.Description += "\n" + doc.Content
			}
		}
	}

	// Determine type
	if element.Type != "" {
		// Reference to existing type (built-in, or a named complexType/simpleType)
		universalType, err := mapXSDTypeToUniversal(element.Type, schema, options)
		if err != nil {
			return nil, err
		}
		prop.Type = universalType.Type
		prop.Format = universalType.Format
		prop.Constraints = universalType.Constraints
		prop.EnumValues = universalType.EnumValues
		prop.Properties = universalType.Properties
		prop.AdditionalProperties = universalType.AdditionalProperties
		prop.OneOf = universalType.OneOf
		prop.AnyOf = universalType.AnyOf
		prop.AllOf = universalType.AllOf
		prop.Items = universalType.Items

	} else if element.ComplexType != nil {
		// Inline complex type
		complexSchema, err := convertXSDComplexType(element.ComplexType, schema, options)
		if err != nil {
			return nil, err
		}
		prop.Type = complexSchema.Type
		prop.Properties = complexSchema.Properties
		prop.AdditionalProperties = complexSchema.AdditionalProperties
		prop.OneOf = complexSchema.OneOf
		prop.AnyOf = complexSchema.AnyOf
		prop.AllOf = complexSchema.AllOf

	} else if element.SimpleType != nil {
		// Inline simple type
		simpleSchema, err := convertXSDSimpleType(element.SimpleType, schema, options)
		if err != nil {
			return nil, err
		}
		prop.Type = simpleSchema.Type
		prop.Format = simpleSchema.Format
		prop.Constraints = simpleSchema.Constraints
		prop.EnumValues = simpleSchema.EnumValues
		prop.Items = simpleSchema.Items
		prop.OneOf = simpleSchema.OneOf

	} else if element.Ref != "" {
		// <xs:element ref="..."/>: expand the referenced global <xs:element name="..."> declaration.
		// Occurrence/nillable/default/fixed set at THIS reference site (already handled above) take
		// precedence; only the type/structure and any description not overridden here are inherited.
		if resolved, found := resolveGlobalElement(element.Ref, schema, options); found {
			key := "elem:" + resolved.Name
			if options.resolving[key] {
				// Cycle: truncate here with an opaque object instead of recursing forever.
				prop.Type = "object"
			} else {
				options.resolving[key] = true
				resolvedProp, err := convertXSDElement(resolved, schema, options)
				delete(options.resolving, key)
				if err != nil {
					return nil, err
				}
				prop.Type = resolvedProp.Type
				prop.Format = resolvedProp.Format
				prop.Constraints = resolvedProp.Constraints
				prop.EnumValues = resolvedProp.EnumValues
				prop.Properties = resolvedProp.Properties
				prop.AdditionalProperties = resolvedProp.AdditionalProperties
				prop.OneOf = resolvedProp.OneOf
				prop.AnyOf = resolvedProp.AnyOf
				prop.AllOf = resolvedProp.AllOf
				prop.Items = resolvedProp.Items
				if prop.Default == nil {
					prop.Default = resolvedProp.Default
				}
				if prop.Description == "" {
					prop.Description = resolvedProp.Description
				}
			}
		} else if options.SkipUnsupported {
			prop.Type = "string"
		} else {
			return nil, fmt.Errorf("unresolved element reference: %s", element.Ref)
		}

	} else {
		// Default to string if no type specified
		prop.Type = "string"
	}

	return prop, nil
}

// convertXSDComplexType converts XSD complex type to universal schema
func convertXSDComplexType(complexType *XSDComplexType, schema *XSDSchema, options ConversionOptions) (*UniversalSchema, error) {
	universal := &UniversalSchema{
		Type:       "object",
		Properties: make(map[string]*UniversalProperty),
	}

	// Handle mixed content
	if complexType.Mixed == "true" {
		universal.Mixed = true
	}

	// Handle documentation
	if complexType.Annotation != nil {
		for _, doc := range complexType.Annotation.Documentation {
			if universal.Description == "" {
				universal.Description = doc.Content
			} else {
				universal.Description += "\n" + doc.Content
			}
		}
	}

	// Process content model
	if complexType.Sequence != nil {
		err := processXSDSequence(complexType.Sequence, universal, schema, options)
		if err != nil {
			return nil, err
		}
	}

	if complexType.Choice != nil {
		err := processXSDChoice(complexType.Choice, universal, schema, options)
		if err != nil {
			return nil, err
		}
	}

	if complexType.All != nil {
		err := processXSDAll(complexType.All, universal, schema, options)
		if err != nil {
			return nil, err
		}
	}

	if complexType.Group != nil {
		if err := processXSDGroupRef(complexType.Group, universal, schema, options); err != nil {
			return nil, err
		}
	}

	// Process attributes
	for _, attr := range complexType.Attributes {
		prop, err := convertXSDAttribute(&attr, schema, options)
		if err != nil {
			if options.SkipUnsupported {
				continue
			}
			return nil, err
		}
		universal.Properties[attributePropertyName(&attr)] = prop
	}

	if complexType.AttributeGroup != nil {
		if err := processXSDAttributeGroupRef(complexType.AttributeGroup, universal, schema, options); err != nil {
			return nil, err
		}
	}

	// Handle simple content
	if complexType.SimpleContent != nil {
		err := processXSDSimpleContent(complexType.SimpleContent, universal, schema, options)
		if err != nil {
			return nil, err
		}
	}

	// Handle complex content
	if complexType.ComplexContent != nil {
		err := processXSDComplexContent(complexType.ComplexContent, universal, schema, options)
		if err != nil {
			return nil, err
		}
	}

	applyStrictAdditionalProperties(universal, hasWildcardContent(complexType.Sequence, complexType.Choice, complexType.AnyAttribute), options)

	return universal, nil
}

// convertXSDComplexTypeDef converts named XSD complex type to universal schema
func convertXSDComplexTypeDef(complexType *XSDComplexTypeDef, schema *XSDSchema, options ConversionOptions) (*UniversalSchema, error) {
	universal := &UniversalSchema{
		Type:       "object",
		Properties: make(map[string]*UniversalProperty),
	}

	// Handle mixed content
	if complexType.Mixed == "true" {
		universal.Mixed = true
	}

	// Handle documentation
	if complexType.Annotation != nil {
		for _, doc := range complexType.Annotation.Documentation {
			if universal.Description == "" {
				universal.Description = doc.Content
			} else {
				universal.Description += "\n" + doc.Content
			}
		}
	}

	// Process content model (same as inline complex type)
	if complexType.Sequence != nil {
		err := processXSDSequence(complexType.Sequence, universal, schema, options)
		if err != nil {
			return nil, err
		}
	}

	if complexType.Choice != nil {
		err := processXSDChoice(complexType.Choice, universal, schema, options)
		if err != nil {
			return nil, err
		}
	}

	if complexType.All != nil {
		err := processXSDAll(complexType.All, universal, schema, options)
		if err != nil {
			return nil, err
		}
	}

	if complexType.Group != nil {
		if err := processXSDGroupRef(complexType.Group, universal, schema, options); err != nil {
			return nil, err
		}
	}

	// Process attributes
	for _, attr := range complexType.Attributes {
		prop, err := convertXSDAttribute(&attr, schema, options)
		if err != nil {
			if options.SkipUnsupported {
				continue
			}
			return nil, err
		}
		universal.Properties[attributePropertyName(&attr)] = prop
	}

	if complexType.AttributeGroup != nil {
		if err := processXSDAttributeGroupRef(complexType.AttributeGroup, universal, schema, options); err != nil {
			return nil, err
		}
	}

	// Handle simple content
	if complexType.SimpleContent != nil {
		err := processXSDSimpleContent(complexType.SimpleContent, universal, schema, options)
		if err != nil {
			return nil, err
		}
	}

	// Handle complex content
	if complexType.ComplexContent != nil {
		err := processXSDComplexContent(complexType.ComplexContent, universal, schema, options)
		if err != nil {
			return nil, err
		}
	}

	applyStrictAdditionalProperties(universal, hasWildcardContent(complexType.Sequence, complexType.Choice, complexType.AnyAttribute), options)

	return universal, nil
}

// convertXSDSimpleType converts XSD simple type to universal schema
func convertXSDSimpleType(simpleType *XSDSimpleType, schema *XSDSchema, options ConversionOptions) (*UniversalSchema, error) {
	universal := &UniversalSchema{}

	// Handle documentation
	if simpleType.Annotation != nil {
		for _, doc := range simpleType.Annotation.Documentation {
			if universal.Description == "" {
				universal.Description = doc.Content
			} else {
				universal.Description += "\n" + doc.Content
			}
		}
	}

	if simpleType.Restriction != nil {
		err := processXSDRestriction(simpleType.Restriction, universal, schema, options)
		if err != nil {
			return nil, err
		}
	}

	if err := applyXSDListAndUnion(simpleType.List, simpleType.Union, universal, schema, options); err != nil {
		return nil, err
	}

	return universal, nil
}

// convertXSDSimpleTypeDef converts named XSD simple type to universal schema
func convertXSDSimpleTypeDef(simpleType *XSDSimpleTypeDef, schema *XSDSchema, options ConversionOptions) (*UniversalSchema, error) {
	universal := &UniversalSchema{}

	// Handle documentation
	if simpleType.Annotation != nil {
		for _, doc := range simpleType.Annotation.Documentation {
			if universal.Description == "" {
				universal.Description = doc.Content
			} else {
				universal.Description += "\n" + doc.Content
			}
		}
	}

	if simpleType.Restriction != nil {
		err := processXSDRestriction(simpleType.Restriction, universal, schema, options)
		if err != nil {
			return nil, err
		}
	}

	if err := applyXSDListAndUnion(simpleType.List, simpleType.Union, universal, schema, options); err != nil {
		return nil, err
	}

	return universal, nil
}

// applyXSDListAndUnion converts an xs:list / xs:union facet into the equivalent universal
// array/oneOf representation. Item and member types may be a named type reference (resolved via
// mapXSDTypeToUniversal, including named complex/simple types) or an inline anonymous simpleType.
func applyXSDListAndUnion(list *XSDList, union *XSDUnion, universal *UniversalSchema, schema *XSDSchema, options ConversionOptions) error {
	if list != nil {
		universal.Type = "array"
		switch {
		case list.ItemType != "":
			itemSchema, err := mapXSDTypeToUniversal(list.ItemType, schema, options)
			if err != nil {
				return err
			}
			universal.Items = itemSchema
		case list.SimpleType != nil:
			itemSchema, err := convertXSDSimpleType(list.SimpleType, schema, options)
			if err != nil {
				return err
			}
			universal.Items = itemSchema
		}
	}

	if union != nil {
		universal.OneOf = []*UniversalSchema{}
		if union.MemberTypes != "" {
			for _, memberType := range strings.Fields(union.MemberTypes) {
				memberSchema, err := mapXSDTypeToUniversal(memberType, schema, options)
				if err != nil {
					if options.SkipUnsupported {
						continue
					}
					return err
				}
				universal.OneOf = append(universal.OneOf, memberSchema)
			}
		}
		for i := range union.SimpleTypes {
			memberSchema, err := convertXSDSimpleType(&union.SimpleTypes[i], schema, options)
			if err != nil {
				if options.SkipUnsupported {
					continue
				}
				return err
			}
			universal.OneOf = append(universal.OneOf, memberSchema)
		}
	}

	return nil
}

// convertXSDAttribute converts XSD attribute to universal property
func convertXSDAttribute(attr *XSDAttribute, schema *XSDSchema, options ConversionOptions) (*UniversalProperty, error) {
	prop := &UniversalProperty{}

	// Handle required
	if attr.Use == "required" {
		required := true
		prop.Required = &required
	}

	// Handle default and fixed values
	if attr.Default != "" {
		prop.Default = attr.Default
	}
	if attr.Fixed != "" {
		prop.Fixed = attr.Fixed
	}

	// Handle documentation
	if attr.Annotation != nil {
		for _, doc := range attr.Annotation.Documentation {
			if prop.Description == "" {
				prop.Description = doc.Content
			} else {
				prop.Description += "\n" + doc.Content
			}
		}
	}

	// Determine type
	if attr.Type != "" {
		universalType, err := mapXSDTypeToUniversal(attr.Type, schema, options)
		if err != nil {
			return nil, err
		}
		prop.Type = universalType.Type
		prop.Format = universalType.Format
		prop.Constraints = universalType.Constraints
		prop.EnumValues = universalType.EnumValues
		prop.Items = universalType.Items
		prop.OneOf = universalType.OneOf

	} else if attr.SimpleType != nil {
		simpleSchema, err := convertXSDSimpleType(attr.SimpleType, schema, options)
		if err != nil {
			return nil, err
		}
		prop.Type = simpleSchema.Type
		prop.Format = simpleSchema.Format
		prop.Constraints = simpleSchema.Constraints
		prop.EnumValues = simpleSchema.EnumValues
		prop.Items = simpleSchema.Items
		prop.OneOf = simpleSchema.OneOf

	} else if attr.Ref != "" {
		// <xs:attribute ref="..."/>: expand the referenced global <xs:attribute name="..."> declaration.
		// use="required" set at THIS reference site (already handled above) takes precedence.
		if resolved, found := resolveGlobalAttribute(attr.Ref, schema, options); found {
			key := "attr:" + resolved.Name
			if options.resolving[key] {
				prop.Type = "string"
			} else {
				options.resolving[key] = true
				resolvedProp, err := convertXSDAttribute(resolved, schema, options)
				delete(options.resolving, key)
				if err != nil {
					return nil, err
				}
				prop.Type = resolvedProp.Type
				prop.Format = resolvedProp.Format
				prop.Constraints = resolvedProp.Constraints
				prop.EnumValues = resolvedProp.EnumValues
				prop.Items = resolvedProp.Items
				prop.OneOf = resolvedProp.OneOf
				if prop.Default == nil {
					prop.Default = resolvedProp.Default
				}
				if prop.Description == "" {
					prop.Description = resolvedProp.Description
				}
			}
		} else if options.SkipUnsupported {
			prop.Type = "string"
		} else {
			return nil, fmt.Errorf("unresolved attribute reference: %s", attr.Ref)
		}

	} else {
		// Default to string for attributes
		prop.Type = "string"
	}

	return prop, nil
}

// processXSDSequence processes XSD sequence
func processXSDSequence(sequence *XSDSequence, universal *UniversalSchema, schema *XSDSchema, options ConversionOptions) error {
	for _, element := range sequence.Elements {
		prop, err := convertXSDElement(&element, schema, options)
		if err != nil {
			if options.SkipUnsupported {
				continue
			}
			return err
		}
		universal.Properties[elementPropertyName(&element)] = prop
	}

	// Process nested sequences and choices
	for _, nestedChoice := range sequence.Choices {
		err := processXSDChoice(&nestedChoice, universal, schema, options)
		if err != nil {
			return err
		}
	}

	for _, nestedSequence := range sequence.Sequences {
		err := processXSDSequence(&nestedSequence, universal, schema, options)
		if err != nil {
			return err
		}
	}

	// <xs:group ref="..."/> particles: merge the referenced group's own content model in directly.
	for _, groupRef := range sequence.Groups {
		if err := processXSDGroupRef(&groupRef, universal, schema, options); err != nil {
			return err
		}
	}

	return nil
}

// processXSDChoice processes XSD choice
func processXSDChoice(choice *XSDChoice, universal *UniversalSchema, schema *XSDSchema, options ConversionOptions) error {
	// For choices, we create a oneOf structure
	if universal.OneOf == nil {
		universal.OneOf = []*UniversalSchema{}
	}

	for _, element := range choice.Elements {
		choiceSchema := &UniversalSchema{
			Type:       "object",
			Properties: make(map[string]*UniversalProperty),
		}

		prop, err := convertXSDElement(&element, schema, options)
		if err != nil {
			if options.SkipUnsupported {
				continue
			}
			return err
		}
		choiceSchema.Properties[elementPropertyName(&element)] = prop
		universal.OneOf = append(universal.OneOf, choiceSchema)
	}

	// A nested <xs:sequence> branch: its whole element group is a single alternative.
	for _, nestedSequence := range choice.Sequences {
		branch := &UniversalSchema{Type: "object", Properties: make(map[string]*UniversalProperty)}
		if err := processXSDSequence(&nestedSequence, branch, schema, options); err != nil {
			return err
		}
		universal.OneOf = append(universal.OneOf, branch)
	}

	// A nested <xs:choice> flattens into the parent's alternatives rather than nesting oneOf-in-oneOf.
	for _, nestedChoice := range choice.Choices {
		nestedUniversal := &UniversalSchema{}
		if err := processXSDChoice(&nestedChoice, nestedUniversal, schema, options); err != nil {
			return err
		}
		universal.OneOf = append(universal.OneOf, nestedUniversal.OneOf...)
	}

	// A <xs:group ref="..."/> branch: its content model becomes one alternative, unless the
	// referenced group's own content is itself a choice, in which case it flattens in too.
	for _, groupRef := range choice.Groups {
		group, found := resolveGroupDef(groupRef.Ref, schema, options)
		if !found {
			if options.SkipUnsupported {
				continue
			}
			return fmt.Errorf("unresolved group reference: %s", groupRef.Ref)
		}
		key := "group:" + group.Name
		if options.resolving[key] {
			continue // cycle - skip re-expansion
		}
		options.resolving[key] = true

		if group.Choice != nil {
			nestedUniversal := &UniversalSchema{}
			err := processXSDChoice(group.Choice, nestedUniversal, schema, options)
			delete(options.resolving, key)
			if err != nil {
				return err
			}
			universal.OneOf = append(universal.OneOf, nestedUniversal.OneOf...)
			continue
		}

		branch := &UniversalSchema{Type: "object", Properties: make(map[string]*UniversalProperty)}
		var err error
		if group.Sequence != nil {
			err = processXSDSequence(group.Sequence, branch, schema, options)
		} else if group.All != nil {
			err = processXSDAll(group.All, branch, schema, options)
		}
		delete(options.resolving, key)
		if err != nil {
			return err
		}
		universal.OneOf = append(universal.OneOf, branch)
	}

	return nil
}

// processXSDAll processes XSD all
func processXSDAll(all *XSDAll, universal *UniversalSchema, schema *XSDSchema, options ConversionOptions) error {
	// xs:all is similar to sequence but order doesn't matter
	for _, element := range all.Elements {
		prop, err := convertXSDElement(&element, schema, options)
		if err != nil {
			if options.SkipUnsupported {
				continue
			}
			return err
		}
		universal.Properties[elementPropertyName(&element)] = prop
	}

	return nil
}

// processXSDRestriction processes XSD restriction facets
func processXSDRestriction(restriction *XSDRestriction, universal *UniversalSchema, schema *XSDSchema, options ConversionOptions) error {
	// Map base type
	baseSchema, err := mapXSDTypeToUniversal(restriction.Base, schema, options)
	if err != nil {
		return err
	}

	universal.Type = baseSchema.Type
	universal.Format = baseSchema.Format

	if universal.Constraints == nil {
		universal.Constraints = &UniversalConstraints{}
	}

	// Process facets
	if restriction.MinLength != nil {
		if minLen, err := strconv.Atoi(restriction.MinLength.Value); err == nil {
			universal.Constraints.MinLength = &minLen
		}
	}

	if restriction.MaxLength != nil {
		if maxLen, err := strconv.Atoi(restriction.MaxLength.Value); err == nil {
			universal.Constraints.MaxLength = &maxLen
		}
	}

	if restriction.Length != nil {
		if length, err := strconv.Atoi(restriction.Length.Value); err == nil {
			universal.Constraints.MinLength = &length
			universal.Constraints.MaxLength = &length
		}
	}

	if restriction.MinInclusive != nil {
		if minVal, err := strconv.ParseFloat(restriction.MinInclusive.Value, 64); err == nil {
			universal.Constraints.Minimum = &minVal
		}
	}

	if restriction.MaxInclusive != nil {
		if maxVal, err := strconv.ParseFloat(restriction.MaxInclusive.Value, 64); err == nil {
			universal.Constraints.Maximum = &maxVal
		}
	}

	if restriction.MinExclusive != nil {
		if minVal, err := strconv.ParseFloat(restriction.MinExclusive.Value, 64); err == nil {
			exclusive := true
			universal.Constraints.Minimum = &minVal
			universal.Constraints.ExclusiveMinimum = &exclusive
		}
	}

	if restriction.MaxExclusive != nil {
		if maxVal, err := strconv.ParseFloat(restriction.MaxExclusive.Value, 64); err == nil {
			exclusive := true
			universal.Constraints.Maximum = &maxVal
			universal.Constraints.ExclusiveMaximum = &exclusive
		}
	}

	if restriction.TotalDigits != nil {
		if totalDigits, err := strconv.Atoi(restriction.TotalDigits.Value); err == nil {
			universal.Constraints.TotalDigits = &totalDigits
		}
	}

	if restriction.FractionDigits != nil {
		if fractionDigits, err := strconv.Atoi(restriction.FractionDigits.Value); err == nil {
			universal.Constraints.FractionDigits = &fractionDigits
		}
	}

	// Handle patterns
	if len(restriction.Pattern) > 0 {
		patterns := make([]string, len(restriction.Pattern))
		for i, pattern := range restriction.Pattern {
			patterns[i] = pattern.Value
		}
		universal.Constraints.Pattern = patterns
	}

	// Handle enumerations
	if len(restriction.Enumerations) > 0 {
		enumValues := make([]interface{}, len(restriction.Enumerations))
		for i, enum := range restriction.Enumerations {
			enumValues[i] = enum.Value
		}
		universal.EnumValues = enumValues
	}

	return nil
}

// processXSDSimpleContent processes XSD simple content
func processXSDSimpleContent(simpleContent *XSDSimpleContent, universal *UniversalSchema, schema *XSDSchema, options ConversionOptions) error {
	if simpleContent.Extension != nil {
		// Base type becomes the content type
		baseSchema, err := mapXSDTypeToUniversal(simpleContent.Extension.Base, schema, options)
		if err != nil {
			return err
		}

		// Add content property
		universal.Properties["_content"] = &UniversalProperty{
			Type:   baseSchema.Type,
			Format: baseSchema.Format,
		}

		// Process attributes
		for _, attr := range simpleContent.Extension.Attributes {
			prop, err := convertXSDAttribute(&attr, schema, options)
			if err != nil {
				if options.SkipUnsupported {
					continue
				}
				return err
			}
			universal.Properties[attributePropertyName(&attr)] = prop
		}
	}

	if simpleContent.Restriction != nil {
		// Similar to extension but with restrictions
		baseSchema, err := mapXSDTypeToUniversal(simpleContent.Restriction.Base, schema, options)
		if err != nil {
			return err
		}

		universal.Properties["_content"] = &UniversalProperty{
			Type:   baseSchema.Type,
			Format: baseSchema.Format,
		}

		// Apply restrictions to content
		if len(simpleContent.Restriction.Enumerations) > 0 {
			enumValues := make([]interface{}, len(simpleContent.Restriction.Enumerations))
			for i, enum := range simpleContent.Restriction.Enumerations {
				enumValues[i] = enum.Value
			}
			universal.Properties["_content"].EnumValues = enumValues
		}

		// Process attributes
		for _, attr := range simpleContent.Restriction.Attributes {
			prop, err := convertXSDAttribute(&attr, schema, options)
			if err != nil {
				if options.SkipUnsupported {
					continue
				}
				return err
			}
			universal.Properties[attributePropertyName(&attr)] = prop
		}
	}

	return nil
}

// processXSDComplexContent processes XSD complex content
func processXSDComplexContent(complexContent *XSDComplexContent, universal *UniversalSchema, schema *XSDSchema, options ConversionOptions) error {
	if complexContent.Extension != nil {
		// xs:extension = base type's own members plus whatever new content is declared here.
		if complexContent.Extension.Base != "" {
			baseSchema, err := mapXSDTypeToUniversal(complexContent.Extension.Base, schema, options)
			if err != nil {
				if !options.SkipUnsupported {
					return err
				}
			} else {
				for name, baseProp := range baseSchema.Properties {
					universal.Properties[name] = baseProp
				}
				if baseSchema.AdditionalProperties != nil && universal.AdditionalProperties == nil {
					universal.AdditionalProperties = baseSchema.AdditionalProperties
				}
			}
		}

		// Process new content
		if complexContent.Extension.Sequence != nil {
			err := processXSDSequence(complexContent.Extension.Sequence, universal, schema, options)
			if err != nil {
				return err
			}
		}

		if complexContent.Extension.Choice != nil {
			err := processXSDChoice(complexContent.Extension.Choice, universal, schema, options)
			if err != nil {
				return err
			}
		}

		// Process attributes
		for _, attr := range complexContent.Extension.Attributes {
			prop, err := convertXSDAttribute(&attr, schema, options)
			if err != nil {
				if options.SkipUnsupported {
					continue
				}
				return err
			}
			universal.Properties[attributePropertyName(&attr)] = prop
		}
	}

	if complexContent.Restriction != nil {
		// Similar to extension but restricts base type
		if complexContent.Restriction.Sequence != nil {
			err := processXSDSequence(complexContent.Restriction.Sequence, universal, schema, options)
			if err != nil {
				return err
			}
		}

		if complexContent.Restriction.Choice != nil {
			err := processXSDChoice(complexContent.Restriction.Choice, universal, schema, options)
			if err != nil {
				return err
			}
		}

		// Process attributes
		for _, attr := range complexContent.Restriction.Attributes {
			prop, err := convertXSDAttribute(&attr, schema, options)
			if err != nil {
				if options.SkipUnsupported {
					continue
				}
				return err
			}
			universal.Properties[attributePropertyName(&attr)] = prop
		}
	}

	return nil
}

// mapXSDTypeToUniversal maps XSD built-in types to universal schema types
func mapXSDTypeToUniversal(xsdType string, schema *XSDSchema, options ConversionOptions) (*UniversalSchema, error) {
	// Remove namespace prefix if present
	if colonIndex := strings.LastIndex(xsdType, ":"); colonIndex >= 0 {
		xsdType = xsdType[colonIndex+1:]
	}

	universal := &UniversalSchema{}

	switch xsdType {
	// String types
	case "string", "normalizedString", "token", "NMTOKEN", "NMTOKENS", "Name", "NCName", "ID", "IDREF", "IDREFS", "ENTITY", "ENTITIES", "language":
		universal.Type = "string"

	// Numeric types
	case "decimal":
		universal.Type = "number"
		universal.Format = "decimal"
	case "integer", "nonPositiveInteger", "negativeInteger", "long", "int", "short", "byte", "nonNegativeInteger", "unsignedLong", "unsignedInt", "unsignedShort", "unsignedByte", "positiveInteger":
		universal.Type = "integer"
	case "double":
		universal.Type = "number"
		universal.Format = "double"
	case "float":
		universal.Type = "number"
		universal.Format = "float"

	// Boolean type
	case "boolean":
		universal.Type = "boolean"

	// Date/time types
	case "dateTime":
		universal.Type = "string"
		universal.Format = "date-time"
	case "date":
		universal.Type = "string"
		universal.Format = "date"
	case "time":
		universal.Type = "string"
		universal.Format = "time"
	case "duration":
		universal.Type = "string"
		universal.Format = "duration"
	case "gYear":
		universal.Type = "string"
		universal.Format = "year"
	case "gMonth":
		universal.Type = "string"
		universal.Format = "month"
	case "gDay":
		universal.Type = "string"
		universal.Format = "day"
	case "gYearMonth":
		universal.Type = "string"
		universal.Format = "year-month"
	case "gMonthDay":
		universal.Type = "string"
		universal.Format = "month-day"

	// Binary types
	case "base64Binary":
		universal.Type = "string"
		universal.Format = "base64"
	case "hexBinary":
		universal.Type = "string"
		universal.Format = "hex"

	// URI type
	case "anyURI":
		universal.Type = "string"
		universal.Format = "uri"

	// QName type
	case "QName":
		universal.Type = "string"
		universal.Format = "qname"

	// NOTATION type
	case "NOTATION":
		universal.Type = "string"
		universal.Format = "notation"

	// anyType and anySimpleType
	case "anyType":
		// Leave type unspecified to allow any type
		return universal, nil
	case "anySimpleType":
		// Union of all simple types
		universal.OneOf = []*UniversalSchema{
			{Type: "string"},
			{Type: "number"},
			{Type: "integer"},
			{Type: "boolean"},
		}

	default:
		if resolved, found, err := resolveNamedXSDType(xsdType, schema, options); found {
			return resolved, err
		}
		// Genuinely unknown type (e.g. a typo) rather than an unresolved named reference.
		if options.SkipUnsupported {
			universal.Type = "string" // Default fallback
		} else {
			return nil, fmt.Errorf("unsupported XSD type: %s", xsdType)
		}
	}

	return universal, nil
}

// resolveNamedXSDType looks up a named <xs:complexType>/<xs:simpleType> declared in the schema and
// converts it. It guards against direct/indirect self-reference (a type that contains an element,
// list item, or union member of its own type) via options.resolving, since the universal schema
// model has no $ref/pointer mechanism and would otherwise recurse until the stack overflows.
// The bool return reports whether xsdType matched a named type at all (as opposed to being unknown).
// Lookup searches schema first, then any options.AdditionalSchemas (xs:include/xs:import content
// supplied by the caller) so types split across files can still be found. Matching is by local
// name only across all schemas (namespaces are not distinguished), consistent with this activity's
// namespaceHandling="ignore" default.
func resolveNamedXSDType(typeName string, schema *XSDSchema, options ConversionOptions) (*UniversalSchema, bool, error) {
	if resolved, found, err := resolveNamedXSDTypeInSchema(typeName, schema, options); found {
		return resolved, found, err
	}
	for _, extra := range options.parsedAdditionalSchemas {
		if extra == schema {
			continue
		}
		if resolved, found, err := resolveNamedXSDTypeInSchema(typeName, extra, options); found {
			return resolved, found, err
		}
	}
	return nil, false, nil
}

// resolveNamedXSDTypeInSchema is the single-schema lookup used by resolveNamedXSDType.
func resolveNamedXSDTypeInSchema(typeName string, schema *XSDSchema, options ConversionOptions) (*UniversalSchema, bool, error) {
	for _, ct := range schema.ComplexTypes {
		if ct.Name != typeName {
			continue
		}
		if options.resolving[typeName] {
			// Cycle: truncate here with an opaque object instead of recursing forever.
			return &UniversalSchema{Type: "object"}, true, nil
		}
		options.resolving[typeName] = true
		defer delete(options.resolving, typeName)
		resolved, err := convertXSDComplexTypeDef(&ct, schema, options)
		return resolved, true, err
	}

	for _, st := range schema.SimpleTypes {
		if st.Name != typeName {
			continue
		}
		if options.resolving[typeName] {
			return &UniversalSchema{Type: "string"}, true, nil
		}
		options.resolving[typeName] = true
		defer delete(options.resolving, typeName)
		resolved, err := convertXSDSimpleTypeDef(&st, schema, options)
		return resolved, true, err
	}

	return nil, false, nil
}

// localName strips a namespace prefix from a QName-style reference (e.g. "tns:Foo" -> "Foo").
func localName(qname string) string {
	if idx := strings.LastIndex(qname, ":"); idx >= 0 {
		return qname[idx+1:]
	}
	return qname
}

// elementPropertyName returns the map key for an XSD element particle: its own name, or - for a
// <xs:element ref="..."/> particle, which has no name of its own - the local name of the
// referenced global element.
func elementPropertyName(element *XSDElement) string {
	if element.Name != "" {
		return element.Name
	}
	return localName(element.Ref)
}

// attributePropertyName returns the map key for an XSD attribute particle: its own name, or -
// for a <xs:attribute ref="..."/> particle - the local name of the referenced global attribute.
func attributePropertyName(attr *XSDAttribute) string {
	if attr.Name != "" {
		return attr.Name
	}
	return localName(attr.Ref)
}

// resolveGlobalElement looks up a top-level <xs:element name="..."> declaration referenced by a
// local <xs:element ref="..."/> particle, searching schema first then any additionalSchemas.
func resolveGlobalElement(ref string, schema *XSDSchema, options ConversionOptions) (*XSDElement, bool) {
	name := localName(ref)
	for i := range schema.Elements {
		if schema.Elements[i].Name == name {
			return &schema.Elements[i], true
		}
	}
	for _, extra := range options.parsedAdditionalSchemas {
		if extra == schema {
			continue
		}
		for i := range extra.Elements {
			if extra.Elements[i].Name == name {
				return &extra.Elements[i], true
			}
		}
	}
	return nil, false
}

// resolveGlobalAttribute looks up a top-level <xs:attribute name="..."> declaration referenced
// by a local <xs:attribute ref="..."/> particle.
func resolveGlobalAttribute(ref string, schema *XSDSchema, options ConversionOptions) (*XSDAttribute, bool) {
	name := localName(ref)
	for i := range schema.Attributes {
		if schema.Attributes[i].Name == name {
			return &schema.Attributes[i], true
		}
	}
	for _, extra := range options.parsedAdditionalSchemas {
		if extra == schema {
			continue
		}
		for i := range extra.Attributes {
			if extra.Attributes[i].Name == name {
				return &extra.Attributes[i], true
			}
		}
	}
	return nil, false
}

// resolveGroupDef looks up a named <xs:group name="..."> declaration referenced by <xs:group ref="...">.
func resolveGroupDef(ref string, schema *XSDSchema, options ConversionOptions) (*XSDGroup, bool) {
	name := localName(ref)
	for i := range schema.Groups {
		if schema.Groups[i].Name == name {
			return &schema.Groups[i], true
		}
	}
	for _, extra := range options.parsedAdditionalSchemas {
		if extra == schema {
			continue
		}
		for i := range extra.Groups {
			if extra.Groups[i].Name == name {
				return &extra.Groups[i], true
			}
		}
	}
	return nil, false
}

// resolveAttributeGroupDef looks up a named <xs:attributeGroup name="..."> declaration referenced
// by <xs:attributeGroup ref="...">.
func resolveAttributeGroupDef(ref string, schema *XSDSchema, options ConversionOptions) (*XSDAttributeGroup, bool) {
	name := localName(ref)
	for i := range schema.AttributeGroups {
		if schema.AttributeGroups[i].Name == name {
			return &schema.AttributeGroups[i], true
		}
	}
	for _, extra := range options.parsedAdditionalSchemas {
		if extra == schema {
			continue
		}
		for i := range extra.AttributeGroups {
			if extra.AttributeGroups[i].Name == name {
				return &extra.AttributeGroups[i], true
			}
		}
	}
	return nil, false
}

// processXSDGroupRef resolves a <xs:group ref="..."/> particle and merges its content model
// (sequence/choice/all) directly into universal, guarding against a group that (directly or
// indirectly) references itself.
func processXSDGroupRef(ref *XSDGroupRef, universal *UniversalSchema, schema *XSDSchema, options ConversionOptions) error {
	group, found := resolveGroupDef(ref.Ref, schema, options)
	if !found {
		if options.SkipUnsupported {
			return nil
		}
		return fmt.Errorf("unresolved group reference: %s", ref.Ref)
	}
	key := "group:" + group.Name
	if options.resolving[key] {
		return nil // cycle - skip re-expansion
	}
	options.resolving[key] = true
	defer delete(options.resolving, key)

	if group.Sequence != nil {
		if err := processXSDSequence(group.Sequence, universal, schema, options); err != nil {
			return err
		}
	}
	if group.Choice != nil {
		if err := processXSDChoice(group.Choice, universal, schema, options); err != nil {
			return err
		}
	}
	if group.All != nil {
		if err := processXSDAll(group.All, universal, schema, options); err != nil {
			return err
		}
	}
	return nil
}

// processXSDAttributeGroupRef resolves a <xs:attributeGroup ref="..."/> particle and merges its
// attributes (and any nested attributeGroup refs, recursively) into universal.Properties.
func processXSDAttributeGroupRef(ref *XSDAttributeGroupRef, universal *UniversalSchema, schema *XSDSchema, options ConversionOptions) error {
	group, found := resolveAttributeGroupDef(ref.Ref, schema, options)
	if !found {
		if options.SkipUnsupported {
			return nil
		}
		return fmt.Errorf("unresolved attributeGroup reference: %s", ref.Ref)
	}
	key := "attrgroup:" + group.Name
	if options.resolving[key] {
		return nil // cycle - skip re-expansion
	}
	options.resolving[key] = true
	defer delete(options.resolving, key)

	for _, attr := range group.Attributes {
		prop, err := convertXSDAttribute(&attr, schema, options)
		if err != nil {
			if options.SkipUnsupported {
				continue
			}
			return err
		}
		universal.Properties[attributePropertyName(&attr)] = prop
	}
	for _, nested := range group.AttributeGroups {
		if err := processXSDAttributeGroupRef(&nested, universal, schema, options); err != nil {
			return err
		}
	}
	return nil
}

// hasWildcardContent reports whether a complex type's immediate content model permits arbitrary
// additional elements/attributes via xs:any / xs:anyAttribute. XSD complex types are implicitly
// "closed" (no undeclared members) unless such a wildcard is present. Wildcards nested inside a
// simpleContent/complexContent extension or restriction are not inspected (kept deliberately simple).
func hasWildcardContent(sequence *XSDSequence, choice *XSDChoice, anyAttribute *XSDAnyAttribute) bool {
	if anyAttribute != nil {
		return true
	}
	if sequence != nil && sequenceHasAny(sequence) {
		return true
	}
	if choice != nil && choiceHasAny(choice) {
		return true
	}
	return false
}

func sequenceHasAny(sequence *XSDSequence) bool {
	if len(sequence.Any) > 0 {
		return true
	}
	for i := range sequence.Choices {
		if choiceHasAny(&sequence.Choices[i]) {
			return true
		}
	}
	for i := range sequence.Sequences {
		if sequenceHasAny(&sequence.Sequences[i]) {
			return true
		}
	}
	return false
}

func choiceHasAny(choice *XSDChoice) bool {
	if len(choice.Any) > 0 {
		return true
	}
	for i := range choice.Choices {
		if choiceHasAny(&choice.Choices[i]) {
			return true
		}
	}
	for i := range choice.Sequences {
		if sequenceHasAny(&choice.Sequences[i]) {
			return true
		}
	}
	return false
}

// applyStrictAdditionalProperties sets additionalProperties:false on closed XSD complex types when
// strict mode is enabled (matching XSD's implicit closed content model), or true when a wildcard
// grants open content. Opt-in via strictAdditionalProperties so the existing (permissive) default
// output is unchanged for callers that haven't asked for it.
func applyStrictAdditionalProperties(universal *UniversalSchema, hasWildcard bool, options ConversionOptions) {
	if !options.StrictAdditionalProperties || universal.AdditionalProperties != nil {
		return
	}
	universal.AdditionalProperties = hasWildcard
}

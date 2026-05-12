package parser

import (
	"encoding/json"
	"fmt"
	"strings"

	jsonpath "github.com/oliveagle/jsonpath"
)

// JSONParser parses JSON documents and resolves paths using github.com/oliveagle/jsonpath —
// the same library used by Flogo's OOTB json.path() function.
type JSONParser struct{}

func (p *JSONParser) Parse(content string) (Document, error) {
	var root interface{}
	if err := json.Unmarshal([]byte(content), &root); err != nil {
		return nil, fmt.Errorf("JSON parse error: %w", err)
	}
	return &jsonDocument{root: root}, nil
}

type jsonDocument struct {
	root interface{}
}

func (d *jsonDocument) Root() interface{} { return d.root }

// ResolveScope expands a JSONPath expression to a slice of scope objects.
//
// Supported forms:
//   - "" or "$"            → [root]
//   - "$.key"              → [value at key]
//   - "$.arr[*]"           → each element of arr
//   - "$.arr[*].subfield"  → subfield of each element
//
// [*] wildcard expansion is handled manually on top of oliveagle/jsonpath
// because that library returns a single value, not an array expansion.
func (d *jsonDocument) ResolveScope(path string) ([]interface{}, error) {
	if path == "" || path == "$" {
		if arr, ok := d.root.([]interface{}); ok {
			return arr, nil
		}
		return []interface{}{d.root}, nil
	}

	if strings.Contains(path, "[*]") {
		return d.expandWildcard(path)
	}

	val, err := jsonpath.JsonPathLookup(d.root, path)
	if err != nil {
		return nil, nil // path not found → empty scope, not an error
	}
	if arr, ok := val.([]interface{}); ok {
		return arr, nil
	}
	return []interface{}{val}, nil
}

// expandWildcard handles paths containing [*] by splitting at the first wildcard,
// resolving the array, then applying the suffix to each element.
func (d *jsonDocument) expandWildcard(path string) ([]interface{}, error) {
	idx := strings.Index(path, "[*]")
	prefix := path[:idx]   // e.g. "$.resources"
	suffix := path[idx+3:] // e.g. ".data"  or ""

	arr, err := d.resolveArray(prefix)
	if err != nil || len(arr) == 0 {
		return nil, nil
	}

	if suffix == "" {
		return arr, nil
	}

	// suffix starts with "." — apply to each array element
	var results []interface{}
	for _, item := range arr {
		val, err := jsonpath.JsonPathLookup(item, "$"+suffix)
		if err != nil {
			continue
		}
		if sub, ok := val.([]interface{}); ok {
			results = append(results, sub...)
		} else if val != nil {
			results = append(results, val)
		}
	}
	return results, nil
}

func (d *jsonDocument) resolveArray(prefix string) ([]interface{}, error) {
	if prefix == "$" || prefix == "" {
		if arr, ok := d.root.([]interface{}); ok {
			return arr, nil
		}
		return nil, nil
	}
	val, err := jsonpath.JsonPathLookup(d.root, prefix)
	if err != nil {
		return nil, nil
	}
	arr, ok := val.([]interface{})
	if !ok {
		return nil, nil
	}
	return arr, nil
}

// ResolvePath resolves a path relative to a scope object.
// Accepts dot-notation ("activity.ref") or full JSONPath ("$.activity.ref").
func (d *jsonDocument) ResolvePath(obj interface{}, path string) (interface{}, bool) {
	if path == "" {
		return obj, true
	}

	// Ensure the path is absolute JSONPath
	jpath := path
	if !strings.HasPrefix(path, "$") {
		jpath = "$." + path
	}

	val, err := jsonpath.JsonPathLookup(obj, jpath)
	if err != nil {
		return nil, false
	}
	return val, true
}

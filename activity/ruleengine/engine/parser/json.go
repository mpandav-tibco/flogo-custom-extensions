package parser

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/ohler55/ojg/jp"
)

// compiledPaths caches parsed JSONPath expressions keyed by path string.
var compiledPaths sync.Map

func parsePath(path string) (jp.Expr, error) {
	if cached, ok := compiledPaths.Load(path); ok {
		return cached.(jp.Expr), nil
	}
	x, err := jp.ParseString(path)
	if err != nil {
		return nil, err
	}
	compiledPaths.Store(path, x)
	return x, nil
}

// JSONParser parses JSON documents and resolves paths using github.com/ohler55/ojg/jp,
// an actively maintained JSONPath library (MIT, v1.28.1+).
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
// The ojg/jp library natively handles [*] wildcards and returns all matches,
// so no manual wildcard expansion is needed.
func (d *jsonDocument) ResolveScope(path string) ([]interface{}, error) {
	if path == "" || path == "$" {
		if arr, ok := d.root.([]interface{}); ok {
			return arr, nil
		}
		return []interface{}{d.root}, nil
	}

	x, err := parsePath(path)
	if err != nil {
		return nil, fmt.Errorf("invalid JSONPath %q: %w", path, err)
	}
	results := x.Get(d.root)
	return results, nil
}

// ResolvePath resolves a path relative to a scope object.
// Accepts dot-notation ("activity.ref") or full JSONPath ("$.activity.ref").
func (d *jsonDocument) ResolvePath(obj interface{}, path string) (interface{}, bool) {
	if path == "" {
		return obj, true
	}

	jpath := path
	if !strings.HasPrefix(path, "$") {
		jpath = "$." + path
	}

	x, err := parsePath(jpath)
	if err != nil {
		return nil, false
	}
	return x.FirstFound(obj)
}

package parser

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// YAMLParser parses YAML/YML documents (Kubernetes manifests, Helm values, etc.)
// and resolves paths using dot-notation.
type YAMLParser struct{}

func (p *YAMLParser) Parse(content string) (Document, error) {
	var root interface{}
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		return nil, fmt.Errorf("YAML parse error: %w", err)
	}
	// yaml.v3 unmarshals maps as map[string]interface{}
	return &kvDocument{root: normalizeYAML(root)}, nil
}

// normalizeYAML converts map[interface{}]interface{} (produced by some YAML libs)
// to map[string]interface{} so path resolution works uniformly.
func normalizeYAML(v interface{}) interface{} {
	switch val := v.(type) {
	case map[interface{}]interface{}:
		m := make(map[string]interface{}, len(val))
		for k, v2 := range val {
			m[fmt.Sprintf("%v", k)] = normalizeYAML(v2)
		}
		return m
	case map[string]interface{}:
		for k, v2 := range val {
			val[k] = normalizeYAML(v2)
		}
		return val
	case []interface{}:
		for i, v2 := range val {
			val[i] = normalizeYAML(v2)
		}
		return val
	}
	return v
}

// KVParser parses key=value config files (EMS .conf, .properties, .env).
type KVParser struct{}

func (p *KVParser) Parse(content string) (Document, error) {
	m := make(map[string]interface{})
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		sep := strings.IndexAny(line, "=:")
		if sep < 0 {
			continue
		}
		key := strings.TrimSpace(line[:sep])
		val := strings.TrimSpace(line[sep+1:])
		// Strip inline comments marked by " #" or "\t#".
		// Find the earliest occurrence of either form.
		ci := -1
		for _, mark := range []string{" #", "\t#"} {
			if i := strings.Index(val, mark); i >= 0 && (ci < 0 || i < ci) {
				ci = i
			}
		}
		if ci >= 0 {
			val = strings.TrimSpace(val[:ci])
		}
		m[key] = val
	}
	return &kvDocument{root: m}, nil
}

// kvDocument handles both YAML and KV parsed documents using dot-notation paths.
type kvDocument struct {
	root interface{}
}

func (d *kvDocument) Root() interface{} { return d.root }

func (d *kvDocument) ResolveScope(path string) ([]interface{}, error) {
	if path == "" {
		if arr, ok := d.root.([]interface{}); ok {
			return arr, nil
		}
		return []interface{}{d.root}, nil
	}
	// Strip a trailing [*] wildcard. Rule authors familiar with JSONPath may write
	// "spec.containers[*]" to mean "iterate containers". For YAML/KV the bare path
	// "spec.containers" is sufficient — the scope resolver expands arrays
	// element-by-element. Without this stripping, dotGet fails silently because
	// parseArrayIndex rejects the non-numeric "*" index, yielding an empty scope
	// and zero findings with no error reported.
	cleanedPath := strings.TrimSuffix(path, "[*]")
	val, ok := dotGet(d.root, cleanedPath)
	if !ok {
		return nil, nil
	}
	if arr, ok := val.([]interface{}); ok {
		return arr, nil
	}
	return []interface{}{val}, nil
}

func (d *kvDocument) ResolvePath(obj interface{}, path string) (interface{}, bool) {
	if path == "" {
		return obj, true
	}
	return dotGet(obj, path)
}

// dotGet navigates a dot-separated path through nested maps and arrays.
// Supports: "spec.containers", "spec.containers[0].image"
func dotGet(obj interface{}, path string) (interface{}, bool) {
	parts := splitPath(path)
	current := obj
	for _, part := range parts {
		if current == nil {
			return nil, false
		}
		// Array index: "containers[0]"
		if idx, key, isIdx := parseArrayIndex(part); isIdx {
			m, ok := current.(map[string]interface{})
			if !ok {
				return nil, false
			}
			arr, ok := m[key].([]interface{})
			if !ok || idx >= len(arr) {
				return nil, false
			}
			current = arr[idx]
			continue
		}
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		current, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func splitPath(path string) []string {
	return strings.FieldsFunc(path, func(r rune) bool { return r == '.' })
}

func parseArrayIndex(part string) (int, string, bool) {
	open := strings.Index(part, "[")
	close := strings.Index(part, "]")
	if open < 0 || close < 0 || close < open {
		return 0, "", false
	}
	key := part[:open]
	var idx int
	if n, _ := fmt.Sscanf(part[open+1:close], "%d", &idx); n == 0 {
		return 0, "", false
	}
	if idx < 0 {
		// Negative indices are not supported; treat as not found to avoid
		// an index-out-of-range panic in the caller.
		return 0, "", false
	}
	return idx, key, true
}

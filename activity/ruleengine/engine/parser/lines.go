package parser

import (
	"strings"
)

// LinesParser treats files as an ordered slice of line objects.
// Each scope item is map{"line": "<text>", "number": <1-based int>}.
// This covers log files, plain text, and any line-oriented format.
type LinesParser struct{}

func (p *LinesParser) Parse(content string) (Document, error) {
	rawLines := strings.Split(content, "\n")
	lines := make([]interface{}, 0, len(rawLines))
	for i, l := range rawLines {
		lines = append(lines, map[string]interface{}{
			"line":   strings.TrimRight(l, "\r"),
			"number": i + 1,
		})
	}
	return &linesDocument{lines: lines}, nil
}

type linesDocument struct {
	lines []interface{}
}

func (d *linesDocument) Root() interface{} { return d.lines }

// ResolveScope returns all lines when path is empty or "$[*]",
// otherwise resolves a dot-path on the line objects.
func (d *linesDocument) ResolveScope(path string) ([]interface{}, error) {
	if path == "" || path == "$" || path == "$[*]" {
		return d.lines, nil
	}
	// Support filtering by field: "$[*].line" → extract line strings
	if path == "$[*].line" {
		out := make([]interface{}, len(d.lines))
		for i, l := range d.lines {
			if m, ok := l.(map[string]interface{}); ok {
				out[i] = m["line"]
			}
		}
		return out, nil
	}
	return d.lines, nil
}

func (d *linesDocument) ResolvePath(obj interface{}, path string) (interface{}, bool) {
	if path == "" {
		return obj, true
	}
	// obj is typically a map{"line": ..., "number": ...}
	switch v := obj.(type) {
	case map[string]interface{}:
		val, ok := v[path]
		return val, ok
	case string:
		// obj is a raw line string; only "line" path makes sense
		if path == "line" {
			return v, true
		}
		return nil, false
	}
	return nil, false
}


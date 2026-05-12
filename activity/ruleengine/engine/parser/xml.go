package parser

import (
	"fmt"
	"strings"

	"github.com/antchfx/xmlquery"
)

// XMLParser parses XML/BWP documents and resolves paths using XPath via antchfx/xmlquery
// (the same library used in the xmlfilter activity).
type XMLParser struct{}

func (p *XMLParser) Parse(content string) (Document, error) {
	doc, err := xmlquery.Parse(strings.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("XML parse error: %w", err)
	}
	return &xmlDocument{root: doc}, nil
}

type xmlDocument struct {
	root *xmlquery.Node
}

func (d *xmlDocument) Root() interface{} { return d.root }

// ResolveScope expands an XPath expression to a slice of *xmlquery.Node objects.
func (d *xmlDocument) ResolveScope(path string) ([]interface{}, error) {
	if path == "" {
		return []interface{}{d.root}, nil
	}
	nodes, err := xmlquery.QueryAll(d.root, path)
	if err != nil {
		return nil, fmt.Errorf("XPath error %q: %w", path, err)
	}
	result := make([]interface{}, len(nodes))
	for i, n := range nodes {
		result[i] = n
	}
	return result, nil
}

// ResolvePath resolves an XPath expression relative to a scope node.
// obj must be a *xmlquery.Node returned by ResolveScope.
func (d *xmlDocument) ResolvePath(obj interface{}, path string) (interface{}, bool) {
	node, ok := obj.(*xmlquery.Node)
	if !ok {
		return nil, false
	}
	if path == "" {
		return node.InnerText(), true
	}
	found, err := xmlquery.Query(node, path)
	if err != nil || found == nil {
		// Try attribute lookup (e.g. "@enabled")
		if strings.HasPrefix(path, "@") {
			attrName := strings.TrimPrefix(path, "@")
			for _, attr := range node.Attr {
				if attr.Name.Local == attrName {
					return attr.Value, true
				}
			}
		}
		return nil, false
	}
	return found.InnerText(), true
}

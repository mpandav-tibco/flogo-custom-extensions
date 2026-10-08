package evaluator

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"text/template"
)

// compiledTemplates caches parsed templates keyed by template string.
// Template.Execute is safe for concurrent use; parsing is the expensive step.
var compiledTemplates sync.Map

// TemplateContext holds the variables available inside location/recommendation templates.
type TemplateContext struct {
	Scope interface{} // current scope object
	Root  interface{} // full parsed document root
	File  FileInfo
	Match interface{} // the actual value that triggered the match
}

// FileInfo carries source file metadata available in templates.
type FileInfo struct {
	Name      string
	Extension string
	Size      int
}

// Interpolate renders a Go template string using the provided context.
// Template syntax: {{.Scope.name}}, {{.File.Name}}, {{.Match}}, etc.
// Unknown fields render as empty string (missingkey=zero behaviour).
func Interpolate(tmpl string, ctx TemplateContext) string {
	if tmpl == "" || !strings.Contains(tmpl, "{{") {
		return tmpl
	}

	// Convert the scope object to a navigable map for template access
	data := map[string]interface{}{
		"Scope": toMap(ctx.Scope),
		"Root":  toMap(ctx.Root),
		"File": map[string]interface{}{
			"Name":      ctx.File.Name,
			"Extension": ctx.File.Extension,
		},
		"Match": fmt.Sprintf("%v", ctx.Match),
	}

	var t *template.Template
	if cached, ok := compiledTemplates.Load(tmpl); ok {
		t = cached.(*template.Template)
	} else {
		var err error
		t, err = template.New("").Option("missingkey=zero").Parse(tmpl)
		if err != nil {
			return fmt.Sprintf("[template parse error: %v]", err)
		}
		compiledTemplates.Store(tmpl, t)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return fmt.Sprintf("[template error: %v]", err)
	}
	// missingkey=zero yields nil for interface{} maps, which renders as "<no value>"; suppress it.
	return strings.ReplaceAll(buf.String(), "<no value>", "")
}

// toMap converts an interface{} to map[string]interface{} for template access.
// Passes through existing maps; wraps primitives under "value".
func toMap(v interface{}) interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return map[string]interface{}{"value": v}
}

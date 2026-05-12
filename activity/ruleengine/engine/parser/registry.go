package parser

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

// Document is the abstraction over a parsed file. The evaluator works
// exclusively through this interface — it never knows the underlying format.
type Document interface {
	// ResolveScope expands a path expression to an ordered slice of scope objects.
	// For JSON: JSONPath (e.g. "$.resources[*].data")
	// For XML:  XPath  (e.g. "//process")
	// For YAML/KV: dot-path (e.g. "spec.containers")
	// Empty path returns the root document wrapped in a single-element slice.
	ResolveScope(path string) ([]interface{}, error)

	// ResolvePath resolves a dot-notation or format-native path relative to
	// a scope object returned by ResolveScope. Returns the value and whether
	// the path was found.
	ResolvePath(obj interface{}, path string) (interface{}, bool)

	// Root returns the fully parsed document for template interpolation.
	Root() interface{}
}

// Parser converts raw string content into a navigable Document.
type Parser interface {
	Parse(content string) (Document, error)
}

// Registry maps parser names and file extensions to Parser implementations.
type Registry struct {
	byName map[string]Parser
	byExt  map[string]string // lowercase extension → parser name
}

// NewRegistry creates an empty registry. Callers register parsers before use.
func NewRegistry() *Registry {
	return &Registry{
		byName: make(map[string]Parser),
		byExt:  make(map[string]string),
	}
}

// Register associates a Parser with a name and one or more file extensions.
func (r *Registry) Register(name string, extensions []string, p Parser) {
	r.byName[name] = p
	for _, ext := range extensions {
		r.byExt[strings.ToLower(ext)] = name
	}
}

// Get returns the parser registered under the given name.
func (r *Registry) Get(name string) (Parser, bool) {
	p, ok := r.byName[name]
	return p, ok
}

// Detect infers the parser from a file name's extension.
// Returns the parser, its registered name, and whether one was found.
func (r *Registry) Detect(filename string) (Parser, string, bool) {
	ext := strings.ToLower(filepath.Ext(filename))
	name, ok := r.byExt[ext]
	if !ok {
		return nil, "", false
	}
	p, ok := r.byName[name]
	return p, name, ok
}

// MustGet returns the parser or panics — useful in engine init where all
// built-in parsers are always registered.
func (r *Registry) MustGet(name string) Parser {
	p, ok := r.byName[name]
	if !ok {
		panic(fmt.Sprintf("rule engine: no parser registered for %q", name))
	}
	return p
}

var (
	defaultRegistryOnce sync.Once
	defaultRegistry     *Registry
)

// DefaultRegistry returns the shared registry pre-loaded with all built-in parsers.
// Initialised once on first call; subsequent calls return the same instance.
func DefaultRegistry() *Registry {
	defaultRegistryOnce.Do(func() {
		r := NewRegistry()
		r.Register("json", []string{".json", ".flogo"}, &JSONParser{})
		r.Register("xml", []string{".xml", ".bwp"}, &XMLParser{})
		r.Register("yaml", []string{".yaml", ".yml"}, &YAMLParser{})
		r.Register("kv", []string{".conf", ".properties", ".env"}, &KVParser{})
		r.Register("lines", []string{".log", ".txt", ".out"}, &LinesParser{})
		defaultRegistry = r
	})
	return defaultRegistry
}

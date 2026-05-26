package parser

import (
	"testing"
)

// ─── fixtures ────────────────────────────────────────────────────────────────

const kubeDeploymentYAML = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: production
  labels:
    app: my-app
    tier: frontend
spec:
  replicas: 3
  template:
    spec:
      containers:
        - name: app
          image: nginx:latest
          resources:
            requests:
              memory: "64Mi"
              cpu: "250m"
        - name: sidecar
          image: busybox
`

const helmValuesYAML = `
replicaCount: 1
image:
  repository: nginx
  tag: ""
  pullPolicy: IfNotPresent
service:
  type: ClusterIP
  port: 80
ingress:
  enabled: false
resources: {}
`

const kvProperties = `
SERVER_URL=http://localhost:8080
# This is a comment
MAX_CONNECTIONS=100
EMPTY_VALUE=
SSL_ENABLED=true
QUEUE_NAME=orders.incoming
LOG_LEVEL=DEBUG # inline comment
`

const kvColonSeparated = `
host: localhost
port: 5672
; semicolon comment
vhost: /
user: admin
`

// ─── YAML parser ──────────────────────────────────────────────────────────────

func TestYAMLParser_ValidYAML(t *testing.T) {
	doc, err := (&YAMLParser{}).Parse(kubeDeploymentYAML)
	if err != nil || doc == nil {
		t.Fatalf("expected non-nil doc, got err=%v", err)
	}
}

func TestYAMLParser_InvalidYAML(t *testing.T) {
	_, err := (&YAMLParser{}).Parse("key: [unclosed bracket")
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestYAMLParser_EmptyDocument(t *testing.T) {
	doc, err := (&YAMLParser{}).Parse("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	scope, _ := doc.ResolveScope("")
	if len(scope) != 1 {
		t.Fatalf("expected 1 scope item (nil root), got %d", len(scope))
	}
}

func TestYAMLParser_Root_ReturnsMap(t *testing.T) {
	doc, _ := (&YAMLParser{}).Parse(kubeDeploymentYAML)
	root := doc.Root()
	m, ok := root.(map[string]interface{})
	if !ok {
		t.Fatal("expected root to be a map")
	}
	if m["kind"] != "Deployment" {
		t.Fatalf("expected kind=Deployment, got %v", m["kind"])
	}
}

// ─── YAML ResolveScope ────────────────────────────────────────────────────────

func TestYAMLParser_ResolveScope_Empty_ReturnsRoot(t *testing.T) {
	doc, _ := (&YAMLParser{}).Parse(kubeDeploymentYAML)
	scope, err := doc.ResolveScope("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 1 {
		t.Fatalf("expected 1 root item, got %d", len(scope))
	}
}

func TestYAMLParser_ResolveScope_DotPath(t *testing.T) {
	doc, _ := (&YAMLParser{}).Parse(kubeDeploymentYAML)
	scope, err := doc.ResolveScope("spec.template.spec.containers")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(scope))
	}
}

func TestYAMLParser_ResolveScope_ContainerNames(t *testing.T) {
	doc, _ := (&YAMLParser{}).Parse(kubeDeploymentYAML)
	scope, _ := doc.ResolveScope("spec.template.spec.containers")

	names := []string{}
	for _, item := range scope {
		m, ok := item.(map[string]interface{})
		if !ok {
			t.Fatal("expected map item")
		}
		names = append(names, m["name"].(string))
	}
	if names[0] != "app" || names[1] != "sidecar" {
		t.Fatalf("unexpected container names: %v", names)
	}
}

func TestYAMLParser_ResolveScope_WildcardSuffix_SameAsBarePath(t *testing.T) {
	// Rule authors familiar with JSONPath may write "spec.template.spec.containers[*]"
	// expecting array expansion. For YAML, the bare path already expands arrays;
	// the [*] suffix must be stripped so the result is identical to the bare path.
	doc, _ := (&YAMLParser{}).Parse(kubeDeploymentYAML)
	withoutWildcard, _ := doc.ResolveScope("spec.template.spec.containers")
	withWildcard, _ := doc.ResolveScope("spec.template.spec.containers[*]")

	if len(withWildcard) != len(withoutWildcard) {
		t.Fatalf("expected %d scope items with [*] suffix, got %d",
			len(withoutWildcard), len(withWildcard))
	}
	for i, item := range withWildcard {
		m, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("item %d: expected map, got %T", i, item)
		}
		expected := withoutWildcard[i].(map[string]interface{})
		if m["name"] != expected["name"] {
			t.Fatalf("item %d: expected name=%v, got %v", i, expected["name"], m["name"])
		}
	}
}

func TestYAMLParser_ResolveScope_PathNotFound_ReturnsEmpty(t *testing.T) {
	doc, _ := (&YAMLParser{}).Parse(kubeDeploymentYAML)
	scope, err := doc.ResolveScope("nonexistent.path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 0 {
		t.Fatalf("expected empty scope, got %d", len(scope))
	}
}

func TestYAMLParser_ResolveScope_ScalarPath(t *testing.T) {
	doc, _ := (&YAMLParser{}).Parse(kubeDeploymentYAML)
	scope, _ := doc.ResolveScope("metadata.name")
	if len(scope) != 1 {
		t.Fatalf("expected 1 scalar scope item, got %d", len(scope))
	}
	if scope[0] != "my-app" {
		t.Fatalf("expected 'my-app', got %v", scope[0])
	}
}

// ─── YAML ResolvePath ─────────────────────────────────────────────────────────

func TestYAMLParser_ResolvePath_DotNotation(t *testing.T) {
	doc, _ := (&YAMLParser{}).Parse(kubeDeploymentYAML)
	scope, _ := doc.ResolveScope("")
	root := scope[0]

	val, ok := doc.ResolvePath(root, "metadata.name")
	if !ok || val != "my-app" {
		t.Fatalf("expected 'my-app', got %v (ok=%v)", val, ok)
	}
}

func TestYAMLParser_ResolvePath_OnContainer(t *testing.T) {
	doc, _ := (&YAMLParser{}).Parse(kubeDeploymentYAML)
	scope, _ := doc.ResolveScope("spec.template.spec.containers")
	container := scope[0] // app container

	val, ok := doc.ResolvePath(container, "image")
	if !ok || val != "nginx:latest" {
		t.Fatalf("expected 'nginx:latest', got %v", val)
	}
}

func TestYAMLParser_ResolvePath_Missing(t *testing.T) {
	doc, _ := (&YAMLParser{}).Parse(kubeDeploymentYAML)
	scope, _ := doc.ResolveScope("")

	_, ok := doc.ResolvePath(scope[0], "spec.replicas.nonexistent")
	if ok {
		t.Fatal("expected not found for deeply missing path")
	}
}

func TestYAMLParser_ResolvePath_EmptyPath_ReturnsSelf(t *testing.T) {
	doc, _ := (&YAMLParser{}).Parse(kubeDeploymentYAML)
	scope, _ := doc.ResolveScope("")

	val, ok := doc.ResolvePath(scope[0], "")
	if !ok || val == nil {
		t.Fatal("empty path should return the object itself")
	}
}

func TestYAMLParser_ResolvePath_ArrayIndex(t *testing.T) {
	doc, _ := (&YAMLParser{}).Parse(kubeDeploymentYAML)
	scope, _ := doc.ResolveScope("")
	root := scope[0]

	// Access first container by index
	val, ok := doc.ResolvePath(root, "spec.template.spec.containers[0].name")
	if !ok || val != "app" {
		t.Fatalf("expected 'app' at index 0, got %v (ok=%v)", val, ok)
	}
}

func TestYAMLParser_ResolvePath_ArrayIndexOutOfBounds(t *testing.T) {
	doc, _ := (&YAMLParser{}).Parse(kubeDeploymentYAML)
	scope, _ := doc.ResolveScope("")

	_, ok := doc.ResolvePath(scope[0], "spec.template.spec.containers[99].name")
	if ok {
		t.Fatal("expected not found for out-of-bounds index")
	}
}

func TestYAMLParser_NumericField(t *testing.T) {
	doc, _ := (&YAMLParser{}).Parse(helmValuesYAML)
	scope, _ := doc.ResolveScope("")

	val, ok := doc.ResolvePath(scope[0], "replicaCount")
	if !ok || val == nil {
		t.Fatal("expected numeric replicaCount")
	}
}

// ─── KV parser ────────────────────────────────────────────────────────────────

func TestKVParser_EqualsFormat(t *testing.T) {
	doc, err := (&KVParser{}).Parse(kvProperties)
	if err != nil || doc == nil {
		t.Fatalf("expected non-nil doc, got err=%v", err)
	}
}

func TestKVParser_ResolvePath_BasicKey(t *testing.T) {
	doc, _ := (&KVParser{}).Parse(kvProperties)
	scope, _ := doc.ResolveScope("")

	val, ok := doc.ResolvePath(scope[0], "SERVER_URL")
	if !ok || val != "http://localhost:8080" {
		t.Fatalf("expected SERVER_URL value, got %v", val)
	}
}

func TestKVParser_SkipsComments(t *testing.T) {
	doc, _ := (&KVParser{}).Parse(kvProperties)
	scope, _ := doc.ResolveScope("")
	m := scope[0].(map[string]interface{})

	// Comment lines should not appear as keys
	for k := range m {
		if k == "# This is a comment" || k == "#" {
			t.Fatalf("comment line was parsed as a key: %q", k)
		}
	}
}

func TestKVParser_InlineComment_Stripped(t *testing.T) {
	doc, _ := (&KVParser{}).Parse(kvProperties)
	scope, _ := doc.ResolveScope("")

	val, ok := doc.ResolvePath(scope[0], "LOG_LEVEL")
	if !ok || val != "DEBUG" {
		t.Fatalf("expected 'DEBUG' (inline comment stripped), got %q", val)
	}
}

func TestKVParser_EmptyValue(t *testing.T) {
	doc, _ := (&KVParser{}).Parse(kvProperties)
	scope, _ := doc.ResolveScope("")

	val, ok := doc.ResolvePath(scope[0], "EMPTY_VALUE")
	if !ok {
		t.Fatal("expected EMPTY_VALUE to be found (even if empty)")
	}
	if val != "" {
		t.Fatalf("expected empty string, got %q", val)
	}
}

func TestKVParser_ColonSeparator(t *testing.T) {
	doc, _ := (&KVParser{}).Parse(kvColonSeparated)
	scope, _ := doc.ResolveScope("")

	val, ok := doc.ResolvePath(scope[0], "host")
	if !ok || val != "localhost" {
		t.Fatalf("expected 'localhost', got %v", val)
	}
}

func TestKVParser_SemicolonComment_Skipped(t *testing.T) {
	doc, _ := (&KVParser{}).Parse(kvColonSeparated)
	scope, _ := doc.ResolveScope("")
	m := scope[0].(map[string]interface{})

	for k := range m {
		if k == "; semicolon comment" || k == ";" {
			t.Fatalf("semicolon comment parsed as key: %q", k)
		}
	}
}

func TestKVParser_MissingKey(t *testing.T) {
	doc, _ := (&KVParser{}).Parse(kvProperties)
	scope, _ := doc.ResolveScope("")

	_, ok := doc.ResolvePath(scope[0], "NONEXISTENT_KEY")
	if ok {
		t.Fatal("expected not-found for missing key")
	}
}

func TestKVParser_ResolveScope_Empty_ReturnsAll(t *testing.T) {
	doc, _ := (&KVParser{}).Parse(kvProperties)
	scope, err := doc.ResolveScope("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 1 {
		t.Fatalf("expected 1 scope item (the whole map), got %d", len(scope))
	}
}

func TestKVParser_LineMissingDelimiter_Skipped(t *testing.T) {
	content := "VALID_KEY=valid_val\nNO_DELIMITER_LINE\nANOTHER=value"
	doc, _ := (&KVParser{}).Parse(content)
	scope, _ := doc.ResolveScope("")
	m := scope[0].(map[string]interface{})

	if _, ok := m["NO_DELIMITER_LINE"]; ok {
		t.Fatal("line without delimiter should be skipped")
	}
	if _, ok := m["VALID_KEY"]; !ok {
		t.Fatal("valid key should be parsed")
	}
}

// ─── normalizeYAML ────────────────────────────────────────────────────────────

func TestNormalizeYAML_Passthrough(t *testing.T) {
	input := map[string]interface{}{"key": "value"}
	result := normalizeYAML(input)
	m, ok := result.(map[string]interface{})
	if !ok || m["key"] != "value" {
		t.Fatal("expected passthrough of map[string]interface{}")
	}
}

func TestNormalizeYAML_InterfaceKeysConversion(t *testing.T) {
	input := map[interface{}]interface{}{
		"name": "test",
		"num":  42,
	}
	result := normalizeYAML(input)
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected conversion to map[string]interface{}")
	}
	if m["name"] != "test" {
		t.Fatalf("expected 'test', got %v", m["name"])
	}
}

func TestNormalizeYAML_NestedConversion(t *testing.T) {
	inner := map[interface{}]interface{}{"port": 5432}
	input := map[interface{}]interface{}{"db": inner}
	result := normalizeYAML(input)

	outer, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected outer to be map[string]interface{}")
	}
	db, ok := outer["db"].(map[string]interface{})
	if !ok {
		t.Fatal("expected inner to be normalized")
	}
	if db["port"] != 5432 {
		t.Fatalf("expected port=5432, got %v", db["port"])
	}
}

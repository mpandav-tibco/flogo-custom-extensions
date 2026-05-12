package parser

import (
	"strings"
	"testing"
)

// ─── fixtures ────────────────────────────────────────────────────────────────

const processesXML = `<?xml version="1.0" encoding="UTF-8"?>
<processes xmlns="http://xmlns.example.com/bpel">
  <process name="OrderProcess" enabled="true" timeout="30">
    <activities>
      <activity name="InvokePayment" type="REST"/>
      <activity name="SendEmail" type="SMTP"/>
    </activities>
  </process>
  <process name="AuditProcess" enabled="false" timeout="0">
    <activities>
      <activity name="LogAudit" type="LOG"/>
    </activities>
  </process>
</processes>`

const simpleXML = `<root><name>test</name><value>42</value></root>`

const xmlWithAttrs = `<config>
  <setting key="timeout" value="30"/>
  <setting key="retries" value="3"/>
  <setting key="debug" value="false"/>
</config>`

const malformedXML = `<root><unclosed>`

// ─── parse ────────────────────────────────────────────────────────────────────

func TestXMLParser_ValidXML(t *testing.T) {
	doc, err := (&XMLParser{}).Parse(processesXML)
	if err != nil || doc == nil {
		t.Fatalf("expected non-nil doc, got err=%v", err)
	}
}

func TestXMLParser_MalformedXML(t *testing.T) {
	_, err := (&XMLParser{}).Parse(malformedXML)
	if err == nil {
		t.Fatal("expected error for malformed XML")
	}
}

func TestXMLParser_EmptyString_Error(t *testing.T) {
	_, err := (&XMLParser{}).Parse("")
	if err == nil {
		t.Fatal("expected error for empty XML")
	}
}

// ─── ResolveScope ─────────────────────────────────────────────────────────────

func TestXMLParser_ResolveScope_EmptyPath_ReturnsRoot(t *testing.T) {
	doc, _ := (&XMLParser{}).Parse(processesXML)
	scope, err := doc.ResolveScope("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 1 {
		t.Fatalf("expected 1 root node, got %d", len(scope))
	}
}

func TestXMLParser_ResolveScope_XPath_SelectsNodes(t *testing.T) {
	doc, _ := (&XMLParser{}).Parse(processesXML)
	scope, err := doc.ResolveScope("//process")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 2 {
		t.Fatalf("expected 2 process nodes, got %d", len(scope))
	}
}

func TestXMLParser_ResolveScope_XPath_Activities(t *testing.T) {
	doc, _ := (&XMLParser{}).Parse(processesXML)
	scope, err := doc.ResolveScope("//activity")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 3 {
		t.Fatalf("expected 3 activities, got %d", len(scope))
	}
}

func TestXMLParser_ResolveScope_InvalidXPath_Error(t *testing.T) {
	doc, _ := (&XMLParser{}).Parse(simpleXML)
	_, err := doc.ResolveScope("///invalid[[[")
	if err == nil {
		t.Fatal("expected error for invalid XPath")
	}
}

func TestXMLParser_ResolveScope_NotFound_ReturnsEmpty(t *testing.T) {
	doc, _ := (&XMLParser{}).Parse(simpleXML)
	scope, err := doc.ResolveScope("//nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 0 {
		t.Fatalf("expected empty scope, got %d", len(scope))
	}
}

// ─── ResolvePath ──────────────────────────────────────────────────────────────

func TestXMLParser_ResolvePath_Attribute(t *testing.T) {
	doc, _ := (&XMLParser{}).Parse(processesXML)
	scope, _ := doc.ResolveScope("//process")
	firstProcess := scope[0]

	val, ok := doc.ResolvePath(firstProcess, "@name")
	if !ok {
		t.Fatal("expected to find @name attribute")
	}
	if val != "OrderProcess" {
		t.Fatalf("expected 'OrderProcess', got %v", val)
	}
}

func TestXMLParser_ResolvePath_EnabledAttribute(t *testing.T) {
	doc, _ := (&XMLParser{}).Parse(processesXML)
	scope, _ := doc.ResolveScope("//process")

	// First process: enabled=true
	val, ok := doc.ResolvePath(scope[0], "@enabled")
	if !ok || val != "true" {
		t.Fatalf("expected 'true', got %v (ok=%v)", val, ok)
	}

	// Second process: enabled=false
	val, ok = doc.ResolvePath(scope[1], "@enabled")
	if !ok || val != "false" {
		t.Fatalf("expected 'false', got %v (ok=%v)", val, ok)
	}
}

func TestXMLParser_ResolvePath_ZeroTimeout(t *testing.T) {
	doc, _ := (&XMLParser{}).Parse(processesXML)
	scope, _ := doc.ResolveScope("//process")

	val, ok := doc.ResolvePath(scope[1], "@timeout")
	if !ok || val != "0" {
		t.Fatalf("expected '0', got %v (ok=%v)", val, ok)
	}
}

func TestXMLParser_ResolvePath_ChildElement(t *testing.T) {
	// ResolvePath in XML context takes an XPath expression.
	// To find <name> inside <root> when scoped to the document node, use //name.
	doc, _ := (&XMLParser{}).Parse(simpleXML)
	scope, _ := doc.ResolveScope("")
	root := scope[0]

	val, ok := doc.ResolvePath(root, "//name")
	if !ok {
		t.Fatal("expected to find '//name' via XPath")
	}
	if val != "test" {
		t.Fatalf("expected 'test', got %v", val)
	}
}

func TestXMLParser_ResolvePath_ChildElement_FromParentScope(t *testing.T) {
	// When scoped to a specific element (e.g. <root>), relative XPath "name" works.
	doc, _ := (&XMLParser{}).Parse(simpleXML)
	scope, _ := doc.ResolveScope("//root")
	if len(scope) == 0 {
		t.Skip("no <root> element found via //root")
	}
	rootElem := scope[0]

	val, ok := doc.ResolvePath(rootElem, "name")
	if !ok {
		t.Fatal("expected relative XPath 'name' to find <name> child of <root>")
	}
	if val != "test" {
		t.Fatalf("expected 'test', got %v", val)
	}
}

func TestXMLParser_ResolvePath_EmptyPath_ReturnsInnerText(t *testing.T) {
	doc, _ := (&XMLParser{}).Parse(simpleXML)
	scope, _ := doc.ResolveScope("//name")

	val, ok := doc.ResolvePath(scope[0], "")
	if !ok {
		t.Fatal("expected to find inner text with empty path")
	}
	if val != "test" {
		t.Fatalf("expected 'test', got %v", val)
	}
}

func TestXMLParser_ResolvePath_MissingAttribute(t *testing.T) {
	doc, _ := (&XMLParser{}).Parse(processesXML)
	scope, _ := doc.ResolveScope("//process")

	_, ok := doc.ResolvePath(scope[0], "@nonexistent")
	if ok {
		t.Fatal("expected not-found for missing attribute")
	}
}

func TestXMLParser_ResolvePath_WrongType(t *testing.T) {
	doc, _ := (&XMLParser{}).Parse(simpleXML)
	// Pass a non-node object — should return not-found
	_, ok := doc.ResolvePath("not a node", "@attr")
	if ok {
		t.Fatal("expected not-found when obj is not an xmlquery.Node")
	}
}

func TestXMLParser_Root_ReturnsNode(t *testing.T) {
	doc, _ := (&XMLParser{}).Parse(simpleXML)
	root := doc.Root()
	if root == nil {
		t.Fatal("expected non-nil root")
	}
}

// ─── XPath filtering ──────────────────────────────────────────────────────────

func TestXMLParser_XPath_FilterByAttribute(t *testing.T) {
	doc, _ := (&XMLParser{}).Parse(processesXML)
	// Select only enabled processes
	scope, err := doc.ResolveScope("//process[@enabled='true']")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 1 {
		t.Fatalf("expected 1 enabled process, got %d", len(scope))
	}
	val, _ := doc.ResolvePath(scope[0], "@name")
	if val != "OrderProcess" {
		t.Fatalf("expected 'OrderProcess', got %v", val)
	}
}

func TestXMLParser_XPath_SettingAttributes(t *testing.T) {
	doc, _ := (&XMLParser{}).Parse(xmlWithAttrs)
	scope, err := doc.ResolveScope("//setting")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 3 {
		t.Fatalf("expected 3 settings, got %d", len(scope))
	}
}

func TestXMLParser_Parse_WithNamespace(t *testing.T) {
	xmlWithNS := `<ns:root xmlns:ns="http://example.com"><ns:item>value</ns:item></ns:root>`
	doc, err := (&XMLParser{}).Parse(xmlWithNS)
	if err != nil {
		t.Fatalf("unexpected error with namespace: %v", err)
	}
	if doc == nil {
		t.Fatal("expected non-nil doc")
	}
}

func TestXMLParser_LargeDocument(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("<items>")
	for i := 0; i < 50; i++ {
		sb.WriteString(`<item id="` + string(rune('a'+i%26)) + `">value</item>`)
	}
	sb.WriteString("</items>")

	doc, err := (&XMLParser{}).Parse(sb.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	scope, _ := doc.ResolveScope("//item")
	if len(scope) != 50 {
		t.Fatalf("expected 50 items, got %d", len(scope))
	}
}

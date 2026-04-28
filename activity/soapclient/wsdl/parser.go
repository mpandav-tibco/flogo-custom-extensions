package wsdl

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Public entry point
// ---------------------------------------------------------------------------

// maxWSDLFetchBytes caps the response body size for any HTTP WSDL or schema
// fetch. WSDLs in the wild are almost never larger than a few hundred KB;
// this prevents a malicious or misconfigured server from exhausting heap.
const maxWSDLFetchBytes = 10 << 20 // 10 MiB

// ParseOptions controls how the parser fetches and processes the WSDL.
type ParseOptions struct {
	// TLSConfig is used when fetching a WSDL or imported schemas over HTTPS.
	// When nil the system certificate pool is used (secure by default).
	// Set InsecureSkipVerify on the provided config only for development
	// environments where the WSDL server uses a self-signed certificate.
	TLSConfig *tls.Config
	// Timeout is the HTTP client timeout for fetching remote WSDLs.
	// Defaults to 30 seconds when zero.
	Timeout time.Duration
	// BaseURL is the base used to resolve relative <import> locations.
	// It is set automatically from the primary WSDL URL when not specified.
	BaseURL string
}

// Parse fetches and fully parses a WSDL 1.1 document. The source parameter
// can be:
//   - An HTTP/HTTPS URL:   "https://service.example.com/api?wsdl"
//   - A file path:         "/path/to/service.wsdl"
//   - A file:// URI:       "file:///path/to/service.wsdl"
//
// The returned WSDLInfo contains all resolved operations ready for the activity.
func Parse(source string, opts *ParseOptions) (*WSDLInfo, error) {
	if opts == nil {
		opts = &ParseOptions{}
	}
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}

	raw, err := fetchBytes(source, opts)
	if err != nil {
		return nil, fmt.Errorf("wsdl: fetch %q: %w", source, err)
	}

	// Derive base URL for resolving relative imports.
	if opts.BaseURL == "" {
		opts.BaseURL = baseOf(source)
	}

	defs := &Definitions{}
	if err := xmlDecode(raw, defs); err != nil {
		return nil, fmt.Errorf("wsdl: parse XML: %w", err)
	}

	// Resolve external <import> elements — non-fatal; any failures surface as Warnings.
	warnings := resolveImports(defs, opts)
	info := build(defs)
	info.Raw = nil // release the parse tree; all needed data is in info fields
	info.Warnings = warnings
	return info, nil
}

// ---------------------------------------------------------------------------
// Fetch helpers
// ---------------------------------------------------------------------------

func fetchBytes(source string, opts *ParseOptions) ([]byte, error) {
	source = strings.TrimSpace(source)

	// Flogo fileselector stores the value as a JSON object:
	// {"content":"data:application/octet-stream;base64,<b64>","filename":"foo.wsdl"}
	if strings.HasPrefix(source, "{") {
		var sel struct {
			Content  string `json:"content"`
			Filename string `json:"filename"`
		}
		if err := json.Unmarshal([]byte(source), &sel); err == nil && sel.Content != "" {
			// Strip the data URI prefix (data:<mime>;base64,)
			b64 := sel.Content
			if idx := strings.Index(b64, ";base64,"); idx >= 0 {
				b64 = b64[idx+8:]
			}
			decoded, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				return nil, fmt.Errorf("wsdl: decode fileselector base64 (%s): %w", sel.Filename, err)
			}
			// Apply the same size cap used for HTTP and file-path sources for
			// consistency — the string is already in memory but the check
			// prevents passing a multi-hundred-MiB blob into the XML parser.
			if int64(len(decoded)) > maxWSDLFetchBytes {
				return nil, fmt.Errorf("wsdl: fileselector WSDL %q exceeds %d MiB size limit", sel.Filename, maxWSDLFetchBytes>>20)
			}
			return decoded, nil
		}
	}

	if strings.HasPrefix(source, "file://") {
		return readWSDLFile(source[7:])
	}

	// Plain filesystem path (no scheme).
	if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
		return readWSDLFile(source)
	}

	// HTTP / HTTPS
	// Use the caller-supplied TLS config, or nil to use the system trust store.
	// A nil TLSClientConfig in http.Transport means the default secure config.
	client := &http.Client{
		Timeout: opts.Timeout,
		Transport: &http.Transport{
			TLSClientConfig: opts.TLSConfig, // nil → system CA pool (secure default)
		},
		// WSDL endpoints should not redirect. Refusing redirects surfaces
		// misconfigurations early rather than silently following a chain.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("wsdl: unexpected redirect to %s — check the WSDL URL", req.URL)
		},
	}

	resp, err := client.Get(source) // #nosec G107 — URL is user-supplied config value
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, source)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxWSDLFetchBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response from %s: %w", source, err)
	}
	if int64(len(body)) > maxWSDLFetchBytes {
		return nil, fmt.Errorf("WSDL response from %s exceeds %d MiB size limit", source, maxWSDLFetchBytes>>20)
	}
	return body, nil
}

// readWSDLFile reads a local WSDL or schema file with the same size cap applied
// to HTTP fetches, preventing a misconfigured path from exhausting heap.
func readWSDLFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxWSDLFetchBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxWSDLFetchBytes {
		return nil, fmt.Errorf("WSDL file %q exceeds %d MiB size limit", path, maxWSDLFetchBytes>>20)
	}
	return data, nil
}

// baseOf returns the "directory" portion of a URL or path so relative imports
// can be resolved (e.g. "https://host/svc/" from "https://host/svc/svc.wsdl").
func baseOf(source string) string {
	idx := strings.LastIndex(source, "/")
	if idx < 0 {
		return ""
	}
	return source[:idx+1]
}

// ---------------------------------------------------------------------------
// Import resolution
// ---------------------------------------------------------------------------

// resolveImports fetches any imported XSD schemas and merges their elements
// and types into the primary definitions.Types.Schemas slice.
// It returns a (possibly empty) slice of warning strings for any imports that
// could not be fetched — these are non-fatal and the caller should log them.
func resolveImports(defs *Definitions, opts *ParseOptions) []string {
	var warnings []string
	// First pass: top-level WSDL <import> (other WSDLs — rare but valid).
	// visited guards against duplicate <import location="..."> entries.
	visitedWSDL := map[string]bool{}
	for _, imp := range defs.Imports {
		if imp.Location == "" {
			continue
		}
		loc := absoluteURL(imp.Location, opts.BaseURL)
		if visitedWSDL[loc] {
			continue // skip duplicate import declarations
		}
		visitedWSDL[loc] = true
		raw, err := fetchBytes(loc, opts)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("import %q: %s", loc, err))
			continue // best-effort
		}
		sub := &Definitions{}
		if xmlDecode(raw, sub) == nil {
			// Merge direct contents of the imported WSDL.
			// Split-file WSDLs (common in enterprise environments) place abstract
			// definitions in one file and concrete bindings/service addresses in
			// another; discarding Bindings/Services causes zero operations to be
			// discovered and a startup failure.
			defs.Messages = append(defs.Messages, sub.Messages...)
			defs.PortTypes = append(defs.PortTypes, sub.PortTypes...)
			defs.Bindings = append(defs.Bindings, sub.Bindings...)
			defs.Services = append(defs.Services, sub.Services...)
			defs.Types.Schemas = append(defs.Types.Schemas, sub.Types.Schemas...)
			// Recursively resolve imports declared inside the sub-WSDL using
			// the sub-WSDL's own base URL so relative paths resolve correctly.
			if len(sub.Imports) > 0 || len(sub.Types.Schemas) > 0 {
				// Record current sub-slice lengths BEFORE the recursive call so
				// we can append only the newly-discovered items. Without this
				// guard, resolveImports mutates sub (appending into it) and the
				// subsequent merge would re-append items already merged above.
				preMsg := len(sub.Messages)
				prePT := len(sub.PortTypes)
				preBind := len(sub.Bindings)
				preSvc := len(sub.Services)
				preSchema := len(sub.Types.Schemas)
				subOpts := *opts
				subOpts.BaseURL = baseOf(loc)
				subWarnings := resolveImports(sub, &subOpts)
				warnings = append(warnings, subWarnings...)
				// Merge ONLY items appended by the recursive resolution.
				defs.Messages = append(defs.Messages, sub.Messages[preMsg:]...)
				defs.PortTypes = append(defs.PortTypes, sub.PortTypes[prePT:]...)
				defs.Bindings = append(defs.Bindings, sub.Bindings[preBind:]...)
				defs.Services = append(defs.Services, sub.Services[preSvc:]...)
				defs.Types.Schemas = append(defs.Types.Schemas, sub.Types.Schemas[preSchema:]...)
			}
		}
	}

	// Second pass: <xs:import schemaLocation="..."> within embedded schemas.
	// Use an index-based loop (not range) so that schemas appended during
	// iteration are themselves visited — handles multi-level import chains
	// common in enterprise WSDLs with shared type libraries.
	// Each schema resolves its own imports relative to the URL it was fetched
	// from (Schema.SourceURL), falling back to opts.BaseURL for inline schemas
	// that were embedded directly in the WSDL <types> block.
	visited := map[string]bool{}
	for i := 0; i < len(defs.Types.Schemas); i++ {
		schema := &defs.Types.Schemas[i]
		schemaBase := opts.BaseURL
		if schema.SourceURL != "" {
			schemaBase = baseOf(schema.SourceURL)
		}
		for _, si := range schema.Imports {
			if si.SchemaLocation == "" {
				continue
			}
			loc := absoluteURL(si.SchemaLocation, schemaBase)
			if visited[loc] {
				continue // break import cycles
			}
			visited[loc] = true
			raw, err := fetchBytes(loc, opts)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("schema import %q: %s", loc, err))
				continue
			}
			extra := Schema{}
			if xmlDecode(raw, &extra) == nil {
				extra.SourceURL = loc // record fetch URL for transitive import resolution
				defs.Types.Schemas = append(defs.Types.Schemas, extra)
			}
		}
	}
	return warnings
}

func absoluteURL(ref, base string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") ||
		strings.HasPrefix(ref, "file://") {
		return ref
	}
	// Use the standard library to correctly resolve all relative forms:
	//   "./types.xsd"        → base directory + types.xsd
	//   "../common/types.xsd"→ one level up from base
	//   "/schemas/shared.xsd"→ server-root-relative (was broken before)
	baseURL, err := url.Parse(base)
	if err != nil {
		return base + ref // fallback: best-effort concatenation
	}
	refURL, err := url.Parse(ref)
	if err != nil {
		return base + ref // fallback: best-effort concatenation
	}
	return baseURL.ResolveReference(refURL).String()
}

// ---------------------------------------------------------------------------
// XML decode helper with charset support
// ---------------------------------------------------------------------------

// xmlDecode is a drop-in replacement for xml.Unmarshal that sets a
// CharsetReader so non-UTF-8 encoding declarations (e.g. ISO-8859-1 used by
// some older SOAP/WSDL generators) don't cause a parse error.
//
// The passthrough is safe for the data the activity consumes (element names,
// namespace URIs, soapAction values) because WSDL/XSD identifiers are
// restricted to XML NCNames which are ASCII. Non-ASCII characters may appear
// in xs:documentation or service description text but are not used by the
// operation builder, so charset mismatches in those nodes have no effect on
// skeleton or schema generation.
func xmlDecode(data []byte, v interface{}) error {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) {
		return r, nil // pass through; WSDL/XSD content is always ASCII
	}
	return d.Decode(v)
}

// ---------------------------------------------------------------------------
// Builder — produce WSDLInfo from raw Definitions
// ---------------------------------------------------------------------------

func build(defs *Definitions) *WSDLInfo {
	info := &WSDLInfo{
		TargetNamespace: defs.TargetNamespace,
		// Raw is set temporarily so callers can access the parse tree for
		// introspection. Parse() nils it before returning to allow the GC
		// to reclaim the tree once all derived values have been extracted.
		Raw: defs,
	}

	// --- Index helpers ---
	msgByName := indexMessages(defs)
	ptByName := indexPortTypes(defs)
	schemaElements, schemaTypes := indexSchemas(defs)

	// --- Service endpoint (first soap:address found) ---
	for _, svc := range defs.Services {
		for _, port := range svc.Ports {
			if port.SOAPAddress.Location != "" {
				info.ServiceEndpoint = port.SOAPAddress.Location
				break
			}
			if port.SOAP12Addr.Location != "" {
				info.ServiceEndpoint = port.SOAP12Addr.Location
				break
			}
		}
		if info.ServiceEndpoint != "" {
			break
		}
	}

	// --- Build operations from each binding ---
	// Key = "name|version" so that a WSDL with both a SOAP 1.1 and a SOAP 1.2
	// binding for the same portType produces two selectable operations.
	seen := map[string]bool{}
	for _, binding := range defs.Bindings {
		// Detect SOAP version using Transport and Style from both namespaces.
		// Transport is always set for HTTP bindings and is the most reliable
		// indicator — Style may be omitted when defaulting to "document", so
		// checking Style alone causes false SOAP 1.1 classification.
		soapVer := "1.1"
		bindingStyle := binding.SOAPBinding.Style
		if binding.SOAP12Binding.Transport != "" || binding.SOAP12Binding.Style != "" {
			soapVer = "1.2"
			if bindingStyle == "" {
				bindingStyle = binding.SOAP12Binding.Style
			}
		}
		if bindingStyle == "" {
			bindingStyle = "document" // safe default per WSDL 1.1 §2.5
		}

		// Resolve portType to get operation input/output message refs.
		ptName := localName(binding.Type)
		pt, hasPT := ptByName[ptName]
		ptOps := map[string]PTOperation{}
		if hasPT {
			for _, op := range pt.Operations {
				ptOps[op.Name] = op
			}
		}

		for _, bop := range binding.Operations {
			// Deduplicate using name+version key so dual SOAP 1.1/1.2 bindings
			// for the same portType are both retained as distinct operations.
			dedupeKey := bop.Name + "|" + soapVer
			if seen[dedupeKey] {
				continue
			}
			seen[dedupeKey] = true

			op := Operation{
				Name:        bop.Name,
				Style:       bindingStyle,
				SOAPVersion: soapVer,
			}

			// soapAction: prefer SOAP 1.1 then SOAP 1.2 binding.
			op.SOAPAction = bop.SOAPOperation.SOAPAction
			if op.SOAPAction == "" {
				op.SOAPAction = bop.SOAP12Op.SOAPAction
			}
			// Per WS-I BP 1.1: empty soapAction is valid — leave as "".

			// Operation-level style override.
			if bop.SOAPOperation.Style != "" {
				op.Style = bop.SOAPOperation.Style
			}

			// Body use: check SOAP 1.1 namespace first, then SOAP 1.2.
			// SOAP 1.2 deprecated "encoded", so "literal" is always the correct
			// default, but we still read what the WSDL actually says.
			op.BodyUse = bop.Input.SOAPBody.Use
			if op.BodyUse == "" {
				op.BodyUse = bop.Input.SOAP12Body.Use
			}
			if op.BodyUse == "" {
				op.BodyUse = "literal"
			}

			// Resolve input message.
			if ptOp, ok := ptOps[bop.Name]; ok {
				inputMsgName := localName(ptOp.Input.Message)
				if msg, ok := msgByName[inputMsgName]; ok {
					op.InputMsg = resolveMessage(msg, schemaElements, schemaTypes)
				}
				outputMsgName := localName(ptOp.Output.Message)
				if msg, ok := msgByName[outputMsgName]; ok {
					op.OutputMsg = resolveMessage(msg, schemaElements, schemaTypes)
				}
			}

			info.Operations = append(info.Operations, op)
		}
	}

	return info
}

// ---------------------------------------------------------------------------
// Index helpers
// ---------------------------------------------------------------------------

func indexMessages(defs *Definitions) map[string]*Message {
	m := make(map[string]*Message, len(defs.Messages))
	for i := range defs.Messages {
		m[defs.Messages[i].Name] = &defs.Messages[i]
	}
	return m
}

func indexPortTypes(defs *Definitions) map[string]*PortType {
	m := make(map[string]*PortType, len(defs.PortTypes))
	for i := range defs.PortTypes {
		m[defs.PortTypes[i].Name] = &defs.PortTypes[i]
	}
	return m
}

// indexSchemas builds two maps from all embedded schemas:
//   - elements: "{targetNamespace}localName" → SchemaElement
//   - types:    "{targetNamespace}localName" → ComplexType
//
// Using the namespace-qualified key prevents silent collisions when multiple
// imported schemas define elements with the same local name in different namespaces.
func indexSchemas(defs *Definitions) (map[string]*SchemaElement, map[string]*ComplexType) {
	elements := make(map[string]*SchemaElement)
	types := make(map[string]*ComplexType)
	for si := range defs.Types.Schemas {
		schema := &defs.Types.Schemas[si]
		ns := schema.TargetNamespace
		for ei := range schema.Elements {
			name := schema.Elements[ei].Name
			// Index by both namespace-qualified key and bare local name so that
			// message parts that use a bare element reference still resolve.
			if ns != "" {
				elements["{"+ns+"}"+name] = &schema.Elements[ei]
			}
			// Bare name only inserted when no prior entry exists, preventing the
			// last-schema-wins collision while still supporting bare references.
			if _, exists := elements[name]; !exists {
				elements[name] = &schema.Elements[ei]
			}
		}
		for ci := range schema.ComplexTypes {
			name := schema.ComplexTypes[ci].Name
			if ns != "" {
				types["{"+ns+"}"+name] = &schema.ComplexTypes[ci]
			}
			if _, exists := types[name]; !exists {
				types[name] = &schema.ComplexTypes[ci]
			}
		}
	}
	return elements, types
}

// ---------------------------------------------------------------------------
// Message resolution
// ---------------------------------------------------------------------------

func resolveMessage(
	msg *Message,
	elements map[string]*SchemaElement,
	types map[string]*ComplexType,
) *ResolvedMessage {
	rm := &ResolvedMessage{Name: msg.Name}
	for _, part := range msg.Parts {
		rp := ResolvedPart{Name: part.Name}

		switch {
		case part.Element != "":
			// Document-style: part refers to a schema element.
			elemName := localName(part.Element)
			rp.ElementName = elemName
			if el, ok := elements[elemName]; ok {
				// Inline complexType on the element, or referenced named type.
				ct := resolveComplexType(el, elements, types)
				rp.Fields = fieldsFromComplexType(ct, elements, types, 0)
			}

		case part.Type != "":
			// RPC-style: part is a named type.
			typeName := localName(part.Type)
			if ct, ok := types[typeName]; ok {
				rp.Fields = fieldsFromComplexType(ct, elements, types, 0)
			}
		}

		rm.Parts = append(rm.Parts, rp)
	}
	return rm
}

// resolveComplexType returns the ComplexType for a SchemaElement, following a
// named type reference if the element's inline ComplexType is empty.
// It does NOT follow xs:extension here — that is handled inside
// fieldsFromComplexType so that base-type fields are prepended correctly.
func resolveComplexType(
	el *SchemaElement,
	elements map[string]*SchemaElement,
	types map[string]*ComplexType,
) *ComplexType {
	// Prefer inline complexType (including one that uses xs:extension).
	if el.ComplexType.Sequence != nil || el.ComplexType.All != nil ||
		el.ComplexType.Choice != nil || el.ComplexType.ComplexContent != nil {
		return &el.ComplexType
	}
	// Fall back to named type reference.
	if el.Type != "" {
		typeName := localName(el.Type)
		if ct, ok := types[typeName]; ok {
			return ct
		}
	}
	return &el.ComplexType // empty but safe
}

// maxDepth prevents infinite recursion on self-referential schemas.
const maxDepth = 8

// fieldsFromComplexType returns the FieldDef slice for a ComplexType,
// following xs:extension base references to prepend inherited fields.
// It is a thin wrapper that seeds the visited set used to guard against
// diamond inheritance producing duplicate fields.
func fieldsFromComplexType(
	ct *ComplexType,
	elements map[string]*SchemaElement,
	types map[string]*ComplexType,
	depth int,
) []FieldDef {
	return fieldsFromComplexTypeVisited(ct, elements, types, depth, map[string]bool{})
}

// fieldsFromComplexTypeVisited is the recursive inner implementation.
// visited tracks type names already expanded during this traversal to prevent
// diamond-inheritance patterns from emitting duplicate fields.
func fieldsFromComplexTypeVisited(
	ct *ComplexType,
	elements map[string]*SchemaElement,
	types map[string]*ComplexType,
	depth int,
	visited map[string]bool,
) []FieldDef {
	if ct == nil || depth > maxDepth {
		return nil
	}

	// Handle xs:extension: prepend fields from the base type, then append the
	// extension's own fields. This is the standard WSDL pattern for type
	// inheritance generated by JAX-WS, CXF, and WCF.
	if ct.ComplexContent != nil && ct.ComplexContent.Extension != nil {
		ext := ct.ComplexContent.Extension
		var fields []FieldDef
		// Resolve and prepend base type fields, guarding against cycles/diamonds.
		if ext.Base != "" {
			baseName := localName(ext.Base)
			if !visited[baseName] {
				visited[baseName] = true
				if baseCT, ok := types[baseName]; ok {
					fields = append(fields, fieldsFromComplexTypeVisited(baseCT, elements, types, depth+1, visited)...)
				}
			}
		}
		// Append fields defined directly in the extension.
		extCT := &ComplexType{Sequence: ext.Sequence, All: ext.All, Choice: ext.Choice}
		fields = append(fields, fieldsFromComplexTypeVisited(extCT, elements, types, depth+1, visited)...)
		return fields
	}

	var children []SchemaElement
	switch {
	case ct.Sequence != nil:
		children = ct.Sequence.Elements
	case ct.All != nil:
		children = ct.All.Elements
	case ct.Choice != nil:
		children = ct.Choice.Elements
	}

	fields := make([]FieldDef, 0, len(children))
	for _, child := range children {
		fd := FieldDef{
			Name:      child.Name,
			XSDType:   localName(child.Type),
			MinOccurs: child.MinOccurs,
			MaxOccurs: child.MaxOccurs,
		}
		if fd.MinOccurs == "" {
			fd.MinOccurs = "1"
		}
		if fd.MaxOccurs == "" {
			fd.MaxOccurs = "1"
		}

		// Recurse into nested complexType.
		childCT := resolveComplexType(&child, elements, types)
		if childCT.Sequence != nil || childCT.All != nil || childCT.Choice != nil ||
			(childCT.ComplexContent != nil && childCT.ComplexContent.Extension != nil) {
			fd.IsComplex = true
			fd.Children = fieldsFromComplexTypeVisited(childCT, elements, types, depth+1, visited)
		}

		fields = append(fields, fd)
	}
	return fields
}

// ---------------------------------------------------------------------------
// Utility
// ---------------------------------------------------------------------------

// localName strips a namespace prefix (e.g. "tns:GetWeather" → "GetWeather").
func localName(qname string) string {
	if idx := strings.LastIndex(qname, ":"); idx >= 0 {
		return qname[idx+1:]
	}
	return qname
}

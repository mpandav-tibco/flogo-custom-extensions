// Package soapclient provides a Flogo activity for invoking SOAP 1.1/1.2
// services with optional WSDL-driven operation discovery, auto soapAction
// injection, and request-body skeleton generation.
//
// Enhancements over the upstream project-flogo/contrib soapclient:
//   - WSDLUrl: load a WSDL from a URL or local file at startup
//   - WSDLOperation: select the operation; soapAction and body skeleton are
//     derived automatically
//   - AutoUseWSDLEndpoint: override the endpoint from the WSDL <service>
package soapclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	mxj "github.com/clbanning/mxj/v2"
	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/coerce"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/connection"
	"github.com/project-flogo/core/support/trace"

	"github.com/mpandav-tibco/flogo-custom-extensions/activity/soapclient/wsdl"
)

func init() {
	_ = activity.Register(&Activity{}, New)
}

var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})

// transportCache stores shared *http.Transport instances keyed on TLS settings.
// Sharing a transport across Activity instances that use identical TLS config
// means they share a single connection pool, avoiding fd exhaustion when many
// flow instances are created for the same endpoint.
//
// Known limitation: entries are never evicted. In a typical Flogo deployment
// the number of distinct TLS configurations is small and fixed, so this is
// acceptable. If flows are dynamically created with many different cert
// payloads (e.g. per-tenant client certs), consider replacing the sync.Map
// with an LRU-bounded cache.
var transportCache sync.Map

// tlsCacheKey derives a string key from the TLS-relevant Settings fields.
// MaxConnsPerHost is included so that different concurrency limits produce
// separate transports rather than silently sharing the first-created one.
func tlsCacheKey(s *Settings) string {
	return fmt.Sprintf("%v\x00%s\x00%s\x00%s\x00%d",
		s.SkipTLSVerify, s.ServerCertificate, s.ClientCertificate, s.ClientKey, s.MaxConnsPerHost)
}

// ---------------------------------------------------------------------------
// SOAP envelope XML templates
// ---------------------------------------------------------------------------

const soapEnv11Start = `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">`
const soapEnv12Start = `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope">`
const soapEnvEnd = `</soap:Envelope>`

// ---------------------------------------------------------------------------
// New — called once at flow initialisation
// ---------------------------------------------------------------------------

// New creates and initialises a SOAP Client activity instance.
func New(ctx activity.InitContext) (activity.Activity, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(ctx.Settings(), s, true); err != nil {
		return nil, err
	}

	switch s.SoapVersion {
	case "", "1.1", "1.2": // valid; empty string is treated as 1.1
	default:
		return nil, fmt.Errorf("SOAP Client: unsupported soapVersion %q — use \"1.1\" or \"1.2\"", s.SoapVersion)
	}

	if !s.XMLMode && s.XMLAttributePrefix == "" {
		s.XMLAttributePrefix = "-"
	}

	tlsCfg, err := buildTLSConfig(s)
	if err != nil {
		return nil, err
	}
	if s.SkipTLSVerify && s.EnableTLS && s.ServerCertificate != "" {
		ctx.Logger().Warnf("SOAP Client: skipTlsVerify=true is ignored because a serverCertificate is configured — the pinned certificate takes precedence")
	}

	timeout := time.Duration(s.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx.Logger().Debugf("SOAP Client: init — endpoint=%q soapVersion=%s xmlMode=%v timeout=%s tlsEnabled=%v",
		s.SoapServiceEndpoint, s.SoapVersion, s.XMLMode, timeout, s.EnableTLS)

	// Retrieve or create a shared http.Transport for this TLS configuration.
	// Transport instances are cached by TLS fingerprint so that multiple Activity
	// instances pointing at the same endpoint share one connection pool.
	maxConns := s.MaxConnsPerHost
	if maxConns <= 0 {
		maxConns = 100 // sensible default for most deployments
	}
	transportKey := tlsCacheKey(s)
	var transport *http.Transport
	if cached, ok := transportCache.Load(transportKey); ok {
		transport = cached.(*http.Transport)
	} else {
		newTransport := &http.Transport{
			TLSClientConfig:     tlsCfg,
			MaxIdleConnsPerHost: 10,       // default of 2 is too low for concurrent SOAP calls
			MaxConnsPerHost:     maxConns, // configurable via maxConnsPerHost setting
		}
		// Use LoadOrStore to resolve races when multiple goroutines call New()
		// concurrently for the same TLS config.
		if actual, loaded := transportCache.LoadOrStore(transportKey, newTransport); loaded {
			transport = actual.(*http.Transport)
		} else {
			transport = newTransport
		}
	}
	httpClient := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		// SOAP endpoints must not redirect. Refusing redirects prevents
		// Authorization headers from being silently forwarded to an unexpected host.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("SOAP Client: unexpected redirect to %s — SOAP endpoints should not redirect; check soapServiceEndpoint configuration", req.URL)
		},
	}

	act := &Activity{
		settings:   s,
		httpClient: httpClient,
		endpoint:   s.SoapServiceEndpoint,
	}

	// Load the auth connection separately: metadata.MapToStruct cannot safely
	// coerce nil (unset connection) to connection.Manager — coerce.ToConnection
	// has no nil case and returns an error. We guard the nil case here.
	if connVal := ctx.Settings()["authorizationConn"]; connVal != nil {
		var cerr error
		act.authConn, cerr = coerce.ToConnection(connVal)
		if cerr != nil {
			return nil, fmt.Errorf("SOAP Client: authorizationConn: %w", cerr)
		}
	}

	// --- WSDL loading (optional) ---
	// Determine the WSDL source based on the dropdown selection.
	var wsdlSource string
	switch s.WSDLSourceType {
	case "WSDL URL":
		wsdlSource = strings.TrimSpace(s.WSDLHttpUrl)
	case "WSDL File":
		wsdlSource = s.WSDLUrl // fileselector JSON blob or plain path
	default:
		// "None" or unset — skip WSDL integration
	}
	if wsdlSource != "" {
		ctx.Logger().Infof("SOAP Client: loading WSDL from %s", wsdlLabel(wsdlSource))
		wsdlOpts := &wsdl.ParseOptions{
			TLSConfig: tlsCfg,
			Timeout:   timeout,
		}
		info, wErr := loadWSDLWithRetry(ctx, wsdlSource, wsdlOpts)
		if wErr != nil {
			return nil, fmt.Errorf("SOAP Client: WSDL load failed: %w", wErr)
		}

		for _, w := range info.Warnings {
			ctx.Logger().Warnf("SOAP Client: WSDL warning: %s", w)
		}
		ctx.Logger().Infof("SOAP Client: WSDL loaded — %d operations available", len(info.Operations))

		if s.AutoUseWSDLEndpoint && info.ServiceEndpoint != "" {
			ctx.Logger().Infof("SOAP Client: using WSDL endpoint: %s", info.ServiceEndpoint)
			act.endpoint = info.ServiceEndpoint
		}

		act.wsdlTargetNS = info.TargetNamespace
		if s.WSDLOperation != "" {
			op := wsdl.FindOperation(info, s.WSDLOperation, s.SoapVersion)
			if op == nil {
				return nil, fmt.Errorf("SOAP Client: operation %q not found in WSDL (available: %s)",
					s.WSDLOperation, operationNames(info))
			}
			act.wsdlOp = op
			ctx.Logger().Debugf("SOAP Client: selected operation=%q soapAction=%q style=%s version=%s ns=%q",
				op.Name, op.SOAPAction, op.Style, op.SOAPVersion, info.TargetNamespace)
		}
	}

	// Validate that an endpoint is available. Without this, the first Eval()
	// call would fail with Go's opaque "parse \"\": empty url" error.
	if act.endpoint == "" {
		return nil, fmt.Errorf("SOAP Client: no service endpoint configured — " +
			"set soapServiceEndpoint or enable autoUseWsdlEndpoint with a WSDL that declares a <soap:address>")
	}

	ctx.Logger().Infof("SOAP Client: initialised — endpoint=%s soapVersion=%s xmlMode=%v",
		act.endpoint, s.SoapVersion, s.XMLMode)
	return act, nil
}

// ---------------------------------------------------------------------------
// Activity struct
// ---------------------------------------------------------------------------

// Activity is the SOAP Client activity instance.
type Activity struct {
	settings     *Settings
	httpClient   *http.Client
	endpoint     string             // effective endpoint (may be overridden from WSDL)
	wsdlOp       *wsdl.Operation    // nil when no operation selected
	wsdlTargetNS string             // WSDL targetNamespace — used to wrap body elements correctly
	authConn     connection.Manager // nil when authentication is not configured
}

func (a *Activity) Metadata() *activity.Metadata { return activityMd }

// ---------------------------------------------------------------------------
// Eval — called on every flow execution
// ---------------------------------------------------------------------------

func (a *Activity) Eval(ctx activity.Context) (done bool, err error) {
	log := ctx.Logger()
	input := &Input{}
	if err = ctx.GetInputObject(input); err != nil {
		return false, err
	}

	log.Debugf("SOAP Client: Eval start — soapVersion=%s xmlMode=%v endpoint=%s",
		a.settings.SoapVersion, a.settings.XMLMode, a.endpoint)

	output := &Output{}

	// Resolve effective SOAPAction.
	soapAction := input.SoapAction
	if soapAction == "" && a.wsdlOp != nil {
		soapAction = a.wsdlOp.SOAPAction
		log.Debugf("SOAP Client: auto-injected soapAction=%q from WSDL operation %q", soapAction, a.wsdlOp.Name)
	}

	// Build the target URL with properly encoded, deterministically ordered query params.
	// Parse the endpoint to preserve any existing query parameters from the WSDL <soap:address>.
	parsedURI, err := url.Parse(a.endpoint)
	if err != nil {
		return false, fmt.Errorf("SOAP Client: invalid endpoint URL: %w", err)
	}
	if len(input.HttpQueryParams) > 0 {
		existing := parsedURI.Query()
		for k, v := range input.HttpQueryParams {
			existing.Set(k, v)
		}
		parsedURI.RawQuery = existing.Encode()
	}
	uri := parsedURI.String()
	log.Infof("SOAP Client: invoking endpoint=%s soapVersion=%s", uri, a.settings.SoapVersion)

	// Build the SOAP envelope.
	envXML, err := a.buildEnvelope(ctx, input)
	if err != nil {
		return false, err
	}
	log.Debugf("SOAP Client: envelope built — size=%d bytes", len(envXML))

	// OTel tracing: start a child span for this outbound SOAP call.
	// Uses the Flogo trace API (project-flogo/core/support/trace) which delegates
	// to whatever tracer is registered at runtime (OTel, Jaeger, no-op, etc.).
	// The span is finished unconditionally in a deferred call so it is always
	// exported even when an error is returned.
	var spanCtx trace.TracingContext
	if trace.Enabled() {
		tracer := trace.GetTracer()
		spanCtx, err = tracer.StartTrace(trace.Config{
			Operation: "SOAPClient." + func() string {
				if a.wsdlOp != nil {
					return a.wsdlOp.Name
				}
				return "invoke"
			}(),
			Tags: map[string]interface{}{
				"soap.version":  a.settings.SoapVersion,
				"soap.endpoint": uri,
				"soap.action":   soapAction,
			},
			Logger: log,
		}, ctx.GetTracingContext())
		if err != nil {
			log.Warnf("SOAP Client: failed to start trace span: %s", err)
			spanCtx = nil
			err = nil // tracing failure must not abort the SOAP call
		}
		defer func() {
			if spanCtx != nil {
				_ = tracer.FinishTrace(spanCtx, err)
			}
		}()
	}

	// Use the flow's context when available so that flow-level cancellation
	// or timeout propagates to the in-flight HTTP call.
	reqCtx := context.Background()
	if fctx, ok := ctx.(interface{ Context() context.Context }); ok {
		reqCtx = fctx.Context()
	}

	// Send the HTTP request.
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, uri, bytes.NewReader(envXML))
	if err != nil {
		return false, fmt.Errorf("SOAP Client: build request: %w", err)
	}

	// SOAP 1.1: action goes in the SOAPAction HTTP header (RFC 2616).
	// SOAP 1.2: SOAPAction is removed; action is embedded in the Content-Type parameter (W3C).
	if a.settings.SoapVersion == "1.2" {
		ct := "application/soap+xml; charset=utf-8"
		if soapAction != "" {
			ct += `; action="` + strings.Trim(soapAction, `"`) + `"`
		}
		req.Header.Set("Content-Type", ct)
	} else {
		req.Header.Set("Content-Type", "text/xml; charset=utf-8")
		soapActionHdr := `""` // default: empty quoted string
		if soapAction != "" {
			soapActionHdr = `"` + strings.Trim(soapAction, `"`) + `"`
		}
		req.Header.Set("SOAPAction", soapActionHdr)
		log.Debugf("SOAP Client: SOAPAction header=%s", soapActionHdr)
	}

	// OTel: propagate the active trace span into outgoing HTTP headers so
	// downstream services can join the distributed trace (W3C traceparent/tracestate).
	if spanCtx != nil {
		if injErr := trace.GetTracer().Inject(spanCtx, trace.HTTPHeaders, req.Header); injErr != nil {
			log.Debugf("SOAP Client: trace inject skipped: %s", injErr)
		} else {
			log.Debugf("SOAP Client: trace context injected into outgoing HTTP headers (traceID=%s)", spanCtx.TraceID())
		}
	}

	// Inject Authorization header from the selected HTTP auth connection.
	if a.settings.Authorization && a.authConn != nil {
		conn := a.authConn.GetConnection()
		if conn != nil {
			authHdr, aerr := buildAuthHeader(conn)
			if aerr != nil {
				return false, fmt.Errorf("SOAP Client: auth header: %w", aerr)
			}
			if authHdr != "" {
				req.Header.Set("Authorization", authHdr)
				log.Debugf("SOAP Client: Authorization header injected from connection")
			}
		}
	}

	log.Debugf("SOAP Client: request envelope (bytes=%d):\n%s", len(envXML), string(envXML))

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return false, activity.NewRetriableError(fmt.Sprintf("SOAP Client: HTTP call failed: %s", err.Error()), "HTTP-TRANSPORT-ERROR", nil)
	}
	defer resp.Body.Close()

	output.HttpStatus = resp.StatusCode
	log.Infof("SOAP Client: HTTP response status=%d", resp.StatusCode)

	// Warn when the response Content-Type doesn't indicate XML. The activity
	// still attempts SOAP envelope parsing — if parsing fails the raw body is
	// returned as a string in soapResponsePayload. The warning helps diagnose
	// gateway errors (e.g. a JSON error from a load balancer in front of the
	// SOAP service returning text/html or application/json).
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		ctLower := strings.ToLower(ct)
		if !strings.Contains(ctLower, "xml") && !strings.Contains(ctLower, "soap") {
			log.Warnf("SOAP Client: response Content-Type %q does not indicate XML — response may not be a SOAP envelope", ct)
		}
	}

	// OTel: record HTTP status on the span.
	if spanCtx != nil {
		spanCtx.SetTag("http.status_code", resp.StatusCode)
	}

	maxBytes := int64(a.settings.MaxResponseBodyMB) << 20
	if maxBytes <= 0 {
		maxBytes = 10 << 20 // 10 MiB default
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return false, fmt.Errorf("SOAP Client: read response: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return false, fmt.Errorf("SOAP Client: response body exceeds limit of %d MiB — increase maxResponseBodyMB setting if needed", maxBytes>>20)
	}

	log.Debugf("SOAP Client: response body (bytes=%d):\n%s", len(body), string(body))

	if err := a.parseResponse(ctx, body, resp.StatusCode, output); err != nil {
		return false, err
	}

	if output.IsFault {
		log.Warnf("SOAP Client: SOAP Fault received (httpStatus=%d)", resp.StatusCode)
		if spanCtx != nil {
			spanCtx.SetTag("soap.fault", true)
		}
	} else {
		log.Debugf("SOAP Client: Eval complete — isFault=false httpStatus=%d", resp.StatusCode)
	}

	return true, ctx.SetOutputObject(output)
}

// ---------------------------------------------------------------------------
// Authentication helper
// ---------------------------------------------------------------------------

// buildAuthHeader constructs an HTTP Authorization header value from a Flogo
// HTTP auth connection object. Uses reflection to read fields from the
// authorization.AuthorizationConnection struct without importing the
// proprietary TIBCO package directly. Supports Basic, Bearer Token, and
// OAuth2 (cached access token) connection types.
func buildAuthHeader(conn interface{}) (string, error) {
	rv := reflect.ValueOf(conn)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return "", nil
	}
	authType := reflectStringField(rv, "Type")
	switch authType {
	case "Basic":
		user := reflectStringField(rv, "UserName")
		if user == "" {
			return "", fmt.Errorf("Basic auth connection missing required UserName field")
		}
		pass := reflectStringField(rv, "Password")
		encoded := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
		return "Basic " + encoded, nil
	case "Bearer Token":
		token := reflectStringField(rv, "BearerToken")
		if token == "" {
			return "", fmt.Errorf("Bearer Token auth connection missing required BearerToken field")
		}
		return "Bearer " + token, nil
	case "OAuth2":
		// The Token field holds a cached *authrizationToken with AccessToken.
		tokenField := rv.FieldByName("Token")
		// Guard Kind before calling IsNil — IsNil panics on non-nilable kinds
		// (e.g. struct) and would crash the Flogo engine goroutine.
		if tokenField.IsValid() && tokenField.Kind() == reflect.Ptr && !tokenField.IsNil() {
			accessToken := reflectStringField(tokenField.Elem(), "AccessToken")
			if accessToken != "" {
				return "Bearer " + accessToken, nil
			}
		}
		// Fail explicitly — silently sending an unauthenticated request is worse than failing fast.
		return "", fmt.Errorf("OAuth2 connection has no cached access token — ensure the token has been acquired before invoking this activity")
	default:
		if authType == "" {
			return "", fmt.Errorf("auth connection has no 'Type' field — check the connection configuration")
		}
		return "", fmt.Errorf("unsupported auth connection type %q (supported: Basic, Bearer Token, OAuth2)", authType)
	}
}

// reflectStringField reads an exported string field from a struct via reflection.
func reflectStringField(rv reflect.Value, field string) string {
	f := rv.FieldByName(field)
	if !f.IsValid() || f.Kind() != reflect.String {
		return ""
	}
	return f.String()
}

// ---------------------------------------------------------------------------
// Envelope building
// ---------------------------------------------------------------------------

func (a *Activity) buildEnvelope(ctx activity.Context, input *Input) ([]byte, error) {
	var buf bytes.Buffer

	if a.settings.SoapVersion == "1.2" {
		buf.WriteString(soapEnv12Start)
	} else {
		buf.WriteString(soapEnv11Start)
	}

	if input.SOAPRequestHeaders != nil {
		hdrXML, err := a.toXMLBytes(ctx, input.SOAPRequestHeaders, "headers")
		if err != nil {
			return nil, err
		}
		buf.WriteString("<soap:Header>")
		buf.Write(hdrXML)
		buf.WriteString("</soap:Header>")
	}

	buf.WriteString("<soap:Body>")
	if input.SOAPRequestBody != nil {
		var bodyXML []byte
		var bodyErr error
		if !a.settings.XMLMode && a.wsdlOp != nil &&
			a.wsdlOp.InputMsg != nil && len(a.wsdlOp.InputMsg.Parts) > 0 {
			// WSDL-aware JSON mode: wrap the flat JSON fields in the WSDL operation
			// root element with its target namespace so the service can correctly
			// dispatch the request.
			// Without this, mxj serialises {"a":12,"b":122} as <doc><a>12</a><b>122</b></doc>
			// instead of the required <Add xmlns="http://tempuri.org/"><a>12</a><b>122</b></Add>.
			bodyXML, bodyErr = a.buildWSDLBodyXML(ctx, input.SOAPRequestBody)
		} else {
			bodyXML, bodyErr = a.toXMLBytes(ctx, input.SOAPRequestBody, "body")
		}
		if bodyErr != nil {
			return nil, bodyErr
		}
		buf.Write(bodyXML)
	}
	buf.WriteString("</soap:Body>")
	buf.WriteString(soapEnvEnd)

	return buf.Bytes(), nil
}

// ---------------------------------------------------------------------------
// Response parsing
// ---------------------------------------------------------------------------

// soapEnvelope is a generic envelope for XML unmarshalling.
type soapEnvelope struct {
	XMLName xml.Name  `xml:"Envelope"`
	Header  *soapPart `xml:"Header"`
	Body    soapPart  `xml:"Body"`
}

type soapPart struct {
	Inner []byte `xml:",innerxml"`
}

func (a *Activity) parseResponse(ctx activity.Context, raw []byte, status int, out *Output) error {
	var env soapEnvelope
	if err := xml.Unmarshal(raw, &env); err != nil {
		// If we can't parse as XML, just return the raw body as a string.
		ctx.Logger().Warnf("SOAP Client: HTTP %d — response is not a SOAP envelope (%s)", status, err)
		out.SOAPResponsePayload = string(raw)
		return nil
	}

	// Warn when the response envelope namespace doesn't match the configured SOAP version.
	// Not fatal — many services respond with SOAP 1.1 regardless — but aids debugging.
	wantNS := "http://schemas.xmlsoap.org/soap/envelope/"
	if a.settings.SoapVersion == "1.2" {
		wantNS = "http://www.w3.org/2003/05/soap-envelope"
	}
	if env.XMLName.Space != "" && env.XMLName.Space != wantNS {
		ctx.Logger().Warnf("SOAP Client: response envelope namespace %q does not match configured soapVersion %q",
			env.XMLName.Space, a.settings.SoapVersion)
	}

	if env.Header != nil && len(env.Header.Inner) > 0 {
		out.SOAPResponseHeaders = a.fromXMLBytes(ctx, env.Header.Inner)
	}

	bodyInner := env.Body.Inner
	if len(bodyInner) == 0 {
		return nil
	}

	// Detect fault by unmarshalling to a typed probe — avoids false positives from
	// element names or text content that merely contain the word "fault".
	// Go's xml package matches on local name regardless of namespace prefix,
	// so this handles both SOAP 1.1 <soap:Fault> and SOAP 1.2 <env:Fault>.
	var faultProbe struct {
		XMLName xml.Name `xml:"Fault"`
	}
	if xml.Unmarshal(bodyInner, &faultProbe) == nil && faultProbe.XMLName.Local == "Fault" {
		out.IsFault = true
		out.SOAPResponseFault = a.fromXMLBytes(ctx, bodyInner)
		return nil
	}

	out.SOAPResponsePayload = a.fromXMLBytes(ctx, bodyInner)
	return nil
}

// ---------------------------------------------------------------------------
// Conversion helpers
// ---------------------------------------------------------------------------

func (a *Activity) toXMLBytes(ctx activity.Context, value interface{}, label string) ([]byte, error) {
	if a.settings.XMLMode {
		switch v := value.(type) {
		case string:
			if err := validateXMLFragment([]byte(v)); err != nil {
				return nil, fmt.Errorf("SOAP Client: xmlMode=true but %s is not well-formed XML: %w", label, err)
			}
			return []byte(v), nil
		case []byte:
			if err := validateXMLFragment(v); err != nil {
				return nil, fmt.Errorf("SOAP Client: xmlMode=true but %s is not well-formed XML: %w", label, err)
			}
			return v, nil
		case map[string]interface{}:
			// JSON object mapped from the Flogo designer (complex_object field).
			// Convert to XML using jsonToXMLOrdered so the designer can map a typed
			// object without a type-mismatch validation error.
			// Use @-prefixed keys for XML attributes, e.g.:
			//   {"Add": {"@xmlns": "http://tempuri.org/", "a": 10, "b": 5}}
			//   → <Add xmlns="http://tempuri.org/"><a>10</a><b>5</b></Add>
			b, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("SOAP Client: marshal %s: %w", label, err)
			}
			return jsonToXMLOrdered(b)
		default:
			return nil, fmt.Errorf("SOAP Client: xmlMode=true but %s is not a string or object", label)
		}
	}
	b, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("SOAP Client: marshal JSON %s: %w", label, err)
	}
	// jsonToXMLOrdered uses json.Decoder.Token() so element order matches the
	// JSON input order — safe for services that enforce xs:sequence ordering.
	xmlBytes, err := jsonToXMLOrdered(b)
	if err != nil {
		return nil, fmt.Errorf("SOAP Client: JSON→XML %s: %w", label, err)
	}
	return xmlBytes, nil
}

// fromXMLBytes converts raw XML bytes to either a string (xmlMode) or a JSON
// map (default mode). On parse failure it logs a warning and returns the raw
// bytes as a string — the flow continues rather than failing on response decode.
// Namespace declaration attributes (e.g. xmlns="...", xmlns:tns="...") are stripped
// from the JSON map since they are XML structural metadata, not payload data.
func (a *Activity) fromXMLBytes(ctx activity.Context, raw []byte) interface{} {
	if a.settings.XMLMode {
		return string(raw)
	}
	mv, err := mxj.NewMapXml(raw, false)
	if err != nil {
		ctx.Logger().Warnf("SOAP Client: response XML could not be parsed as JSON map, returning raw string: %s", err)
		return string(raw)
	}
	return stripNamespaceAttrs(mv.Old(), a.settings.XMLAttributePrefix)
}

// buildWSDLBodyXML converts a flat JSON body value to XML wrapped in the WSDL
// operation's root element with its target namespace.
// e.g. {"a":12,"b":122} → <Add xmlns="http://tempuri.org/"><a>12</a><b>122</b></Add>
// Field order is preserved from the JSON input via jsonToXMLOrdered.
func (a *Activity) buildWSDLBodyXML(ctx activity.Context, value interface{}) ([]byte, error) {
	part := a.wsdlOp.InputMsg.Parts[0]
	rootElem := part.ElementName
	if rootElem == "" {
		rootElem = a.wsdlOp.Name // RPC-style fallback
	}
	ns := part.ElementNS
	if ns == "" {
		ns = a.wsdlTargetNS // WSDL-level targetNamespace as a reliable fallback
	}

	b, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("SOAP Client: marshal JSON body: %w", err)
	}
	// Convert body fields to XML preserving document order, then wrap in the
	// WSDL operation root element with its target namespace. This replaces the
	// previous mxj.Map approach which lost key order via Go map iteration.
	childrenXML, err := jsonToXMLOrdered(b)
	if err != nil {
		return nil, fmt.Errorf("SOAP Client: JSON→XML body: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteByte('<')
	buf.WriteString(rootElem)
	if ns != "" {
		fmt.Fprintf(&buf, ` xmlns="%s"`, ns)
	}
	buf.WriteByte('>')
	buf.Write(childrenXML)
	fmt.Fprintf(&buf, "</%s>", rootElem)

	result := buf.Bytes()
	ctx.Logger().Debugf("SOAP Client: WSDL body wrapped — element=<%s> ns=%q size=%d", rootElem, ns, len(result))
	return result, nil
}

// ---------------------------------------------------------------------------
// JSON → XML ordered conversion
// ---------------------------------------------------------------------------

// jsonToXMLOrdered converts a JSON object (as marshalled bytes) to XML, preserving
// the key order of every nested object exactly as it appears in the JSON input.
// It uses json.Decoder.Token() which streams tokens in document order, so the
// XML element sequence reflects the JSON field sequence. This makes the activity
// safe to use with SOAP services that enforce xs:sequence element ordering.
//
// Only a JSON object ({...}) at the top level is accepted; arrays or scalars at
// the top level return an error — use xmlMode=true for non-object payloads.
func jsonToXMLOrdered(jsonBytes []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(jsonBytes))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("invalid JSON input")
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return nil, fmt.Errorf("JSON→XML: top-level value must be a JSON object, got %T", tok)
	}
	var buf bytes.Buffer
	if err := jxWriteObject(dec, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// jxWriteObject writes all key-value pairs of a JSON object (opening { already
// consumed) to buf as XML elements, then consumes the closing }.
// @-prefixed keys are skipped here — they are XML attributes that belong to a
// named element's opening tag and are handled by jxWriteField instead.
func jxWriteObject(dec *json.Decoder, buf *bytes.Buffer) error {
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		key := keyTok.(string)
		if len(key) > 0 && key[0] == '@' {
			// Attribute key without a parent element context — consume and discard.
			var discard json.RawMessage
			if err := dec.Decode(&discard); err != nil {
				return err
			}
			continue
		}
		if err := jxWriteField(dec, buf, key); err != nil {
			return err
		}
	}
	_, err := dec.Token() // consume closing }
	return err
}

// jxWriteField reads the next JSON value from dec and emits it wrapped in
// <name>…</name>. For object values, @-prefixed keys become XML attributes on
// the opening tag. For array values, one <name> element is emitted per item.
func jxWriteField(dec *json.Decoder, buf *bytes.Buffer, name string) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch v := tok.(type) {
	case json.Delim:
		if v == '{' {
			// Collect key-value pairs, separating @-prefixed attribute keys from
			// child element keys. We buffer via json.RawMessage so we can write the
			// opening tag (with attributes) before writing child elements.
			type rawKV struct {
				key string
				val json.RawMessage
			}
			var attrs []rawKV
			var children []rawKV
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return err
				}
				key := keyTok.(string)
				var raw json.RawMessage
				if err := dec.Decode(&raw); err != nil {
					return err
				}
				if len(key) > 0 && key[0] == '@' {
					attrs = append(attrs, rawKV{key[1:], raw})
				} else {
					children = append(children, rawKV{key, raw})
				}
			}
			if _, err := dec.Token(); err != nil { // consume closing }
				return err
			}
			// Write opening tag with any collected XML attributes.
			buf.WriteByte('<')
			buf.WriteString(name)
			for _, a := range attrs {
				buf.WriteByte(' ')
				buf.WriteString(a.key)
				buf.WriteString(`="`)
				var s string
				if jerr := json.Unmarshal(a.val, &s); jerr == nil {
					// String value: XML-escape it.
					escapeXMLAttr(buf, s)
				} else {
					// Number/bool/null: write the raw JSON token as-is (e.g. 42, true).
					// Strip surrounding quotes just in case the raw token is a quoted string
					// that failed to unmarshal above (should not happen, but defensive).
					raw := strings.Trim(string(a.val), `"`)
					buf.WriteString(raw)
				}
				buf.WriteByte('"')
			}
			buf.WriteByte('>')
			// Write child elements using a fresh decoder per buffered value so
			// the parent decoder's stream position is not affected.
			for _, c := range children {
				childDec := json.NewDecoder(bytes.NewReader(c.val))
				childDec.UseNumber()
				if err := jxWriteField(childDec, buf, c.key); err != nil {
					return err
				}
			}
			buf.WriteString("</")
			buf.WriteString(name)
			buf.WriteByte('>')
		} else { // '['
			// Array: emit one <name>…</name> wrapper per item.
			for dec.More() {
				buf.WriteByte('<')
				buf.WriteString(name)
				buf.WriteByte('>')
				if err := jxWriteArrayItem(dec, buf, name); err != nil {
					return err
				}
				buf.WriteString("</")
				buf.WriteString(name)
				buf.WriteByte('>')
			}
			_, err := dec.Token() // consume closing ]
			return err
		}
	default:
		buf.WriteByte('<')
		buf.WriteString(name)
		buf.WriteByte('>')
		jxWriteScalar(buf, v)
		buf.WriteString("</")
		buf.WriteString(name)
		buf.WriteByte('>')
	}
	return nil
}

// jxWriteArrayItem reads and encodes one array item's content into buf.
// The caller is responsible for writing the surrounding <name>…</name> tags.
func jxWriteArrayItem(dec *json.Decoder, buf *bytes.Buffer, name string) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch v := tok.(type) {
	case json.Delim:
		if v == '{' {
			return jxWriteObject(dec, buf)
		}
		// Nested array (rare in SOAP bodies): emit each item's content directly.
		for dec.More() {
			if err := jxWriteArrayItem(dec, buf, name); err != nil {
				return err
			}
		}
		_, err := dec.Token() // consume closing ]
		return err
	default:
		jxWriteScalar(buf, v)
	}
	return nil
}

// jxWriteScalar encodes a JSON scalar (string, number, bool, null) as XML text
// content. Strings are XML-escaped; numbers and booleans are emitted verbatim.
// null emits nothing, producing an empty element (<field></field>).
func jxWriteScalar(buf *bytes.Buffer, v interface{}) {
	switch val := v.(type) {
	case string:
		_ = xml.EscapeText(buf, []byte(val))
	case json.Number:
		buf.WriteString(val.String())
	case bool:
		if val {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
		// nil (JSON null) — emit nothing; caller has already opened the element tag
	}
}

// escapeXMLAttr writes s to buf with XML attribute-safe escaping.
// The characters <, >, &, and " are escaped; all others are written as-is.
func escapeXMLAttr(buf *bytes.Buffer, s string) {
	for _, c := range s {
		switch c {
		case '<':
			buf.WriteString("&lt;")
		case '>':
			buf.WriteString("&gt;")
		case '&':
			buf.WriteString("&amp;")
		case '"':
			buf.WriteString("&quot;")
		default:
			buf.WriteRune(c)
		}
	}
}

// stripNamespaceAttrs recursively removes XML namespace declaration keys from a
// parsed JSON map value. mxj represents xmlns="..." as "-xmlns" (using the
// xmlAttributePrefix). These carry no payload data and would otherwise appear
// as noisy "-xmlns" fields in the activity output.
func stripNamespaceAttrs(v interface{}, attrPrefix string) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, child := range val {
			bare := strings.TrimPrefix(k, attrPrefix)
			if bare == "xmlns" || strings.HasPrefix(bare, "xmlns:") {
				continue
			}
			out[k] = stripNamespaceAttrs(child, attrPrefix)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, item := range val {
			out[i] = stripNamespaceAttrs(item, attrPrefix)
		}
		return out
	default:
		return v
	}
}

// validateXMLFragment checks that b is a well-formed XML fragment by consuming
// all tokens. Returns an error for empty/whitespace-only input or the first
// parser error encountered.
func validateXMLFragment(b []byte) error {
	if len(bytes.TrimSpace(b)) == 0 {
		return fmt.Errorf("XML fragment is empty")
	}
	d := xml.NewDecoder(bytes.NewReader(b))
	for {
		_, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// ---------------------------------------------------------------------------
// TLS helpers
// ---------------------------------------------------------------------------

func buildTLSConfig(s *Settings) (*tls.Config, error) {
	// InsecureSkipVerify defaults to false (secure by default). Set skipTlsVerify=true
	// only when connecting to a service with a self-signed cert that cannot be added
	// to the system trust store — doing so makes the connection vulnerable to MitM attacks.
	cfg := &tls.Config{InsecureSkipVerify: s.SkipTLSVerify} // #nosec G402 — explicit opt-in only
	if !s.EnableTLS {
		return cfg, nil
	}

	if s.ServerCertificate != "" {
		raw, err := decodeCerts(s.ServerCertificate)
		if err != nil {
			return nil, err
		}
		if raw != nil {
			pool := x509.NewCertPool()
			rest := raw
			loaded := 0
			for {
				var blk *pem.Block
				blk, rest = pem.Decode(rest)
				if blk == nil {
					break
				}
				cert, err := x509.ParseCertificate(blk.Bytes)
				if err != nil {
					return nil, fmt.Errorf("SOAP Client: invalid certificate in PEM bundle: %w", err)
				}
				pool.AddCert(cert)
				loaded++
			}
			if loaded == 0 {
				return nil, fmt.Errorf("SOAP Client: unsupported certificate format — expected PEM")
			}
			cfg.RootCAs = pool
			cfg.InsecureSkipVerify = false // pinned cert supersedes skipTlsVerify
		}
	}

	if s.ClientCertificate != "" && s.ClientKey != "" {
		certPEM, err := decodeCerts(s.ClientCertificate)
		if err != nil {
			return nil, err
		}
		keyPEM, err := decodeCerts(s.ClientKey)
		if err != nil {
			return nil, err
		}
		if certPEM != nil && keyPEM != nil {
			pair, err := tls.X509KeyPair(certPEM, keyPEM)
			if err != nil {
				return nil, err
			}
			cfg.Certificates = []tls.Certificate{pair}
		}
	}
	return cfg, nil
}

func decodeCerts(cert string) ([]byte, error) {
	if cert == "" {
		return nil, nil
	}
	// Inline PEM block — detect before any path/base64 checks because base64
	// alphabet includes '/' which would otherwise cause false path matches.
	if strings.HasPrefix(strings.TrimSpace(cert), "-----BEGIN") {
		return []byte(cert), nil
	}
	// Explicit file:// URI.
	if strings.HasPrefix(cert, "file://") {
		return readCertFile(cert[7:])
	}
	// File path: absolute Unix path, Windows drive path, relative directory
	// path, or filename with a well-known cert/key extension.
	// Windows drive letter check requires the third character to be \ or / so
	// that base64 payloads whose second byte is ':' are not misclassified as
	// file paths (producing a confusing "no such file or directory" error).
	if strings.HasPrefix(cert, "/") || strings.HasPrefix(cert, `\`) ||
		(len(cert) > 2 && cert[1] == ':' && (cert[2] == '\\' || cert[2] == '/')) ||
		strings.HasPrefix(cert, "./") || strings.HasPrefix(cert, `.\`) ||
		strings.HasPrefix(cert, "../") || strings.HasPrefix(cert, `..\`) ||
		certHasKnownExtension(cert) {
		return readCertFile(cert)
	}
	decoded, err := base64.StdEncoding.DecodeString(cert)
	if err != nil {
		return nil, fmt.Errorf("SOAP Client: certificate is not a PEM block, file path, or valid base64: %w", err)
	}
	return decoded, nil
}

// certHasKnownExtension reports whether cert looks like a certificate/key
// filename based on its file extension.
func certHasKnownExtension(cert string) bool {
	lower := strings.ToLower(cert)
	for _, ext := range []string{".pem", ".crt", ".cer", ".key", ".pfx", ".p12"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// maxCertFileBytes caps certificate file reads. Real PEM bundles are never
// larger than a few KB; this prevents a misconfigured path from exhausting heap.
const maxCertFileBytes = 1 << 20 // 1 MiB

// readCertFile reads a certificate or key file with a size cap.
func readCertFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCertFileBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxCertFileBytes {
		return nil, fmt.Errorf("SOAP Client: certificate file %q exceeds %d MiB size limit — check the path", path, maxCertFileBytes>>20)
	}
	return data, nil
}

// ---------------------------------------------------------------------------
// Utility
// ---------------------------------------------------------------------------

func operationNames(info *wsdl.WSDLInfo) string {
	// Deduplicate by name: dual SOAP 1.1/1.2 bindings produce two Operation
	// entries with the same name; show each name only once in the error message.
	seen := map[string]bool{}
	names := make([]string, 0, len(info.Operations))
	for _, op := range info.Operations {
		if !seen[op.Name] {
			seen[op.Name] = true
			names = append(names, op.Name)
		}
	}
	return strings.Join(names, ", ")
}

// loadWSDLWithRetry fetches and parses the WSDL, automatically retrying up to
// three times for network sources to handle transient startup failures.
// File sources are attempted only once.
func loadWSDLWithRetry(ctx activity.InitContext, source string, opts *wsdl.ParseOptions) (*wsdl.WSDLInfo, error) {
	isNetwork := strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://")
	maxAttempts := 1
	if isNetwork {
		maxAttempts = 3
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		info, err := wsdl.Parse(source, opts)
		if err == nil {
			return info, nil
		}
		lastErr = err
		if attempt < maxAttempts {
			ctx.Logger().Warnf("SOAP Client: WSDL load attempt %d/%d failed (%s) — retrying in ~1s",
				attempt, maxAttempts, lastErr)
			// Base 1 s delay plus up to 1 s of random jitter. Jitter prevents a
			// thundering herd when multiple app instances restart simultaneously.
			jitter := time.Duration(rand.Intn(1000)) * time.Millisecond //nolint:gosec — non-crypto
			time.Sleep(time.Second + jitter)
		}
	}
	return nil, lastErr
}

// wsdlLabel returns a short human-readable description of the WSDL source
// safe to write to a log file.
//   - Flogo fileselector JSON objects → filename only, or "[WSDL file upload]"
//     when the JSON is malformed (prevents dumping kilobytes of base64 to the log)
//   - HTTP/HTTPS URLs → scheme+host+path, query string stripped to prevent
//     credential leakage when tokens appear as query parameters
//   - File paths → returned unchanged
func wsdlLabel(source string) string {
	source = strings.TrimSpace(source)
	if strings.HasPrefix(source, "{") {
		var sel struct {
			Filename string `json:"filename"`
		}
		if err := json.Unmarshal([]byte(source), &sel); err == nil && sel.Filename != "" {
			return sel.Filename
		}
		// Malformed or missing filename — return a safe fixed label rather than
		// dumping the raw blob (which may be a large base64-encoded WSDL file).
		return "[WSDL file upload]"
	}
	// Strip query string from HTTP URLs before logging.
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		if u, err := url.Parse(source); err == nil {
			u.RawQuery = ""
			u.Fragment = ""
			return u.String()
		}
	}
	return source
}

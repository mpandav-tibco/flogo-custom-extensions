# SOAP Client Activity

![SOAP Client Activity](icons/soap-client.svg)

Invokes SOAP 1.1 and 1.2 web services from a Flogo flow. Handles envelope construction, optional WSDL-driven operation discovery, automatic `SOAPAction` injection, JSON↔XML body conversion, mutual TLS, HTTP authentication, and OpenTelemetry tracing.

Supports:
- SOAP 1.1 and 1.2 — correct envelope namespace and action header per version
- WSDL-driven operation discovery — `soapAction` and body namespace wrapping injected from the WSDL binding automatically
- JSON↔XML conversion with preserved element order (streaming via `json.Decoder.Token()`)
- XML mode — JSON object to XML with `@`-prefixed attribute keys (e.g. `@xmlns`); raw XML strings also accepted
- Mutual TLS — server certificate pinning and client certificate authentication
- HTTP authentication — Basic, Bearer Token, OAuth2 via Flogo HTTP Auth Connector
- OpenTelemetry tracing — child span per call with W3C `traceparent`/`tracestate` propagation
- Built-in Flogo retry (`feature.retry`) and circuit breaker (`feature.circuitBreaker`)
- Configurable response body size cap — prevents OOM from large payloads

---

## Settings

| Setting | Type | Required | Default | Description |
|---------|------|----------|---------|-------------|
| `wsdlSourceType` | string | | `None` | WSDL source: `None`, `WSDL File`, or `WSDL URL`. |
| `wsdlUrl` | string | | — | WSDL file selected via the file browser (shown when `wsdlSourceType=WSDL File`). |
| `wsdlHttpUrl` | string | | — | HTTP/HTTPS URL of the WSDL document (shown when `wsdlSourceType=WSDL URL`). Browser CORS blocks design-time parsing; use `WSDL File` for operation discovery. |
| `wsdlOperation` | string | | — | WSDL operation to invoke; dropdown populated from the parsed WSDL. |
| `autoUseWsdlEndpoint` | boolean | | `false` | Override `soapServiceEndpoint` with the endpoint from the WSDL `<service>` element. |
| `soapServiceEndpoint` | string | ✓ | — | Target SOAP service URL (e.g. `https://service.example.com/api`). |
| `soapVersion` | string | ✓ | `1.1` | SOAP protocol version: `1.1` or `1.2`. |
| `xmlMode` | boolean | | `false` | When `true`, the request body JSON object is converted directly to XML (no WSDL wrapping); use `@`-prefixed keys for XML attributes (e.g. `@xmlns`). The response is returned as a raw XML string. |
| `xmlAttributePrefix` | string | | `-` | Prefix character used for XML attributes in JSON↔XML conversion (JSON mode only). |
| `authorization` | boolean | | `false` | Enable HTTP authentication via the selected auth connector. |
| `authorizationConn` | connection | | — | HTTP auth connector (Basic, Bearer Token, or OAuth2); visible when `authorization=true`. |
| `enableTLS` | boolean | | `false` | Enable custom TLS configuration (reveals certificate fields below). |
| `skipTlsVerify` | boolean | | `false` | Disable TLS certificate verification — **development only**. |
| `serverCertificate` | string | | — | CA or server PEM certificate for server verification / certificate pinning. |
| `clientCertificate` | string | | — | Client PEM certificate for mutual TLS. |
| `clientKey` | string | | — | Client private key for mutual TLS. |
| `timeout` | integer | | `30` | HTTP request timeout in seconds. |
| `maxResponseBodyMB` | integer | | `10` | Maximum response body size in MiB; responses exceeding this limit are rejected. |
| `maxConnsPerHost` | integer | | `100` | Maximum simultaneous HTTP connections to the SOAP endpoint host. Increase for high-concurrency flows. |

---

## Inputs

| Input | Type | Description |
|-------|------|-------------|
| `soapAction` | string | Overrides the `SOAPAction` header (SOAP 1.1) or the `action` parameter in `Content-Type` (SOAP 1.2). When empty and a WSDL operation is configured, injected automatically from the WSDL binding. |
| `httpQueryParams` | object | Key-value pairs appended to the endpoint URL as query parameters, merged with any existing query string from the endpoint. |
| `soapRequestHeaders` | any | SOAP `<Header>` element contents. JSON object (JSON mode) or JSON object / raw XML string (XML mode). Leave empty when no SOAP headers are needed. |
| `soapRequestBody` | complex_object | SOAP `<Body>` element contents. JSON object (JSON mode); JSON object with `@`-prefixed keys for XML attributes or raw XML string (XML mode). |

---

## Outputs

| Output | Type | Description |
|--------|------|-------------|
| `httpStatus` | integer | HTTP status code returned by the SOAP service. |
| `isFault` | boolean | `true` when the response body contained a SOAP `<Fault>` element. |
| `soapResponsePayload` | complex_object | Successful response body — JSON object (JSON mode) or XML string (XML mode). Set when `isFault=false`. |
| `soapResponseHeaders` | any | SOAP response `<Header>` element contents in the same format as the request headers. |
| `soapResponseFault` | complex_object | SOAP Fault details when `isFault=true`. Fields: `faultcode`, `faultstring`, `faultactor`, `detail`. |

---

## XML Mode vs JSON Mode

| | JSON Mode (default) | XML Mode (`xmlMode=true`) |
|-|---------------------|--------------------------|
| **Request body** | JSON object — converted to XML automatically | JSON object with `@`-prefixed keys for attributes, or raw XML string |
| **Response body** | JSON object (namespace attributes stripped) | Raw XML string |
| **XML attributes** | Controlled by `xmlAttributePrefix` setting | Use `@`-prefixed keys: `{"El": {"@xmlns": "http://...", "v": 1}}` |
| **Element order** | Preserved — streaming `json.Decoder.Token()` | Preserved from JSON input; **alphabetical when body is a designer-mapped JSON object** |
| **WSDL body wrapping** | Automatic when a WSDL operation is selected | Bypassed — body converted directly to XML as-is |
| **Best for** | Most services; WSDL-driven typed mapping | Full namespace control, complex attribute placement |

> **Tip:** When exact element ordering is required (WSDL `xs:sequence`), pass a raw XML string as the body rather than a JSON object — Go map serialisation produces alphabetical element order.

---

## WSDL Source Types

| Type | Runtime behaviour | Design-time behaviour |
|------|-------------------|-----------------------|
| `None` | No WSDL — `soapAction` and body structure are fully manual. | — |
| `WSDL File` | File content decoded from the fileselector base64 blob at startup. | Operations and JSON body schema auto-populated from the parsed WSDL. |
| `WSDL URL` | WSDL fetched over HTTP/HTTPS at engine startup (retried 3 times with jitter). | Browser CORS blocks external fetches — use `WSDL File` for design-time operation discovery. |

---

## Certificate Formats

The `serverCertificate`, `clientCertificate`, and `clientKey` settings accept any of the following formats, detected automatically:

| Format | Example |
|--------|---------|
| PEM block (inline) | `-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----` |
| Absolute file path | `/opt/certs/ca.pem` |
| Relative file path | `./certs/client.crt` |
| `file://` URI | `file:///opt/certs/ca.pem` |
| Base64-encoded PEM | `LS0tLS1CRUdJTi...` |
| Well-known extension | `my-service.crt`, `key.pem` |

---

## Validation

**At startup (activity initialisation):**

| Check | Behaviour on failure |
|-------|----------------------|
| `soapVersion` is `"1.1"` or `"1.2"` | Startup error — activity does not initialise. |
| `soapServiceEndpoint` is non-empty (or derivable from WSDL) | Startup error — activity does not initialise. |
| WSDL can be fetched and parsed (when configured) | Startup error after 3 retries for HTTP sources. |
| `wsdlOperation` exists in the WSDL bindings | Startup error — available operations listed in the message. |
| TLS certificates can be parsed and key pair is valid | Startup error. |

**At runtime (each flow invocation):**

| Check | Behaviour on failure |
|-------|----------------------|
| `soapRequestBody` / `soapRequestHeaders` type is string or object | Activity error — flow stops or retry triggers. |
| Endpoint URL can be parsed | Activity error. |
| Response body size within `maxResponseBodyMB` | Activity error. |
| Response XML parseable as a SOAP envelope | Warning logged; raw body returned as string in `soapResponsePayload` — **flow continues**. |

Not validated: request/response body against WSDL/XSD schema; `soapAction` format against the WSDL binding; SOAP version of the response envelope.

---

## Limitations

| Area | Limitation |
|------|------------|
| **WSDL version** | Only WSDL 1.1 is supported. WSDL 2.0 documents will fail to parse. |
| **XSD / schema validation** | Request and response bodies are not validated against the WSDL schema. Violations are reported by the downstream service as a SOAP Fault. |
| **WSDL URL — design-time** | Browser CORS blocks external WSDL fetches. Operation discovery only works at design time with `WSDL File`. |
| **WS-Security / message signing** | No built-in WS-Security, XML-DSig, or XML-Enc. Construct headers manually and pass via `soapRequestHeaders` in XML mode. |
| **MTOM / SOAP with Attachments** | Multipart MIME (MTOM) is not supported. All payloads must be inline XML or base64-encoded within the SOAP body. |
| **XML mode — element ordering via JSON object** | When the body is a designer-mapped JSON object, element order in the resulting XML is alphabetical. Pass a raw XML string when a specific element sequence is required. |
| **OAuth2 token refresh** | Uses the cached `AccessToken` at the time of the call. Token refresh is managed by the connector, not this activity. |
| **Redirect following** | The HTTP client refuses all redirects. Use the final endpoint URL directly. |
| **Connection pool eviction** | `http.Transport` instances are cached indefinitely per TLS configuration. In deployments with many distinct TLS configs, consider replacing the cache with an LRU-bounded implementation. |

---

## Error Reference

| Error message | Cause | Resolution |
|---------------|-------|------------|
| `no service endpoint configured` | `soapServiceEndpoint` is empty and no WSDL `<soap:address>` is available. | Set `soapServiceEndpoint` or check the WSDL `<service>` element. |
| `unsupported soapVersion "X"` | `soapVersion` is not `"1.1"` or `"1.2"`. | Set `soapVersion` to `"1.1"` or `"1.2"`. |
| `WSDL load failed` | WSDL URL/file unreachable or malformed (after 3 retries for HTTP sources). | Verify the WSDL URL and network connectivity. |
| `operation "X" not found in WSDL` | `wsdlOperation` is not present in the WSDL bindings. | Check the operation name; available operations are listed in the error message. |
| `xmlMode=true but body is not a string or object` | Input body type is neither string nor JSON-mapped object. | Map a JSON object to `soapRequestBody`, or pass a plain XML string at runtime. |
| `response body exceeds limit of N MiB` | Service response larger than `maxResponseBodyMB`. | Increase `maxResponseBodyMB` or investigate the service payload size. |
| `SOAP Client: unexpected redirect` | Service redirected the POST request. | Set `soapServiceEndpoint` to the final non-redirecting URL. |
| `HTTP call failed: …` (RetriableError) | Transport error: connection refused, timeout, DNS failure. | Check endpoint reachability; configure Flogo retry in the flow. |
| `authorizationConn: …` | Auth connector cannot be coerced to a connection. | Ensure the connector is properly registered and the connection ID is valid. |

---

## Example — JSON mode, no WSDL

The simplest configuration. Provide the endpoint, version, and a JSON body; the activity wraps in a SOAP envelope and converts JSON to XML automatically.

| Setting | Value |
|---------|-------|
| SOAP Service Endpoint | `http://www.dneonline.com/calculator.asmx` |
| SOAP Version | `1.1` |
| XML Mode | `false` |
| Timeout (seconds) | `30` |

| Input | Value |
|-------|-------|
| `soapAction` | `http://tempuri.org/Add` |
| `soapRequestBody` | `{"Add": {"intA": 10, "intB": 32}}` |

Map `$activity[...].soapResponsePayload.AddResponse.AddResult` to read the result. Set a flow condition on `$activity[...].isFault` to route fault handling.

---

## Example — XML mode with namespace attribute

Use XML mode when the service requires precise namespace placement or attribute control. Provide the body as a JSON object and use `@`-prefixed keys for XML attributes.

| Setting | Value |
|---------|-------|
| SOAP Service Endpoint | `http://www.dneonline.com/calculator.asmx` |
| SOAP Version | `1.1` |
| XML Mode | `true` |

| Input | Value |
|-------|-------|
| `soapAction` | `"http://tempuri.org/Multiply"` |
| `soapRequestBody` | `{"Multiply": {"@xmlns": "http://tempuri.org/", "intA": 6, "intB": 7}}` |

The body is converted to `<Multiply xmlns="http://tempuri.org/"><intA>6</intA><intB>7</intB></Multiply>` and placed inside the SOAP envelope body. The response is returned as a raw XML string in `soapResponsePayload`.

---

## Example — WSDL file with auto soapAction injection

Upload the WSDL in the designer. The runtime fetches it at startup, auto-injects `soapAction`, and wraps the flat body fields in the WSDL operation root element with the correct namespace.

| Setting | Value |
|---------|-------|
| WSDL Source | `WSDL File` |
| WSDL File | _(select via file browser)_ |
| WSDL Operation | `Divide` |
| Auto Use WSDL Endpoint | `true` |
| SOAP Version | `1.2` |
| XML Mode | `false` |

| Input | Value |
|-------|-------|
| `soapRequestBody` | `{"intA": 84, "intB": 2}` |

The activity wraps the body as `<Divide xmlns="http://tempuri.org/"><intA>84</intA><intB>2</intB></Divide>` and reads the `soapAction` and endpoint directly from the WSDL binding.

---

## Example — Mutual TLS with WS-Security SOAP header

Enable mTLS and pass a WS-Security SOAP header as a raw XML string. Raw strings are preferred when precise namespace prefix declarations are required.

| Setting | Value |
|---------|-------|
| SOAP Service Endpoint | `https://mtls.internal.example.com/soap` |
| SOAP Version | `1.1` |
| Enable TLS | `true` |
| Server Certificate | `/opt/certs/ca.pem` |
| Client Certificate | `/opt/certs/client.crt` |
| Client Key | `/opt/certs/client.key` |
| XML Mode | `true` |

| Input | Value |
|-------|-------|
| `soapAction` | `"http://internal.example.com/Process"` |
| `soapRequestHeaders` | `<wsse:Security xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"><wsse:UsernameToken><wsse:Username>svc-user</wsse:Username><wsse:Password>svc-pass</wsse:Password></wsse:UsernameToken></wsse:Security>` |
| `soapRequestBody` | `<Process xmlns="http://internal.example.com/"><Id>12345</Id></Process>` |

---

## Example — HTTP Basic authentication via Auth Connector

Enable authentication and select a Basic auth connector configured in the Flogo designer.

| Setting | Value |
|---------|-------|
| SOAP Service Endpoint | `https://api.example.com/soap` |
| SOAP Version | `1.1` |
| Enable Authentication | `true` |
| Authentication Connection | _(select Basic auth connector)_ |
| XML Mode | `false` |

| Input | Value |
|-------|-------|
| `soapAction` | `http://example.com/GetData` |
| `soapRequestBody` | `{"GetData": {"id": "42"}}` |

---

## Running Tests

```bash
# Unit tests + mock-server integration tests (no network required)
cd activity/soapclient
go test -run "TestActivity|TestWSDL|TestBody|TestJSON" -v ./...

# All tests including live services (require network access to dneonline.com)
go test -v ./...

# Live tests only
go test -run TestLive -v ./...

# With race detector
go test -race -run "TestActivity|TestWSDL|TestBody|TestJSON" ./...
```

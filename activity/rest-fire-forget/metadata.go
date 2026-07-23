package restfireforget

// ---------------------------------------------------------------------------
// Settings — configured once at activity design/initialisation time.
// ---------------------------------------------------------------------------

// Settings holds the compile-time configuration for the REST Fire & Forget activity.
type Settings struct {
	// Timeout bounds how long the detached background request may run before it
	// is abandoned, in milliseconds. This does NOT delay the flow — the flow
	// continues immediately. It only prevents leaked/long-lived goroutines.
	// 0 (or unset) means a 30000 ms default.
	Timeout int `md:"timeout"`

	// SkipTLSVerify disables TLS certificate verification (insecure — dev only).
	SkipTLSVerify bool `md:"skipTlsVerify"`

	// MaxConcurrentRequests is the upper bound on in-flight fire-and-forget
	// requests for this activity instance. Excess requests are rejected
	// (accepted=false) instead of blocking the flow. 0 (or unset) means 1000.
	MaxConcurrentRequests int `md:"maxConcurrentRequests"`
}

// ---------------------------------------------------------------------------
// Input — supplied per flow execution.
// ---------------------------------------------------------------------------

// Input holds the runtime inputs for a single request.
type Input struct {
	// Method is the HTTP method for this request. One of GET, POST, PUT, DELETE,
	// PATCH, HEAD, OPTIONS. The descriptor marks it required (UI enforces it,
	// defaulting to POST); the Go tag is intentionally not "required" so Eval()
	// can default gracefully.
	Method string `md:"method"`

	// URL is the full endpoint URL, e.g. "https://host/path".
	URL string `md:"url,required"`

	// Headers are request headers as a { "Header-Name": "value" } object.
	Headers map[string]interface{} `md:"headers"`

	// QueryParams are appended to the URL as a { "key": "value" } object.
	QueryParams map[string]interface{} `md:"queryParams"`

	// Body is the request payload (string or JSON object). Sent only for
	// methods that carry a body (POST/PUT/PATCH/DELETE).
	Body interface{} `md:"body"`
}

// ---------------------------------------------------------------------------
// Output — returned to the flow immediately after dispatch.
// ---------------------------------------------------------------------------

// Output is the only signal the flow receives; fire-and-forget never surfaces
// the HTTP response itself.
type Output struct {
	// Accepted is true when the request was handed off to the background sender,
	// and false when it was not dispatched (missing/invalid URL, unsupported
	// method, un-marshalable body, or the concurrency limit was reached). The
	// flow neither blocks on nor receives the HTTP response.
	Accepted bool `md:"accepted"`
}

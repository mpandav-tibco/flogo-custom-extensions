package restfireforget

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/coerce"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/log"
)

var activityMd = activity.ToMetadata(&Settings{}, &Input{})

func init() {
	_ = activity.Register(&Activity{}, New)
}

// Activity is a fire-and-forget HTTP client activity: it dispatches a request
// to an endpoint and returns to the flow immediately, without waiting for (or
// reading) the HTTP response.
type Activity struct {
	client  *http.Client
	method  string
	timeout time.Duration
	sem     chan struct{}
	logger  log.Logger
}

// New creates a new REST Fire & Forget activity instance.
func New(ctx activity.InitContext) (activity.Activity, error) {
	if ctx == nil {
		return nil, fmt.Errorf("initialization context cannot be nil")
	}

	s := &Settings{}
	if err := metadata.MapToStruct(ctx.Settings(), s, true); err != nil {
		return nil, fmt.Errorf("failed to map settings: %w", err)
	}

	method := strings.ToUpper(strings.TrimSpace(s.Method))
	if method == "" {
		method = http.MethodPost
	}
	if !isValidMethod(method) {
		return nil, fmt.Errorf("unsupported HTTP method %q (allowed: GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS)", s.Method)
	}

	timeout := 30 * time.Second
	if s.Timeout > 0 {
		timeout = time.Duration(s.Timeout) * time.Millisecond
	}

	maxConc := 1000
	if s.MaxConcurrentRequests > 0 {
		maxConc = s.MaxConcurrentRequests
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: s.SkipTLSVerify}, //nolint:gosec // opt-in via skipTlsVerify
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	a := &Activity{
		client:  &http.Client{Transport: transport},
		method:  method,
		timeout: timeout,
		sem:     make(chan struct{}, maxConc),
		logger:  ctx.Logger(),
	}

	if a.logger != nil {
		a.logger.Infof("REST Fire & Forget initialised: method=%s timeout=%s maxConcurrent=%d skipTLSVerify=%t",
			a.method, a.timeout, maxConc, s.SkipTLSVerify)
	}
	return a, nil
}

// Metadata returns the activity's metadata.
func (a *Activity) Metadata() *activity.Metadata {
	return activityMd
}

// Eval builds the request and dispatches it in the background, returning to the
// flow immediately. The HTTP response is never awaited or returned.
func (a *Activity) Eval(ctx activity.Context) (done bool, err error) {
	if ctx == nil {
		return false, fmt.Errorf("activity context cannot be nil")
	}

	rawURL, _ := coerce.ToString(ctx.GetInput("url"))
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return a.reject("url is required")
	}

	u, perr := url.Parse(rawURL)
	if perr != nil || u.Scheme == "" || u.Host == "" {
		return a.reject(fmt.Sprintf("invalid url %q", rawURL))
	}

	// Append query parameters, if any.
	queryParams, _ := coerce.ToObject(ctx.GetInput("queryParams"))
	if len(queryParams) > 0 {
		q := u.Query()
		for k, v := range queryParams {
			q.Set(k, valueToString(v))
		}
		u.RawQuery = q.Encode()
	}

	// Build the request body (only for methods that carry one).
	bodyReader, contentType, berr := a.buildBody(ctx.GetInput("body"))
	if berr != nil {
		return a.reject(berr.Error())
	}

	// IMPORTANT: use a detached background context (not the activity's context,
	// which is cancelled the instant Eval returns) so the request survives.
	reqCtx, cancel := context.WithTimeout(context.Background(), a.timeout)

	req, rerr := http.NewRequestWithContext(reqCtx, a.method, u.String(), bodyReader)
	if rerr != nil {
		cancel()
		return a.reject(fmt.Sprintf("failed to build request: %v", rerr))
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	headers, _ := coerce.ToObject(ctx.GetInput("headers"))
	for k, v := range headers {
		req.Header.Set(k, valueToString(v))
	}

	// Bound concurrency: reject rather than block the flow when saturated.
	select {
	case a.sem <- struct{}{}:
	default:
		cancel()
		return a.reject("concurrency limit reached; request not dispatched")
	}

	target := u.String()
	go func() {
		defer func() {
			cancel()
			<-a.sem
			if r := recover(); r != nil && a.logger != nil {
				a.logger.Errorf("rest-fire-forget: recovered from panic sending %s %s: %v", a.method, target, r)
			}
		}()

		resp, derr := a.client.Do(req)
		if derr != nil {
			if a.logger != nil {
				a.logger.Warnf("rest-fire-forget: %s %s failed: %v", a.method, target, derr)
			}
			return
		}
		// Drain + close the body so the connection can be reused, then discard.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if a.logger != nil {
			a.logger.Debugf("rest-fire-forget: %s %s -> %d", a.method, target, resp.StatusCode)
		}
	}()

	if a.logger != nil {
		a.logger.Debugf("rest-fire-forget: dispatched %s %s (fire-and-forget)", a.method, target)
	}
	return true, nil
}

// buildBody converts the body input to a reader. Bodies are only sent for
// methods that carry a payload.
func (a *Activity) buildBody(raw interface{}) (io.Reader, string, error) {
	if raw == nil || !methodAllowsBody(a.method) {
		return nil, "", nil
	}
	switch b := raw.(type) {
	case string:
		if b == "" {
			return nil, "", nil
		}
		return strings.NewReader(b), "", nil
	case []byte:
		if len(b) == 0 {
			return nil, "", nil
		}
		return bytes.NewReader(b), "", nil
	default:
		data, err := json.Marshal(b)
		if err != nil {
			return nil, "", fmt.Errorf("failed to marshal body: %w", err)
		}
		return bytes.NewReader(data), "application/json", nil
	}
}

// reject records a client-side failure (bad input / saturation) and returns
// without dispatching. Fire-and-forget has no outputs, so the flow simply
// continues; the reason is logged at WARN.
func (a *Activity) reject(msg string) (bool, error) {
	if a.logger != nil {
		a.logger.Warnf("rest-fire-forget: request not dispatched: %s", msg)
	}
	return true, nil
}

func isValidMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete,
		http.MethodPatch, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

func methodAllowsBody(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func valueToString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

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

var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})

func init() {
	_ = activity.Register(&Activity{}, New)
}

// Activity is a fire-and-forget HTTP client activity: it dispatches a request
// to an endpoint and returns to the flow immediately, without waiting for (or
// reading) the HTTP response.
type Activity struct {
	client  *http.Client
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
		timeout: timeout,
		sem:     make(chan struct{}, maxConc),
		logger:  ctx.Logger(),
	}

	if a.logger != nil {
		a.logger.Infof("REST Fire & Forget initialised: timeout=%s maxConcurrent=%d skipTLSVerify=%t",
			a.timeout, maxConc, s.SkipTLSVerify)
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

	methodRaw, _ := coerce.ToString(ctx.GetInput("method"))
	method := strings.ToUpper(strings.TrimSpace(methodRaw))
	if method == "" {
		method = http.MethodPost
	}
	if !isValidMethod(method) {
		return a.reject(ctx, fmt.Sprintf("unsupported HTTP method %q", method))
	}

	rawURL, _ := coerce.ToString(ctx.GetInput("url"))
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return a.reject(ctx, "url is required")
	}

	u, perr := url.Parse(rawURL)
	if perr != nil || u.Scheme == "" || u.Host == "" {
		return a.reject(ctx, fmt.Sprintf("invalid url %q", rawURL))
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
	bodyReader, contentType, berr := a.buildBody(method, ctx.GetInput("body"))
	if berr != nil {
		return a.reject(ctx, berr.Error())
	}

	// IMPORTANT: use a detached background context (not the activity's context,
	// which is cancelled the instant Eval returns) so the request survives.
	reqCtx, cancel := context.WithTimeout(context.Background(), a.timeout)

	req, rerr := http.NewRequestWithContext(reqCtx, method, u.String(), bodyReader)
	if rerr != nil {
		cancel()
		return a.reject(ctx, fmt.Sprintf("failed to build request: %v", rerr))
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
		return a.reject(ctx, "concurrency limit reached; request not dispatched")
	}

	target := u.String()
	go func() {
		defer func() {
			cancel()
			<-a.sem
			if r := recover(); r != nil && a.logger != nil {
				a.logger.Errorf("rest-fire-forget: recovered from panic sending %s %s: %v", method, target, r)
			}
		}()

		resp, derr := a.client.Do(req)
		if derr != nil {
			if a.logger != nil {
				a.logger.Warnf("rest-fire-forget: %s %s failed: %v", method, target, derr)
			}
			return
		}
		// Drain + close the body so the connection can be reused, then discard.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if a.logger != nil {
			a.logger.Debugf("rest-fire-forget: %s %s -> %d", method, target, resp.StatusCode)
		}
	}()

	if a.logger != nil {
		a.logger.Debugf("rest-fire-forget: dispatched %s %s (fire-and-forget)", method, target)
	}
	_ = ctx.SetOutput("accepted", true)
	return true, nil
}

// buildBody converts the body input to a reader. Bodies are only sent for
// methods that carry a payload.
func (a *Activity) buildBody(method string, raw interface{}) (io.Reader, string, error) {
	if raw == nil || !methodAllowsBody(method) {
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

// reject records a client-side failure (bad input / saturation), reports
// accepted=false, and returns without dispatching. The flow is never failed —
// it continues immediately; the reason is logged at WARN.
func (a *Activity) reject(ctx activity.Context, msg string) (bool, error) {
	if a.logger != nil {
		a.logger.Warnf("rest-fire-forget: request not dispatched: %s", msg)
	}
	_ = ctx.SetOutput("accepted", false)
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

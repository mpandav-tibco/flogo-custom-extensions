package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// rerankMaxRetries is the number of additional attempts after the first for
// transient server errors (429 rate-limit, 502/503/504 gateway errors).
const rerankMaxRetries = 3

// rerankHTTPClient is a package-level client with explicit timeouts.
// Using http.DefaultClient would share its pool with all other Go code in the
// process and has no dial / TLS-handshake timeout, risking goroutine leaks on
// hung rerank endpoints.
var rerankHTTPClient = &http.Client{
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
	},
	// Overall request timeout is governed by the caller's context; this
	// Transport-level deadline covers connection + TLS phases only.
}

// rerankAPIRequest holds parameters for calling the reranking endpoint.
type rerankAPIRequest struct {
	Endpoint  string
	APIKey    string
	Model     string
	Query     string
	Documents []interface{}
	TopN      int
}

// rerankRequestBody is the JSON payload sent to the rerank endpoint.
type rerankRequestBody struct {
	Model     string      `json:"model,omitempty"`
	Query     string      `json:"query"`
	Documents interface{} `json:"documents"`
	TopN      int         `json:"top_n,omitempty"`
}

// rerankResult is a single ranked document in the response.
type rerankResult struct {
	Index          int                    `json:"index"`
	RelevanceScore float64                `json:"relevance_score"`
	Document       map[string]interface{} `json:"document,omitempty"`
}

// rerankResponseBody is the envelope returned by the rerank endpoint.
type rerankResponseBody struct {
	Results []rerankResult `json:"results"`
	// Some providers (Cohere) wrap in "data"
	Data []rerankResult `json:"data"`
}

// callRerankAPI calls the configured reranking HTTP endpoint and returns
// the ranked documents as a []interface{} suitable for setting as output.
// It retries on rate-limit (429) and transient server errors (502, 503, 504)
// using exponential backoff starting at 1 second, matching the behaviour of
// the embeddings package.
func callRerankAPI(ctx context.Context, req rerankAPIRequest) ([]interface{}, error) {
	body := rerankRequestBody{
		Model:     req.Model,
		Query:     req.Query,
		Documents: req.Documents,
		TopN:      req.TopN,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("rerank: marshal request: %w", err)
	}

	backoff := time.Second
	for attempt := 0; attempt <= rerankMaxRetries; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, req.Endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("rerank: create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if req.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)
		}

		resp, err := rerankHTTPClient.Do(httpReq)
		if err != nil {
			if attempt < rerankMaxRetries {
				if sleepErr := rerankSleepWithContext(ctx, backoff); sleepErr != nil {
					return nil, sleepErr
				}
				backoff *= 2
				continue
			}
			return nil, fmt.Errorf("rerank: http request: %w", err)
		}

		// Limit response body to 10 MB to prevent unbounded memory allocation.
		respBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("rerank: read response: %w", readErr)
		}

		// Retry on rate-limit and transient gateway errors.
		if resp.StatusCode == 429 || resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 504 {
			if attempt < rerankMaxRetries {
				if sleepErr := rerankSleepWithContext(ctx, backoff); sleepErr != nil {
					return nil, sleepErr
				}
				backoff *= 2
				continue
			}
			return nil, fmt.Errorf("rerank: endpoint returned HTTP %d after %d retries: %s", resp.StatusCode, rerankMaxRetries, string(respBytes))
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("rerank: endpoint returned HTTP %d: %s", resp.StatusCode, string(respBytes))
		}

		var parsed rerankResponseBody
		if err = json.Unmarshal(respBytes, &parsed); err != nil {
			return nil, fmt.Errorf("rerank: unmarshal response: %w", err)
		}

		// Normalise: some providers use "results", others "data"
		results := parsed.Results
		if len(results) == 0 {
			results = parsed.Data
		}

		out := make([]interface{}, len(results))
		for i, r := range results {
			entry := map[string]interface{}{
				"index":           r.Index,
				"relevance_score": r.RelevanceScore,
			}
			if r.Document != nil {
				entry["document"] = r.Document
			} else if int(r.Index) < len(req.Documents) {
				entry["document"] = req.Documents[r.Index]
			}
			out[i] = entry
		}
		return out, nil
	}
	return nil, fmt.Errorf("rerank: max retries (%d) exceeded", rerankMaxRetries)
}

// rerankSleepWithContext waits for d or until ctx is cancelled.
func rerankSleepWithContext(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

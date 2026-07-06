package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"
)

type rerankHTTPClient struct {
	endpoint string
	apiKey   string
	model    string
	topN     int
	timeout  time.Duration
}

func callRerankAPI(ctx context.Context, endpoint, apiKey, model, query string,
	documents []interface{}, topN, timeoutSecs int) ([]interface{}, error) {

	c := &rerankHTTPClient{
		endpoint: endpoint,
		apiKey:   apiKey,
		model:    model,
		topN:     topN,
		timeout:  time.Duration(timeoutSecs) * time.Second,
	}
	return c.doRerank(ctx, query, documents)
}

func (c *rerankHTTPClient) doRerank(ctx context.Context, query string, documents []interface{}) ([]interface{}, error) {
	// Extract text from documents (support string or {text/content} objects)
	texts := make([]string, 0, len(documents))
	for _, d := range documents {
		switch v := d.(type) {
		case string:
			texts = append(texts, v)
		case map[string]interface{}:
			if t, ok := v["text"].(string); ok {
				texts = append(texts, t)
			} else if t, ok := v["content"].(string); ok {
				texts = append(texts, t)
			}
		}
	}
	if len(texts) == 0 {
		return nil, fmt.Errorf("rerank: no text found in documents")
	}

	reqBody, _ := json.Marshal(map[string]interface{}{
		"model":     c.model,
		"query":     query,
		"documents": texts,
		"top_n":     c.topN,
	})

	const maxRetries = 3
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			jitter := time.Duration(rand.Intn(200)) * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt)*500*time.Millisecond + jitter):
			}
		}

		opCtx, cancel := context.WithTimeout(ctx, c.timeout)
		req, err := http.NewRequestWithContext(opCtx, http.MethodPost, c.endpoint, bytes.NewReader(reqBody))
		if err != nil {
			cancel()
			return nil, fmt.Errorf("rerank: create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}

		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			lastErr = fmt.Errorf("rerank: decode response: %w", err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("rerank: server error %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("rerank: client error %d: %v", resp.StatusCode, result)
		}

		// Parse results - Cohere / Jina / Voyage compatible
		var out []interface{}
		if results, ok := result["results"].([]interface{}); ok {
			for _, r := range results {
				if rm, ok := r.(map[string]interface{}); ok {
					idx := 0
					if i, ok := rm["index"].(float64); ok {
						idx = int(i)
					}
					score := 0.0
					if s, ok := rm["relevance_score"].(float64); ok {
						score = s
					}
					entry := map[string]interface{}{
						"index":           idx,
						"relevance_score": score,
					}
					if idx < len(documents) {
						entry["document"] = documents[idx]
					}
					out = append(out, entry)
				}
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("rerank: max retries exceeded: %w", lastErr)
}

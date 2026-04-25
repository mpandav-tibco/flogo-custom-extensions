// Package vdbembed provides a provider-agnostic HTTP client for generating
// dense vector embeddings. It supports OpenAI (and compatible APIs), Azure
// OpenAI, Cohere v2, and Ollama.
//
// This package has no external dependencies — only Go stdlib — so it can be
// shared across multiple Flogo activities without dragging in large dependency
// graphs.
package vdbembed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// embeddingHTTPClient is a package-level client with explicit timeouts.
// http.DefaultClient has no dial timeout and no TLS-handshake timeout, which
// risks goroutine leaks when an embedding endpoint is slow or unresponsive.
var embeddingHTTPClient = &http.Client{
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
}

// EmbeddingProvider identifies the embedding API provider.
type EmbeddingProvider string

const (
	ProviderOpenAI      EmbeddingProvider = "OpenAI"
	ProviderAzureOpenAI EmbeddingProvider = "Azure OpenAI"
	ProviderCohere      EmbeddingProvider = "Cohere"
	ProviderOllama      EmbeddingProvider = "Ollama"
	ProviderCustom      EmbeddingProvider = "Custom"
)

// EmbeddingRequest holds all parameters for generating vector embeddings.
type EmbeddingRequest struct {
	Provider   EmbeddingProvider
	APIKey     string
	BaseURL    string // provider-specific base URL override
	Model      string
	Texts      []string
	Dimensions int // 0 = model default

	// InputType is the Cohere v2 input_type hint for retrieval quality.
	// Use "search_document" when embedding text for indexing/storage,
	// and "search_query" (or leave empty) when embedding a query.
	// Ignored by all other providers.
	InputType string

	// AzureAPIVersion overrides the Azure OpenAI api-version query parameter
	// (default: "2024-02-01"). Only used when Provider == ProviderAzureOpenAI.
	AzureAPIVersion string
}

// EmbeddingResponse holds the result of an embedding API call.
type EmbeddingResponse struct {
	Embeddings [][]float64
	Dimensions int
	TokensUsed int
}

// CreateEmbeddings dispatches to the correct provider implementation and
// returns dense float64 vectors for all input texts.
func CreateEmbeddings(ctx context.Context, req EmbeddingRequest) (*EmbeddingResponse, error) {
	if len(req.Texts) == 0 {
		return nil, fmt.Errorf("embeddings: at least one input text is required")
	}
	switch req.Provider {
	case ProviderCohere:
		return callCohereEmbedAPI(ctx, req)
	case ProviderOllama:
		return callOllamaEmbedAPI(ctx, req)
	default: // OpenAI, Azure OpenAI, Custom — all use OpenAI-compatible format
		return callOpenAIEmbedAPI(ctx, req)
	}
}

// ---------------------------------------------------------------------------
// HTTP retry helper
// ---------------------------------------------------------------------------

// doHTTPWithRetry executes an HTTP POST to endpoint with the given payload,
// retrying on rate-limit (429) and transient server errors (502, 503, 504)
// using exponential backoff starting at 1 second. setHeaders is called for
// every attempt so callers can attach auth headers without reusing requests.
func doHTTPWithRetry(ctx context.Context, endpoint string, payload []byte, setHeaders func(*http.Request)) ([]byte, error) {
	const maxRetries = 3
	backoff := time.Second

	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		setHeaders(req)

		resp, err := embeddingHTTPClient.Do(req)
		if err != nil {
			if attempt < maxRetries {
				if sleepErr := sleepWithContext(ctx, backoff); sleepErr != nil {
					return nil, sleepErr
				}
				backoff *= 2
				continue
			}
			return nil, fmt.Errorf("http request: %w", err)
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read response: %w", readErr)
		}

		if resp.StatusCode == 429 || resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 504 {
			if attempt < maxRetries {
				if sleepErr := sleepWithContext(ctx, backoff); sleepErr != nil {
					return nil, sleepErr
				}
				backoff *= 2
				continue
			}
			return nil, fmt.Errorf("HTTP %d after %d retries: %s", resp.StatusCode, maxRetries, string(body))
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
		}
		return body, nil
	}
	return nil, fmt.Errorf("max retries (%d) exceeded", maxRetries)
}

// sleepWithContext waits for d or until ctx is cancelled.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ---------------------------------------------------------------------------
// OpenAI-compatible embedding API (OpenAI, Azure OpenAI, Custom)
// ---------------------------------------------------------------------------

type openAIEmbedRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type openAIEmbedData struct {
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

type openAIEmbedUsage struct {
	TotalTokens int `json:"total_tokens"`
}

type openAIEmbedResponse struct {
	Data  []openAIEmbedData `json:"data"`
	Usage openAIEmbedUsage  `json:"usage"`
}

func callOpenAIEmbedAPI(ctx context.Context, req EmbeddingRequest) (*EmbeddingResponse, error) {
	baseURL := strings.TrimRight(req.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	var endpoint string
	if req.Provider == ProviderAzureOpenAI {
		// Azure: BaseURL is the full deployment URL including /embeddings
		endpoint = baseURL
		if !strings.Contains(endpoint, "api-version") {
			apiVersion := req.AzureAPIVersion
			if apiVersion == "" {
				apiVersion = "2024-02-01"
			}
			endpoint += "?api-version=" + apiVersion
		}
	} else {
		endpoint = baseURL + "/embeddings"
	}

	body := openAIEmbedRequest{
		Model:      req.Model,
		Input:      req.Texts,
		Dimensions: req.Dimensions,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("embeddings: marshal request: %w", err)
	}

	respBytes, err := doHTTPWithRetry(ctx, endpoint, payload, func(r *http.Request) {
		if req.APIKey != "" {
			if req.Provider == ProviderAzureOpenAI {
				r.Header.Set("api-key", req.APIKey)
			} else {
				r.Header.Set("Authorization", "Bearer "+req.APIKey)
			}
		}
	})
	if err != nil {
		return nil, fmt.Errorf("embeddings: %w", err)
	}

	var parsed openAIEmbedResponse
	if err = json.Unmarshal(respBytes, &parsed); err != nil {
		return nil, fmt.Errorf("embeddings: unmarshal response: %w", err)
	}
	if len(parsed.Data) == 0 {
		return nil, fmt.Errorf("embeddings: provider returned empty data")
	}

	// Preserve original order using index
	embeddings := make([][]float64, len(parsed.Data))
	for _, d := range parsed.Data {
		if d.Index >= 0 && d.Index < len(parsed.Data) {
			embeddings[d.Index] = d.Embedding
		}
	}

	dims := 0
	if len(embeddings[0]) > 0 {
		dims = len(embeddings[0])
	}
	return &EmbeddingResponse{
		Embeddings: embeddings,
		Dimensions: dims,
		TokensUsed: parsed.Usage.TotalTokens,
	}, nil
}

// ---------------------------------------------------------------------------
// Cohere Embed API v2
// ---------------------------------------------------------------------------

type cohereEmbedRequest struct {
	Model          string   `json:"model"`
	Texts          []string `json:"texts"`
	InputType      string   `json:"input_type"`
	EmbeddingTypes []string `json:"embedding_types"`
}

type cohereEmbedResponse struct {
	Embeddings struct {
		Float [][]float64 `json:"float"`
	} `json:"embeddings"`
	Meta struct {
		BilledUnits struct {
			InputTokens int `json:"input_tokens"`
		} `json:"billed_units"`
	} `json:"meta"`
}

func callCohereEmbedAPI(ctx context.Context, req EmbeddingRequest) (*EmbeddingResponse, error) {
	endpoint := req.BaseURL
	if endpoint == "" {
		endpoint = "https://api.cohere.ai/v2/embed"
	}

	// InputType controls Cohere's retrieval optimisation.
	// "search_document" for text being indexed; "search_query" for query text.
	inputType := req.InputType
	if inputType == "" {
		inputType = "search_query"
	}

	body := cohereEmbedRequest{
		Model:          req.Model,
		Texts:          req.Texts,
		InputType:      inputType,
		EmbeddingTypes: []string{"float"},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("embeddings: cohere marshal request: %w", err)
	}

	respBytes, err := doHTTPWithRetry(ctx, endpoint, payload, func(r *http.Request) {
		if req.APIKey != "" {
			r.Header.Set("Authorization", "Bearer "+req.APIKey)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("embeddings: cohere %w", err)
	}

	var parsed cohereEmbedResponse
	if err = json.Unmarshal(respBytes, &parsed); err != nil {
		return nil, fmt.Errorf("embeddings: cohere unmarshal response: %w", err)
	}
	embeddings := parsed.Embeddings.Float
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("embeddings: cohere returned empty embeddings")
	}

	dims := 0
	if len(embeddings[0]) > 0 {
		dims = len(embeddings[0])
	}
	return &EmbeddingResponse{
		Embeddings: embeddings,
		Dimensions: dims,
		TokensUsed: parsed.Meta.BilledUnits.InputTokens,
	}, nil
}

// ---------------------------------------------------------------------------
// Ollama Embed API (/api/embed)
// ---------------------------------------------------------------------------

type ollamaEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaEmbedResponse struct {
	Embeddings      [][]float64 `json:"embeddings"`
	PromptEvalCount int         `json:"prompt_eval_count"`
}

func callOllamaEmbedAPI(ctx context.Context, req EmbeddingRequest) (*EmbeddingResponse, error) {
	base := strings.TrimRight(req.BaseURL, "/")
	if base == "" {
		base = "http://localhost:11434"
	}
	endpoint := base + "/api/embed"

	body := ollamaEmbedRequest{
		Model: req.Model,
		Input: req.Texts,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("embeddings: ollama marshal request: %w", err)
	}

	respBytes, err := doHTTPWithRetry(ctx, endpoint, payload, func(_ *http.Request) {
		// Ollama does not require authentication headers.
	})
	if err != nil {
		return nil, fmt.Errorf("embeddings: ollama %w", err)
	}

	var parsed ollamaEmbedResponse
	if err = json.Unmarshal(respBytes, &parsed); err != nil {
		return nil, fmt.Errorf("embeddings: ollama unmarshal response: %w", err)
	}
	if len(parsed.Embeddings) == 0 {
		return nil, fmt.Errorf("embeddings: ollama returned empty embeddings")
	}

	dims := 0
	if len(parsed.Embeddings[0]) > 0 {
		dims = len(parsed.Embeddings[0])
	}
	return &EmbeddingResponse{
		Embeddings: parsed.Embeddings,
		Dimensions: dims,
		TokensUsed: parsed.PromptEvalCount,
	}, nil
}

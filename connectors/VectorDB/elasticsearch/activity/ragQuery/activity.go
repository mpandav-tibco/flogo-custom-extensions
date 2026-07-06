package ragQuery

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

	vectordb "github.com/mpandav-tibco/flogo-extensions/vectordb-elasticsearch"
	vectordbconnector "github.com/mpandav-tibco/flogo-extensions/vectordb-elasticsearch/connector"
	vdbembed "github.com/mpandav-tibco/flogo-extensions/vectordb-elasticsearch/embeddings"
	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/metadata"
)

// ragLLMHTTPClient is a package-level HTTP client with explicit transport
// timeouts for LLM generation calls.
var ragLLMHTTPClient = &http.Client{
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
		MaxIdleConns:          5,
		IdleConnTimeout:       90 * time.Second,
	},
}

var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})

func init() { _ = activity.Register(&Activity{}, New) }

type Activity struct {
	settings *Settings
	conn     *vectordbconnector.ElasticsearchConnection
}

func (a *Activity) Metadata() *activity.Metadata { return activityMd }

func New(ctx activity.InitContext) (activity.Activity, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(ctx.Settings(), s, true); err != nil {
		return nil, fmt.Errorf("vectordb-rag: %w", err)
	}
	if s.Connection == nil {
		return nil, fmt.Errorf("vectordb-rag: connection is required")
	}
	conn, ok := s.Connection.GetConnection().(*vectordbconnector.ElasticsearchConnection)
	if !ok {
		return nil, fmt.Errorf("vectordb-rag: invalid connection type, expected *ElasticsearchConnection")
	}

	// Inherit embedding settings from connector when not set.
	cs := conn.GetSettings()
	if cs.EnableEmbedding {
		if s.EmbeddingProvider == "" {
			s.EmbeddingProvider = cs.EmbeddingProvider
		}
		if s.EmbeddingAPIKey == "" {
			s.EmbeddingAPIKey = cs.EmbeddingAPIKey
		}
		if s.EmbeddingBaseURL == "" {
			s.EmbeddingBaseURL = cs.EmbeddingBaseURL
		}
	}

	if s.DefaultTopK <= 0 {
		s.DefaultTopK = 5
	}

	ctx.Logger().Infof("RAGQuery initialised: connection=%s embeddingProvider=%s embeddingModel=%s defaultTopK=%d",
		conn.GetName(), s.EmbeddingProvider, s.EmbeddingModel, s.DefaultTopK)
	return &Activity{settings: s, conn: conn}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	l := ctx.Logger()
	l.Debugf("RAGQuery: starting eval")

	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("vectordb-rag: %w", err)
	}

	collectionName := input.CollectionName
	if collectionName == "" {
		collectionName = a.settings.DefaultCollection
	}
	if collectionName == "" {
		return false, fmt.Errorf("vectordb-rag: collectionName is required")
	}
	if input.Query == "" {
		return false, fmt.Errorf("vectordb-rag: query is required")
	}

	topK := input.TopK
	if topK <= 0 {
		topK = a.settings.DefaultTopK
	}
	if topK <= 0 {
		topK = 5
	}

	tc := ctx.GetTracingContext()
	if tc != nil {
		tc.SetTag("db.system", "vectordb")
		tc.SetTag("db.operation", "ragQuery")
		tc.SetTag("db.vectordb.provider", "elasticsearch")
		tc.SetTag("db.vectordb.collection", collectionName)
		tc.SetTag("db.vectordb.top_k", topK)
	}

	start := time.Now()

	// Step 1: Embed the query
	var queryVector []float64
	if a.settings.EmbeddingProvider != "" && a.settings.EmbeddingModel != "" {
		connSettings := a.conn.GetSettings()
		embedTimeout := connSettings.TimeoutSeconds
		if embedTimeout <= 0 {
			embedTimeout = 30
		}
		embedCtx, embedCancel := context.WithTimeout(ctx.GoContext(), time.Duration(embedTimeout)*time.Second)
		embResp, embErr := vdbembed.CreateEmbeddings(embedCtx, vdbembed.EmbeddingRequest{
			Provider: vdbembed.EmbeddingProvider(a.settings.EmbeddingProvider),
			APIKey:   a.settings.EmbeddingAPIKey,
			BaseURL:  a.settings.EmbeddingBaseURL,
			Model:    a.settings.EmbeddingModel,
			Texts:    []string{input.Query},
		})
		embedCancel()
		if embErr != nil {
			l.Errorf("RAGQuery: embed error=%v", embErr)
			if tc != nil {
				tc.SetTag("error", true)
			}
			if err2 := ctx.SetOutputObject(&Output{
				Success:  false,
				Error:    embErr.Error(),
				Duration: time.Since(start).String(),
			}); err2 != nil {
				l.Errorf("SetOutputObject: %v", err2)
			}
			return true, nil
		}
		if len(embResp.Embeddings) > 0 {
			queryVector = embResp.Embeddings[0]
		}
	}

	// Step 2: Vector or hybrid search
	connSettings := a.conn.GetSettings()
	searchTimeout := connSettings.TimeoutSeconds
	if searchTimeout <= 0 {
		searchTimeout = 30
	}
	searchCtx, searchCancel := context.WithTimeout(ctx.GoContext(), time.Duration(searchTimeout)*time.Second)
	defer searchCancel()

	var results []vectordb.SearchResult
	var searchErr error

	alpha := a.settings.HybridAlpha
	if alpha > 0 && alpha < 1 && input.Query != "" && len(queryVector) > 0 {
		results, searchErr = a.conn.GetClient().HybridSearch(searchCtx, vectordb.HybridSearchRequest{
			CollectionName: collectionName,
			QueryText:      input.Query,
			QueryVector:    queryVector,
			TopK:           topK,
			ScoreThreshold: a.settings.ScoreThreshold,
			Filters:        input.Filters,
			Alpha:          alpha,
		})
	} else {
		results, searchErr = a.conn.GetClient().VectorSearch(searchCtx, vectordb.SearchRequest{
			CollectionName: collectionName,
			QueryVector:    queryVector,
			TopK:           topK,
			ScoreThreshold: a.settings.ScoreThreshold,
			Filters:        input.Filters,
		})
	}

	if searchErr != nil {
		l.Errorf("RAGQuery: search error=%v", searchErr)
		if tc != nil {
			tc.SetTag("error", true)
		}
		if err2 := ctx.SetOutputObject(&Output{
			Success:  false,
			Error:    searchErr.Error(),
			Duration: time.Since(start).String(),
		}); err2 != nil {
			l.Errorf("SetOutputObject: %v", err2)
		}
		return true, nil
	}

	// Step 3: Build context string from retrieved docs
	var ctxBuilder strings.Builder
	for i, r := range results {
		ctxBuilder.WriteString(fmt.Sprintf("Document %d (score=%.4f):\n%s\n\n", i+1, r.Score, r.Content))
	}
	retrievedContext := ctxBuilder.String()

	// Step 4: Generate answer via LLM (optional)
	answer := ""
	if a.settings.LLMEndpoint != "" {
		prompt := fmt.Sprintf("Context:\n%s\n\nQuestion: %s\nAnswer:", retrievedContext, input.Query)
		var llmErr error
		answer, llmErr = callLLM(ctx.GoContext(), a.settings.LLMEndpoint, a.settings.LLMAPIKey,
			a.settings.LLMModel, prompt, a.settings.LLMTimeoutSeconds)
		if llmErr != nil {
			l.Warnf("RAGQuery: LLM call failed: %v (returning context only)", llmErr)
		}
	}

	duration := time.Since(start)
	l.Debugf("RAGQuery: collection=%s results=%d duration=%s hasAnswer=%v",
		collectionName, len(results), duration, answer != "")

	if tc != nil {
		tc.SetTag("db.vectordb.result_count", len(results))
	}

	searchResultsOut := make([]interface{}, len(results))
	for i, r := range results {
		b, _ := json.Marshal(r)
		var m map[string]interface{}
		_ = json.Unmarshal(b, &m)
		searchResultsOut[i] = m
	}

	if err := ctx.SetOutputObject(&Output{
		Success:           true,
		Answer:            answer,
		Context:           retrievedContext,
		SearchResults:     searchResultsOut,
		SearchResultCount: len(results),
		Duration:          duration.String(),
	}); err != nil {
		l.Errorf("SetOutputObject: %v", err)
	}
	return true, nil
}

func callLLM(ctx context.Context, endpoint, apiKey, model, prompt string, timeoutSecs int) (string, error) {
	if timeoutSecs <= 0 {
		timeoutSecs = 30
	}
	opCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	reqBody, _ := json.Marshal(map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})

	req, err := http.NewRequestWithContext(opCtx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("LLM: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := ragLLMHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("LLM: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LLM: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("LLM: decode response: %w", err)
	}

	// Extract content from OpenAI-compatible response
	if choices, ok := result["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if msg, ok := choice["message"].(map[string]interface{}); ok {
				if content, ok := msg["content"].(string); ok {
					return content, nil
				}
			}
			if text, ok := choice["text"].(string); ok {
				return text, nil
			}
		}
	}
	// Ollama generate endpoint
	if response, ok := result["response"].(string); ok {
		return response, nil
	}
	return "", fmt.Errorf("LLM: unexpected response format")
}

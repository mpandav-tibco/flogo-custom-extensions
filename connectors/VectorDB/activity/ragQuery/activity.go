package ragQuery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mpandav-tibco/flogo-extensions/vectordb"
	vectordbconnector "github.com/mpandav-tibco/flogo-extensions/vectordb/connector"
	vdbembed "github.com/mpandav-tibco/flogo-extensions/vectordb/embeddings"
	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/metadata"
)

var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})

func init() { _ = activity.Register(&Activity{}, New) }

type Activity struct {
	settings *Settings
	conn     *vectordbconnector.VectorDBConnection
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
	conn, ok := s.Connection.GetConnection().(*vectordbconnector.VectorDBConnection)
	if !ok {
		return nil, fmt.Errorf("vectordb-rag: invalid connection type, expected *VectorDBConnection")
	}

	// Resolve embedding credentials: inherit from connector when opted in.
	// Activity-level values (if set) always take precedence as an override.
	if s.UseConnectorEmbedding {
		connSettings := conn.GetSettings()
		if !connSettings.EnableEmbedding {
			ctx.Logger().Warnf("RAGQuery: useConnectorEmbedding=true but connector does not have enableEmbedding set — falling back to activity-level settings")
		} else {
			if s.EmbeddingProvider == "" {
				s.EmbeddingProvider = connSettings.EmbeddingProvider
			}
			if s.EmbeddingAPIKey == "" {
				s.EmbeddingAPIKey = connSettings.EmbeddingAPIKey
			}
			if s.EmbeddingBaseURL == "" {
				s.EmbeddingBaseURL = connSettings.EmbeddingBaseURL
			}
		}
	}

	if s.EmbeddingProvider == "" {
		s.EmbeddingProvider = string(vdbembed.ProviderOpenAI)
	}
	if s.EmbeddingModel == "" {
		return nil, fmt.Errorf("vectordb-rag: embeddingModel is required")
	}
	if s.DefaultTopK <= 0 {
		s.DefaultTopK = 5
	}
	if s.ContentField == "" {
		s.ContentField = "text"
	}
	if s.ContextFormat == "" {
		s.ContextFormat = "numbered"
	}
	if s.TimeoutSeconds <= 0 {
		s.TimeoutSeconds = 30
	}
	// LLM generation defaults (only applied when EnableLLMGenerate=true at runtime)
	if s.LLMProvider == "" {
		s.LLMProvider = "Ollama"
	}
	if s.LLMModel == "" {
		s.LLMModel = "llama3.1:8b"
	}
	if s.LLMBaseURL == "" && s.LLMProvider == "Ollama" {
		s.LLMBaseURL = "http://localhost:11434"
	}
	if s.MaxTokens <= 0 {
		s.MaxTokens = 1024
	}
	if s.SystemPrompt == "" {
		s.SystemPrompt = "You are a helpful assistant. Answer the question using only the provided context. If the context does not contain enough information, say so."
	}
	ctx.Logger().Infof("RAGQuery initialised: connection=%s provider=%s embeddingModel=%s defaultTopK=%d llmGenerate=%v",
		conn.GetName(), s.EmbeddingProvider, s.EmbeddingModel, s.DefaultTopK, s.EnableLLMGenerate)
	return &Activity{settings: s, conn: conn}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	l := ctx.Logger()
	l.Debugf("RAGQuery: starting eval")

	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("vectordb-rag: %w", err)
	}
	if input.QueryText == "" {
		return false, fmt.Errorf("vectordb-rag: queryText is required")
	}

	collectionName := input.CollectionName
	if collectionName == "" {
		collectionName = a.settings.DefaultCollection
	}
	if collectionName == "" {
		return false, fmt.Errorf("vectordb-rag: collectionName is required")
	}

	topK := input.TopK
	if topK <= 0 {
		topK = a.settings.DefaultTopK
	}

	l.Debugf("RAGQuery: query=%q collection=%s topK=%d", input.QueryText, collectionName, topK)

	// OTel trace tags
	tc := ctx.GetTracingContext()
	if tc != nil {
		tc.SetTag("ai.operation", "ragQuery")
		tc.SetTag("ai.embedding_provider", a.settings.EmbeddingProvider)
		tc.SetTag("ai.embedding_model", a.settings.EmbeddingModel)
		tc.SetTag("db.system", "vectordb")
		tc.SetTag("db.vectordb.provider", a.conn.GetSettings().DBType)
		tc.SetTag("db.vectordb.collection", collectionName)
		tc.SetTag("db.vectordb.top_k", topK)
	}

	opCtx, cancel := context.WithTimeout(ctx.GoContext(), time.Duration(a.settings.TimeoutSeconds)*time.Second)
	defer cancel()

	start := time.Now()

	// Step 1: Embed the query text
	l.Debugf("RAGQuery: embedding query with provider=%s model=%s", a.settings.EmbeddingProvider, a.settings.EmbeddingModel)
	embResult, embErr := vdbembed.CreateEmbeddings(opCtx, vdbembed.EmbeddingRequest{
		Provider:   vdbembed.EmbeddingProvider(a.settings.EmbeddingProvider),
		APIKey:     a.settings.EmbeddingAPIKey,
		BaseURL:    a.settings.EmbeddingBaseURL,
		Model:      a.settings.EmbeddingModel,
		Texts:      []string{input.QueryText},
		Dimensions: a.settings.EmbeddingDimensions,
	})
	if embErr != nil {
		l.Errorf("RAGQuery: embedding failed: %v", embErr)
		if tc != nil {
			tc.SetTag("error", true)
			tc.LogKV(map[string]interface{}{"event": "error", "message": embErr.Error()})
		}
		if err := ctx.SetOutputObject(&Output{
			Success:  false,
			Error:    fmt.Sprintf("embedding failed: %s", embErr.Error()),
			Duration: time.Since(start).String(),
		}); err != nil {
			l.Errorf("SetOutputObject: %v", err)
		}
		return false, fmt.Errorf("vectordb-rag: embedding failed: %w", embErr)
	}
	queryVector := embResult.Embeddings[0]
	l.Debugf("RAGQuery: query embedded: dims=%d tokens=%d", len(queryVector), embResult.TokensUsed)

	// Step 2: Vector search (or hybrid search if configured)
	fqv := make([]float64, len(queryVector))
	copy(fqv, queryVector)

	var searchResults []vectordb.SearchResult
	var searchErr error

	if a.settings.UseHybridSearch {
		alpha := a.settings.HybridAlpha
		// Descriptor default is 0.5 (balanced). Users who want pure BM25 should set
		// alpha=0.0; users who want pure dense vector should set alpha=1.0.
		// Negative values or values >1 are rejected as invalid configuration.
		if alpha < 0 || alpha > 1 {
			return false, fmt.Errorf("vectordb-rag: hybridAlpha must be between 0.0 and 1.0, got %.4f", alpha)
		}
		l.Debugf("RAGQuery: hybrid search collection=%s topK=%d alpha=%.2f", collectionName, topK, alpha)
		searchResults, searchErr = a.conn.GetClient().HybridSearch(opCtx, vectordb.HybridSearchRequest{
			CollectionName: collectionName,
			QueryText:      input.QueryText,
			QueryVector:    fqv,
			TopK:           topK,
			ScoreThreshold: a.settings.ScoreThreshold,
			Filters:        input.Filters,
			Alpha:          alpha,
			// SkipPayload defaults to false (zero value) = include payload.
		})
	} else {
		searchResults, searchErr = a.conn.GetClient().VectorSearch(opCtx, vectordb.SearchRequest{
			CollectionName: collectionName,
			QueryVector:    fqv,
			TopK:           topK,
			ScoreThreshold: a.settings.ScoreThreshold,
			Filters:        input.Filters,
			// SkipPayload defaults to false (zero value) = include payload.
			WithVectors: false,
		})
	}
	if searchErr != nil {
		searchMode := "vector search"
		if a.settings.UseHybridSearch {
			searchMode = "hybrid search"
		}
		l.Errorf("RAGQuery: %s failed: collection=%s error=%v", searchMode, collectionName, searchErr)
		if tc != nil {
			tc.SetTag("error", true)
			tc.LogKV(map[string]interface{}{"event": "error", "message": searchErr.Error()})
		}
		if err := ctx.SetOutputObject(&Output{
			Success:  false,
			Error:    fmt.Sprintf("%s failed: %s", searchMode, searchErr.Error()),
			Duration: time.Since(start).String(),
		}); err != nil {
			l.Errorf("SetOutputObject: %v", err)
		}
		return false, fmt.Errorf("vectordb-rag: %s failed: %w", searchMode, searchErr)
	}

	duration := time.Since(start)
	l.Debugf("RAGQuery: retrieved %d documents duration=%s", len(searchResults), duration)
	if tc != nil {
		tc.SetTag("db.vectordb.result_count", len(searchResults))
	}

	// Step 3: Format context string for LLM
	formattedContext := formatContext(searchResults, a.settings.ContentField, a.settings.ContextFormat)

	// Build queryEmbedding output ([]interface{})
	qEmbOut := make([]interface{}, len(queryVector))
	for i, f := range queryVector {
		qEmbOut[i] = f
	}

	// Build sourceDocuments output
	sourceDocs := searchResultsToInterface(searchResults)

	// Step 4 (optional): LLM answer generation
	answer := ""
	if a.settings.EnableLLMGenerate {
		systemPrompt := a.settings.SystemPrompt
		if input.SystemPrompt != "" {
			systemPrompt = input.SystemPrompt
		}
		l.Debugf("RAGQuery: generating answer with llmProvider=%s llmModel=%s", a.settings.LLMProvider, a.settings.LLMModel)
		var llmErr error
		answer, llmErr = a.generate(opCtx, input.QueryText, formattedContext, systemPrompt)
		if llmErr != nil {
			l.Warnf("RAGQuery: LLM generation failed (%v) — returning context only", llmErr)
			answer = fmt.Sprintf("[LLM generation failed: %s]\n\nRetrieved context:\n%s", llmErr.Error(), formattedContext)
		}
	}

	if err := ctx.SetOutputObject(&Output{
		Success:          true,
		Answer:           answer,
		FormattedContext: formattedContext,
		SourceDocuments:  sourceDocs,
		QueryEmbedding:   qEmbOut,
		TotalFound:       len(searchResults),
		Duration:         duration.String(),
	}); err != nil {
		l.Errorf("SetOutputObject: %v", err)
	}
	return true, nil
}

// formatContext builds an LLM-ready context string from search results.
func formatContext(results []vectordb.SearchResult, contentField, format string) string {
	if len(results) == 0 {
		return ""
	}
	if format == "json" {
		return formatContextJSON(results, contentField)
	}
	var sb strings.Builder
	for i, r := range results {
		content := extractContent(r, contentField)
		switch format {
		case "markdown":
			fmt.Fprintf(&sb, "**[%d]** *(score: %.4f)*\n\n%s\n\n---\n\n", i+1, r.Score, content)
		case "xml":
			fmt.Fprintf(&sb, "<document id=\"%d\" score=\"%.4f\">\n%s\n</document>\n", i+1, r.Score, content)
		case "plain":
			fmt.Fprintf(&sb, "%s\n\n", content)
		default: // "numbered"
			fmt.Fprintf(&sb, "%d. %s\n\n", i+1, content)
		}
	}
	if format == "xml" {
		return "<context>\n" + sb.String() + "</context>"
	}
	return strings.TrimRight(sb.String(), "\n")
}

// formatContextJSON serialises results as a JSON array for native Flogo consumption.
// Each element: {"index":1,"id":"...","score":0.92,"content":"...","payload":{...}}
func formatContextJSON(results []vectordb.SearchResult, contentField string) string {
	type jsonDoc struct {
		Index   int                    `json:"index"`
		ID      string                 `json:"id"`
		Score   float64                `json:"score"`
		Content string                 `json:"content"`
		Payload map[string]interface{} `json:"payload"`
	}
	docs := make([]jsonDoc, len(results))
	for i, r := range results {
		payload := r.Payload
		if payload == nil {
			payload = map[string]interface{}{}
		}
		docs[i] = jsonDoc{
			Index:   i + 1,
			ID:      r.ID,
			Score:   r.Score,
			Content: extractContent(r, contentField),
			Payload: payload,
		}
	}
	b, err := json.Marshal(docs)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// extractContent pulls the text content from a SearchResult.
// Priority: (1) r.Content (the first-class field populated by all providers),
// (2) r.Payload[contentField] (custom key), (3) fallback — join all payload strings.
func extractContent(r vectordb.SearchResult, contentField string) string {
	// First-class Content field is always populated by all VectorDB providers.
	if r.Content != "" {
		return r.Content
	}
	if r.Payload != nil {
		if val, ok := r.Payload[contentField]; ok && val != nil {
			return fmt.Sprintf("%v", val)
		}
	}
	// Last resort: join all non-empty string payload values.
	var parts []string
	for k, v := range r.Payload {
		if s, ok := v.(string); ok && s != "" {
			parts = append(parts, fmt.Sprintf("%s: %s", k, s))
		}
	}
	return strings.Join(parts, " | ")
}

// searchResultsToInterface converts []SearchResult to []interface{} for Flogo output.
func searchResultsToInterface(results []vectordb.SearchResult) []interface{} {
	out := make([]interface{}, len(results))
	for i, r := range results {
		out[i] = map[string]interface{}{
			"id":      r.ID,
			"score":   r.Score,
			"content": r.Content,
			"payload": r.Payload,
		}
	}
	return out
}

// ── LLM generation ──────────────────────────────────────────────────────────

// generate calls the configured LLM to produce an answer grounded in context.
func (a *Activity) generate(ctx context.Context, query, context_, systemPrompt string) (string, error) {
	prompt := buildPrompt(systemPrompt, context_, query)
	switch a.settings.LLMProvider {
	case "Ollama":
		return a.generateOllama(ctx, prompt)
	default: // OpenAI, Azure OpenAI, Custom
		return a.generateOpenAICompat(ctx, prompt)
	}
}

// buildPrompt constructs the full prompt from system prompt, context, and query.
func buildPrompt(systemPrompt, context_, query string) string {
	var sb strings.Builder
	if systemPrompt != "" {
		sb.WriteString(systemPrompt)
		sb.WriteString("\n\n")
	}
	sb.WriteString("Context:\n")
	sb.WriteString(context_)
	sb.WriteString("\n\nQuestion: ")
	sb.WriteString(query)
	sb.WriteString("\n\nAnswer:")
	return sb.String()
}

// ollamaGenerateRequest is the Ollama /api/generate request body.
type ollamaGenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// ollamaGenerateResponse is the non-streaming Ollama response.
type ollamaGenerateResponse struct {
	Response string `json:"response"`
	Error    string `json:"error,omitempty"`
}

func (a *Activity) generateOllama(ctx context.Context, prompt string) (string, error) {
	baseURL := strings.TrimRight(a.settings.LLMBaseURL, "/")
	url := baseURL + "/api/generate"

	reqBody, _ := json.Marshal(ollamaGenerateRequest{
		Model:  a.settings.LLMModel,
		Prompt: prompt,
		Stream: false,
	})

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("ollama: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("ollama: http: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama: status %d: %s", resp.StatusCode, string(body))
	}

	var result ollamaGenerateResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("ollama: parse response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("ollama: %s", result.Error)
	}
	return strings.TrimSpace(result.Response), nil
}

// openAIChatRequest is a minimal OpenAI /v1/chat/completions request body.
type openAIChatRequest struct {
	Model       string              `json:"model"`
	Messages    []openAIChatMessage `json:"messages"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
	Temperature float64             `json:"temperature,omitempty"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message openAIChatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (a *Activity) generateOpenAICompat(ctx context.Context, prompt string) (string, error) {
	baseURL := strings.TrimRight(a.settings.LLMBaseURL, "/")
	url := baseURL + "/v1/chat/completions"

	reqBody, _ := json.Marshal(openAIChatRequest{
		Model: a.settings.LLMModel,
		Messages: []openAIChatMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens:   a.settings.MaxTokens,
		Temperature: a.settings.Temperature,
	})

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("openai-compat: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if a.settings.LLMAPIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+a.settings.LLMAPIKey)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("openai-compat: http: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai-compat: status %d: %s", resp.StatusCode, string(body))
	}

	var result openAIChatResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("openai-compat: parse response: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("openai-compat: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("openai-compat: no choices in response")
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

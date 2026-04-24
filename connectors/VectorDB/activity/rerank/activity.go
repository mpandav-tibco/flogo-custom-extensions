package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/log"
)

var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})
var logger = log.ChildLogger(log.RootLogger(), "vectordb-rerank")

func init() { _ = activity.Register(&Activity{}, New) }

type Activity struct {
	settings   *Settings
	httpClient *http.Client
}

func (a *Activity) Metadata() *activity.Metadata { return activityMd }

func New(ctx activity.InitContext) (activity.Activity, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(ctx.Settings(), s, true); err != nil {
		return nil, fmt.Errorf("vectordb-rerank: %w", err)
	}
	if s.RerankEndpoint == "" {
		return nil, fmt.Errorf("vectordb-rerank: rerankEndpoint is required")
	}
	timeout := s.TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	httpClient := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	return &Activity{settings: s, httpClient: httpClient}, nil
}

type rerankRequest struct {
	Model     string   `json:"model,omitempty"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n,omitempty"`
}

type rerankResponseItem struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

type rerankResponse struct {
	Results []rerankResponseItem `json:"results"`
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("vectordb-rerank: %w", err)
	}
	if input.Query == "" {
		return false, fmt.Errorf("vectordb-rerank: query is required")
	}
	if len(input.Documents) == 0 {
		return false, fmt.Errorf("vectordb-rerank: documents must not be empty")
	}

	// Extract text from documents (support string or map with "content" key)
	texts := make([]string, len(input.Documents))
	for i, d := range input.Documents {
		switch v := d.(type) {
		case string:
			texts[i] = v
		case map[string]interface{}:
			if c, ok := v["content"]; ok {
				texts[i] = fmt.Sprintf("%v", c)
			} else {
				b, _ := json.Marshal(v)
				texts[i] = string(b)
			}
		default:
			texts[i] = fmt.Sprintf("%v", d)
		}
	}

	topN := a.settings.TopN
	if topN <= 0 {
		topN = len(texts)
	}

	reqBody := rerankRequest{
		Model:     a.settings.Model,
		Query:     input.Query,
		Documents: texts,
		TopN:      topN,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	timeout := a.settings.TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	opCtx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(opCtx, http.MethodPost, a.settings.RerankEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return false, fmt.Errorf("vectordb-rerank: failed to build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if a.settings.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+a.settings.APIKey)
	}

	start := time.Now()
	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		logger.Errorf("vectordb-rerank: HTTP error: %v", err)
		_ = ctx.SetOutputObject(&Output{Success: false, Error: err.Error(), Duration: time.Since(start).String()})
		return true, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodySnippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		errMsg := fmt.Sprintf("rerank API returned HTTP %d: %s", resp.StatusCode, string(bodySnippet))
		logger.Errorf("vectordb-rerank: %s", errMsg)
		_ = ctx.SetOutputObject(&Output{Success: false, Error: errMsg, Duration: time.Since(start).String()})
		return true, nil
	}

	var rerankResp rerankResponse
	if err = json.NewDecoder(resp.Body).Decode(&rerankResp); err != nil {
		logger.Errorf("vectordb-rerank: failed to decode response: %v", err)
		_ = ctx.SetOutputObject(&Output{Success: false, Error: err.Error(), Duration: time.Since(start).String()})
		return true, nil
	}

	// Reorder original documents by relevance score
	ranked := make([]interface{}, len(rerankResp.Results))
	for i, r := range rerankResp.Results {
		if r.Index < len(input.Documents) {
			entry := map[string]interface{}{
				"document":        input.Documents[r.Index],
				"relevance_score": r.RelevanceScore,
				"original_index":  r.Index,
			}
			ranked[i] = entry
		}
	}

	_ = ctx.SetOutputObject(&Output{
		Success:         true,
		RankedDocuments: ranked,
		TotalCount:      len(ranked),
		Duration:        time.Since(start).String(),
	})
	return true, nil
}

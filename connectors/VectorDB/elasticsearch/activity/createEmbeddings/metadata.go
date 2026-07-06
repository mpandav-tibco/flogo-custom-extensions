package createEmbeddings

import (
	"fmt"

	"github.com/project-flogo/core/support/connection"
)

// Settings holds design-time activity configuration.
type Settings struct {
	// Connection is the optional Elasticsearch connector reference.
	// Only required when UseConnectorEmbedding=true.
	Connection connection.Manager `md:"connection"`

	// UseConnectorEmbedding instructs the activity to inherit the embedding
	// provider, API key, and base URL from the VectorDB connector settings.
	// When true, only Model (and optionally Dimensions) need to be set here.
	UseConnectorEmbedding bool `md:"useConnectorEmbedding"`

	// Provider selects the AI embedding service.
	// Allowed: "OpenAI", "Azure OpenAI", "Cohere", "Ollama", "Custom".
	// Defaults to "OpenAI" when empty.
	Provider string `md:"provider"`

	// APIKey is the embedding service API key.
	// Use $property[MY_KEY] to inject from an app property at runtime.
	APIKey string `md:"apiKey"`

	// BaseURL overrides the default provider endpoint.
	// Azure: full deployment URL. Ollama: http://localhost:11434.
	// Leave blank for default OpenAI / Cohere endpoints.
	BaseURL string `md:"baseURL"`

	// Model is the embedding model identifier. Required.
	// Examples: "text-embedding-3-small" (OpenAI), "embed-english-v3.0" (Cohere).
	Model string `md:"model,required"`

	// Dimensions requests a specific output dimension from the embedding model.
	// 0 means use the model's native dimension.
	Dimensions int `md:"dimensions"`

	// TimeoutSeconds caps the embedding API call. Default 30.
	TimeoutSeconds int `md:"timeoutSeconds"`
}

// Input holds the runtime text(s) to embed.
type Input struct {
	// InputText is a single text string to embed.
	// Used when only one embedding is needed (e.g. query embedding).
	InputText string `md:"inputText"`

	// InputTexts is an array of text strings to embed in a single batch call.
	// Takes priority over InputText when both are set.
	InputTexts []interface{} `md:"inputTexts"`
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"inputText":  i.InputText,
		"inputTexts": i.InputTexts,
	}
}

func (i *Input) FromMap(v map[string]interface{}) error {
	if val, ok := v["inputText"]; ok && val != nil {
		i.InputText = fmt.Sprintf("%v", val)
	}
	if val, ok := v["inputTexts"]; ok && val != nil {
		if arr, ok := val.([]interface{}); ok {
			i.InputTexts = arr
		}
	}
	return nil
}

// Output holds the generated embedding(s).
type Output struct {
	// Success indicates whether the operation completed without error.
	Success bool `md:"success"`

	// Embedding is the first (or only) embedding vector as []interface{}{float64,...}.
	// Convenient for single-text input.
	Embedding []interface{} `md:"embedding"`

	// Embeddings is the full list of embedding vectors for batch input.
	// Each element is itself a []interface{}{float64,...}.
	Embeddings []interface{} `md:"embeddings"`

	// Dimensions is the length of each embedding vector.
	Dimensions int `md:"dimensions"`

	// TokensUsed is the number of tokens consumed by the embedding API call.
	TokensUsed int `md:"tokensUsed"`

	// Duration is the wall-clock time taken by the embedding API call.
	Duration string `md:"duration"`

	// Error contains the error message when Success=false.
	Error string `md:"error"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"success":    o.Success,
		"embedding":  o.Embedding,
		"embeddings": o.Embeddings,
		"dimensions": o.Dimensions,
		"tokensUsed": o.TokensUsed,
		"duration":   o.Duration,
		"error":      o.Error,
	}
}

func (o *Output) FromMap(v map[string]interface{}) error {
	if val, ok := v["success"]; ok {
		o.Success, _ = val.(bool)
	}
	if val, ok := v["dimensions"]; ok {
		switch n := val.(type) {
		case int:
			o.Dimensions = n
		case float64:
			o.Dimensions = int(n)
		}
	}
	if val, ok := v["tokensUsed"]; ok {
		switch n := val.(type) {
		case int:
			o.TokensUsed = n
		case float64:
			o.TokensUsed = int(n)
		}
	}
	if val, ok := v["duration"]; ok && val != nil {
		o.Duration = fmt.Sprintf("%v", val)
	}
	if val, ok := v["error"]; ok && val != nil {
		o.Error = fmt.Sprintf("%v", val)
	}
	return nil
}

package createEmbeddings

import (
	"fmt"

	"github.com/project-flogo/core/support/connection"
)

// Settings holds design-time activity configuration.
type Settings struct {
	Connection connection.Manager `md:"connection"`
	// UseConnectorEmbedding inherits provider, API key, and base URL from the
	// VectorDB connector. Requires 'Configure Embedding Provider' on the connection.
	// When true, only Model needs to be set below.
	UseConnectorEmbedding bool   `md:"useConnectorEmbedding"`
	Provider              string `md:"provider"`
	APIKey                string `md:"apiKey"`
	BaseURL               string `md:"baseURL"`
	Model                 string `md:"model,required"`
	Dimensions            int    `md:"dimensions"`
	TimeoutSeconds        int    `md:"timeoutSeconds"`
}

// Input holds runtime data for the activity.
type Input struct {
	InputText  string        `md:"inputText"`
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
	if val, ok := v["inputTexts"]; ok {
		if arr, ok := val.([]interface{}); ok {
			i.InputTexts = arr
		}
	}
	return nil
}

// Output holds the activity result.
type Output struct {
	Success    bool          `md:"success"`
	Embedding  []interface{} `md:"embedding"`  // first embedding (convenience)
	Embeddings []interface{} `md:"embeddings"` // all embeddings
	Dimensions int           `md:"dimensions"`
	TokensUsed int           `md:"tokensUsed"`
	Duration   string        `md:"duration"`
	Error      string        `md:"error"`
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
	return nil
}

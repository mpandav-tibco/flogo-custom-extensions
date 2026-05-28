package vectordbconnector

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSettings_String_RedactsSensitiveFields(t *testing.T) {
	s := &Settings{
		Name:              "test-conn",
		Host:              "db.example.com",
		Port:              6333,
		APIKey:            "sk-api-secret",
		EmbeddingProvider: "openai",
		EmbeddingAPIKey:   "sk-embed-secret",
		ClientKey:         "-----BEGIN EC PRIVATE KEY-----",
		Password:          "s3cr3t-pass",
	}
	str := s.String()
	assert.NotContains(t, str, "sk-api-secret", "APIKey must be redacted")
	assert.NotContains(t, str, "sk-embed-secret", "EmbeddingAPIKey must be redacted")
	assert.NotContains(t, str, "BEGIN EC PRIVATE KEY", "ClientKey must be redacted")
	assert.NotContains(t, str, "s3cr3t-pass", "Password must be redacted")
	assert.Contains(t, str, "db.example.com", "Host must remain visible")
	assert.Contains(t, str, "test-conn", "Name must remain visible")
	assert.Contains(t, str, "[redacted]", "redaction marker must appear")
}

package sse

import (
	"testing"
)

// Test Port Validation
func TestValidatePortRange(t *testing.T) {
	tests := []struct {
		name        string
		port        int
		expectError bool
		errorCode   string
	}{
		{"Valid high port", 8080, false, ""},
		{"Valid HTTPS port", 443, false, ""},
		{"Valid HTTP port", 80, false, ""},
		{"Port too low", 0, true, ErrPortRange},
		{"Port too high", 70000, true, ErrPortRange},
		{"Privileged port", 22, true, ErrPortPrivileged},
		{"Another privileged port", 1000, true, ErrPortPrivileged},
		{"Max valid port", 65535, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePortRange(tt.port)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for port %d, got none", tt.port)
				} else if err.Code != tt.errorCode {
					t.Errorf("Expected error code %s, got %s", tt.errorCode, err.Code)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for port %d: %v", tt.port, err)
				}
			}
		})
	}
}

// Test Path Validation
func TestValidateSSEPath(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		expectError bool
		errorCode   string
	}{
		{"Valid simple path", "/events", false, ""},
		{"Valid nested path", "/api/v1/events", false, ""},
		{"Valid path with underscore", "/sse_events", false, ""},
		{"Valid path with dash", "/sse-events", false, ""},
		{"Empty path", "", true, ErrPathEmpty},
		{"Whitespace path", "   ", true, ErrPathEmpty},
		{"No leading slash", "events", true, ErrPathFormat},
		{"Double slashes", "/events//stream", true, ErrPathSlashes},
		{"Invalid characters", "/events?param=value", true, ErrPathChars},
		{"Invalid special chars", "/events@stream", true, ErrPathChars},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSSEPath(tt.path)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for path '%s', got none", tt.path)
				} else if err.Code != tt.errorCode {
					t.Errorf("Expected error code %s, got %s", tt.errorCode, err.Code)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for path '%s': %v", tt.path, err)
				}
			}
		})
	}
}

// Test Max Connections Validation
func TestValidateMaxConnections(t *testing.T) {
	tests := []struct {
		name        string
		maxConn     int
		expectError bool
		errorCode   string
	}{
		{"Valid connection count", 100, false, ""},
		{"Valid max connection count", 1000, false, ""},
		{"Zero connections", 0, true, ErrMaxConnMin},
		{"Negative connections", -1, true, ErrMaxConnMin},
		{"Too many connections", 150000, true, ErrMaxConnMax},
		{"Boundary max connections", 100000, false, ""},
		{"Just over limit", 100001, true, ErrMaxConnMax},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMaxConnections(tt.maxConn)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for maxConnections %d, got none", tt.maxConn)
				} else if err.Code != tt.errorCode {
					t.Errorf("Expected error code %s, got %s", tt.errorCode, err.Code)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for maxConnections %d: %v", tt.maxConn, err)
				}
			}
		})
	}
}

// Test CORS Origins Validation
func TestValidateCORSOrigins(t *testing.T) {
	tests := []struct {
		name        string
		origins     []string
		expectError bool
		errorCode   string
	}{
		{"Wildcard only", []string{"*"}, false, ""},
		{"Valid HTTP origin", []string{"http://localhost:3000"}, false, ""},
		{"Valid HTTPS origin", []string{"https://example.com"}, false, ""},
		{"Multiple valid origins", []string{"http://localhost:3000", "https://example.com"}, false, ""},
		{"Empty origins", []string{}, true, ErrCORSOrigins},
		{"Invalid format", []string{"localhost:3000"}, true, ErrCORSFormat},
		{"Invalid protocol", []string{"ftp://example.com"}, true, ErrCORSFormat},
		{"Mixed valid and invalid", []string{"https://example.com", "invalid-url"}, true, ErrCORSFormat},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCORSOrigins(tt.origins)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for origins %v, got none", tt.origins)
				} else if err.Code != tt.errorCode {
					t.Errorf("Expected error code %s, got %s", tt.errorCode, err.Code)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for origins %v: %v", tt.origins, err)
				}
			}
		})
	}
}

// Test Event Store TTL Validation
func TestValidateEventStoreTTL(t *testing.T) {
	tests := []struct {
		name        string
		ttl         int
		expectError bool
		errorCode   string
	}{
		{"Valid TTL", 300, false, ""},
		{"Minimum TTL", 1, false, ""},
		{"Maximum TTL", 86400, false, ""},
		{"Zero TTL", 0, true, ErrEventTTLMin},
		{"Negative TTL", -1, true, ErrEventTTLMin},
		{"TTL too large", 100000, true, ErrEventTTLMax},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEventStoreTTL(tt.ttl)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for TTL %d, got none", tt.ttl)
				} else if err.Code != tt.errorCode {
					t.Errorf("Expected error code %s, got %s", tt.errorCode, err.Code)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for TTL %d: %v", tt.ttl, err)
				}
			}
		})
	}
}

// Test Keep Alive Interval Validation
func TestValidateKeepAliveInterval(t *testing.T) {
	tests := []struct {
		name        string
		interval    int
		expectError bool
		errorCode   string
	}{
		{"Valid interval", 30, false, ""},
		{"Minimum interval", 5, false, ""},
		{"Maximum interval", 300, false, ""},
		{"Too small interval", 4, true, ErrKeepAliveMin},
		{"Zero interval", 0, true, ErrKeepAliveMin},
		{"Negative interval", -1, true, ErrKeepAliveMin},
		{"Too large interval", 400, true, ErrKeepAliveMax},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateKeepAliveInterval(tt.interval)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for interval %d, got none", tt.interval)
				} else if err.Code != tt.errorCode {
					t.Errorf("Expected error code %s, got %s", tt.errorCode, err.Code)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for interval %d: %v", tt.interval, err)
				}
			}
		})
	}
}

// Test Comprehensive Settings Validation
func TestValidateSettings(t *testing.T) {
	tests := []struct {
		name         string
		settings     map[string]interface{}
		expectErrors int
	}{
		{
			name: "Valid settings",
			settings: map[string]interface{}{
				"port":              8080,
				"path":              "/events",
				"maxConnections":    1000,
				"enableCORS":        true,
				"corsOrigins":       "*",
				"enableEventStore":  true,
				"eventTTL":          300,
				"keepAliveInterval": 30,
			},
			expectErrors: 0,
		},
		{
			name: "Multiple validation errors",
			settings: map[string]interface{}{
				"port":              0,        // Invalid port
				"path":              "events", // Invalid path format
				"maxConnections":    -1,       // Invalid max connections
				"enableCORS":        true,
				"corsOrigins":       "", // Empty CORS origins
				"enableEventStore":  true,
				"eventTTL":          -1, // Invalid TTL
				"keepAliveInterval": 2,  // Invalid keep alive interval
			},
			expectErrors: 5, // Expecting 5 validation errors
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := ValidateSettings(tt.settings)

			if len(errors) != tt.expectErrors {
				t.Errorf("Expected %d errors, got %d", tt.expectErrors, len(errors))
				for i, err := range errors {
					t.Logf("Error %d: %v", i+1, err)
				}
			}
		})
	}
}

// Test ValidationError struct
func TestValidationError(t *testing.T) {
	err := NewValidationError(ErrPortRange, "port", 70000)

	if err.Code != ErrPortRange {
		t.Errorf("Expected error code %s, got %s", ErrPortRange, err.Code)
	}

	if err.Field != "port" {
		t.Errorf("Expected field 'port', got '%s'", err.Field)
	}

	if err.Value != 70000 {
		t.Errorf("Expected value 70000, got %v", err.Value)
	}

	expectedMessage := ValidationMessages[ErrPortRange]
	if err.Message != expectedMessage {
		t.Errorf("Expected message '%s', got '%s'", expectedMessage, err.Message)
	}

	// Test Error() method
	errorString := err.Error()
	expectedErrorString := "[SSE-1001] Port must be between 1 and 65535 (Field: port, Value: 70000)"
	if errorString != expectedErrorString {
		t.Errorf("Expected error string '%s', got '%s'", expectedErrorString, errorString)
	}
}

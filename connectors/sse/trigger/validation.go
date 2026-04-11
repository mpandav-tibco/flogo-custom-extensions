package sse

import (
	"fmt"
	"regexp"
	"strings"
)

// SSE Trigger Validation Messages and Error Codes
// Following SSH connector pattern for structured error handling

const (
	// Port Validation Errors
	ErrPortRange      = "SSE-1001"
	ErrPortPrivileged = "SSE-1002"

	// Path Validation Errors
	ErrPathEmpty   = "SSE-1003"
	ErrPathFormat  = "SSE-1004"
	ErrPathSlashes = "SSE-1005"
	ErrPathChars   = "SSE-1006"

	// Connection Validation Errors
	ErrMaxConnMin = "SSE-1007"
	ErrMaxConnMax = "SSE-1008"

	// CORS Validation Errors
	ErrCORSOrigins = "SSE-1009"
	ErrCORSFormat  = "SSE-1010"

	// Event Store Validation Errors
	ErrEventTTLMin = "SSE-1012"
	ErrEventTTLMax = "SSE-1013"

	// Event Store Size Validation Errors
	ErrEventStoreSizeMin = "SSE-1022"
	ErrEventStoreSizeMax = "SSE-1023"

	// Keep Alive Validation Errors
	ErrKeepAliveMin = "SSE-1014"
	ErrKeepAliveMax = "SSE-1015"

	// Connection Setup Errors
	ErrDuplicateConnection = "SSE-CONNECTION-1001"
	ErrReservedPort        = "SSE-CONNECTION-1002"
	ErrPortInUse           = "SSE-CONNECTION-1003"
	ErrBindFailed          = "SSE-CONNECTION-1004"

	// Runtime Errors
	ErrServerStart      = "SSE-RUNTIME-1001"
	ErrServerStop       = "SSE-RUNTIME-1002"
	ErrConnectionFailed = "SSE-RUNTIME-1003"
	ErrEventSendFailed  = "SSE-RUNTIME-1004"
)

// ValidationMessages provides human-readable messages for error codes
var ValidationMessages = map[string]string{
	// Port Validation Messages
	ErrPortRange:      "Port must be between 1 and 65535",
	ErrPortPrivileged: "Ports below 1024 typically require root privileges",

	// Path Validation Messages
	ErrPathEmpty:   "SSE endpoint path cannot be empty",
	ErrPathFormat:  "SSE endpoint path must start with '/'",
	ErrPathSlashes: "SSE endpoint path cannot contain consecutive slashes",
	ErrPathChars:   "SSE endpoint path contains invalid characters. Use only a-z, A-Z, 0-9, /, _, -",

	// Connection Validation Messages
	ErrMaxConnMin: "Maximum connections must be at least 1",
	ErrMaxConnMax: "Maximum connections should not exceed 100,000 for performance reasons",

	// CORS Validation Messages
	ErrCORSOrigins: "CORS origins must be specified when CORS is enabled",
	ErrCORSFormat:  "Invalid CORS origin format. Use format: http(s)://domain:port or '*' for all origins",

	// Event Store Validation Messages
	ErrEventTTLMin: "Event store TTL must be at least 1 second",
	ErrEventTTLMax: "Event store TTL should not exceed 24 hours (86400 seconds) for memory efficiency",

	// Event Store Size Validation Messages
	ErrEventStoreSizeMin: "Event store size must be at least 1",
	ErrEventStoreSizeMax: "Event store size should not exceed 10,000 for memory efficiency",

	// Keep Alive Validation Messages
	ErrKeepAliveMin: "Keep-alive interval must be at least 5 seconds",
	ErrKeepAliveMax: "Keep-alive interval should not exceed 300 seconds (5 minutes)",

	// Connection Setup Messages
	ErrDuplicateConnection: "SSE trigger with the same port and path combination already exists",
	ErrReservedPort:        "Port is reserved for system services. Please use a different port",
	ErrPortInUse:           "Port is already in use by another service",
	ErrBindFailed:          "Failed to bind to the specified port and address",

	// Runtime Messages
	ErrServerStart:      "Failed to start SSE server",
	ErrServerStop:       "Failed to gracefully stop SSE server",
	ErrConnectionFailed: "Failed to establish SSE connection with client",
	ErrEventSendFailed:  "Failed to send event to SSE client",
}

// ValidationError represents a structured validation error
type ValidationError struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Field   string      `json:"field,omitempty"`
	Value   interface{} `json:"value,omitempty"`
}

// NewValidationError creates a new validation error
func NewValidationError(code, field string, value interface{}) *ValidationError {
	message, exists := ValidationMessages[code]
	if !exists {
		message = "Unknown validation error"
	}

	return &ValidationError{
		Code:    code,
		Message: message,
		Field:   field,
		Value:   value,
	}
}

// Error implements the error interface
func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("[%s] %s (Field: %s, Value: %v)", e.Code, e.Message, e.Field, e.Value)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// ValidatePortRange validates if port is within acceptable range
func ValidatePortRange(port int) *ValidationError {
	if port < 1 || port > 65535 {
		return NewValidationError(ErrPortRange, "port", port)
	}
	if port < 1024 && port != 80 && port != 443 {
		return NewValidationError(ErrPortPrivileged, "port", port)
	}
	return nil
}

// ValidateSSEPath validates SSE endpoint path format
func ValidateSSEPath(path string) *ValidationError {
	if strings.TrimSpace(path) == "" {
		return NewValidationError(ErrPathEmpty, "path", path)
	}
	if !strings.HasPrefix(path, "/") {
		return NewValidationError(ErrPathFormat, "path", path)
	}
	if strings.Contains(path, "//") {
		return NewValidationError(ErrPathSlashes, "path", path)
	}

	// Allow only alphanumeric, /, _, - characters
	validPathRegex := regexp.MustCompile(`^[a-zA-Z0-9\/_-]+$`)
	if !validPathRegex.MatchString(path) {
		return NewValidationError(ErrPathChars, "path", path)
	}

	return nil
}

// ValidateMaxConnections validates maximum connection limit
func ValidateMaxConnections(maxConn int) *ValidationError {
	if maxConn < 1 {
		return NewValidationError(ErrMaxConnMin, "maxConnections", maxConn)
	}
	if maxConn > 100000 {
		return NewValidationError(ErrMaxConnMax, "maxConnections", maxConn)
	}
	return nil
}

// ValidateCORSOrigins validates CORS origin format
func ValidateCORSOrigins(origins []string) *ValidationError {
	if len(origins) == 0 {
		return NewValidationError(ErrCORSOrigins, "corsOrigins", origins)
	}

	urlPattern := regexp.MustCompile(`^https?://[^\s/$.?#].[^\s]*$`)
	localhostPattern := regexp.MustCompile(`^https?://(localhost|127\.0\.0\.1)(:[0-9]+)?$`)

	for _, origin := range origins {
		if origin != "*" && !urlPattern.MatchString(origin) && !localhostPattern.MatchString(origin) {
			return NewValidationError(ErrCORSFormat, "corsOrigins", origin)
		}
	}

	return nil
}

// ValidateEventStoreTTL validates event store TTL range
func ValidateEventStoreTTL(ttl int) *ValidationError {
	if ttl < 1 {
		return NewValidationError(ErrEventTTLMin, "eventStoreTTL", ttl)
	}
	if ttl > 86400 { // 24 hours
		return NewValidationError(ErrEventTTLMax, "eventStoreTTL", ttl)
	}
	return nil
}

// ValidateEventStoreSize validates event store size range
func ValidateEventStoreSize(size int) *ValidationError {
	if size < 1 {
		return NewValidationError(ErrEventStoreSizeMin, "eventStoreSize", size)
	}
	if size > 10000 { // 10,000 events
		return NewValidationError(ErrEventStoreSizeMax, "eventStoreSize", size)
	}
	return nil
}

// ValidateKeepAliveInterval validates keep-alive interval range
func ValidateKeepAliveInterval(interval int) *ValidationError {
	if interval < 5 {
		return NewValidationError(ErrKeepAliveMin, "keepAliveInterval", interval)
	}
	if interval > 300 { // 5 minutes
		return NewValidationError(ErrKeepAliveMax, "keepAliveInterval", interval)
	}
	return nil
}

// ValidateSettings performs comprehensive validation of all SSE trigger settings
func ValidateSettings(settings map[string]interface{}) []*ValidationError {
	var errors []*ValidationError

	// Port validation
	if port, ok := settings["port"].(int); ok {
		if err := ValidatePortRange(port); err != nil {
			errors = append(errors, err)
		}
	}

	// Path validation
	if path, ok := settings["path"].(string); ok {
		if err := ValidateSSEPath(path); err != nil {
			errors = append(errors, err)
		}
	}

	// Max connections validation
	if maxConn, ok := settings["maxConnections"].(int); ok {
		if err := ValidateMaxConnections(maxConn); err != nil {
			errors = append(errors, err)
		}
	}

	// CORS validation
	if corsEnabled, ok := settings["enableCORS"].(bool); ok && corsEnabled {
		if originsStr, ok := settings["corsOrigins"].(string); ok {
			if originsStr == "" {
				errors = append(errors, NewValidationError(ErrCORSOrigins, "corsOrigins", originsStr))
			} else if originsStr != "*" {
				// Split by comma and validate each origin
				origins := strings.Split(originsStr, ",")
				for i, origin := range origins {
					origins[i] = strings.TrimSpace(origin)
				}
				if err := ValidateCORSOrigins(origins); err != nil {
					errors = append(errors, err)
				}
			}
		}
	}

	// Event store validation
	if storeEnabled, ok := settings["enableEventStore"].(bool); ok && storeEnabled {
		if ttl, ok := settings["eventTTL"].(int); ok {
			if err := ValidateEventStoreTTL(ttl); err != nil {
				errors = append(errors, err)
			}
		}
		if size, ok := settings["eventStoreSize"].(int); ok {
			if err := ValidateEventStoreSize(size); err != nil {
				errors = append(errors, err)
			}
		}
	}

	// Keep alive validation
	if keepAliveEnabled, ok := settings["keepAliveEnabled"].(bool); ok && keepAliveEnabled {
		if interval, ok := settings["keepAliveInterval"].(int); ok {
			if err := ValidateKeepAliveInterval(interval); err != nil {
				errors = append(errors, err)
			}
		}
	}

	return errors
}

package ssesend

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/milindpandav/flogo-extensions/sse"
)

// SimpleSSEServer implements SSEServerInterface for testing
type SimpleSSEServer struct {
	events            []*sse.SSEEventData
	connections       []sse.ConnectionInfo
	lastConnectionID  string
	lastTopic         string
	shouldReturnError bool
}

func (s *SimpleSSEServer) BroadcastEvent(event *sse.SSEEventData) error {
	if s.shouldReturnError {
		return fmt.Errorf("test error")
	}
	s.events = append(s.events, event)
	return nil
}

func (s *SimpleSSEServer) BroadcastEventToTopic(topic string, event *sse.SSEEventData) error {
	if s.shouldReturnError {
		return fmt.Errorf("test error")
	}
	s.lastTopic = topic
	s.events = append(s.events, event)
	return nil
}

func (s *SimpleSSEServer) SendEventToConnection(connectionID string, event *sse.SSEEventData) error {
	if s.shouldReturnError {
		return fmt.Errorf("test error")
	}
	s.lastConnectionID = connectionID
	s.events = append(s.events, event)
	return nil
}

func (s *SimpleSSEServer) GetActiveConnections() []sse.ConnectionInfo {
	return s.connections
}

func TestSSESendActivity_DataFormatting_JSON(t *testing.T) {
	act := &Activity{}

	testData := map[string]interface{}{
		"name":   "John",
		"age":    30,
		"active": true,
		"scores": []int{85, 92, 78},
	}

	result, err := act.formatAsJSON(testData)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Verify it's valid JSON
	var parsed map[string]interface{}
	err = json.Unmarshal([]byte(result), &parsed)
	if err != nil {
		t.Errorf("Result is not valid JSON: %v", err)
	}

	if parsed["name"] != "John" {
		t.Errorf("Expected name to be 'John', got: %v", parsed["name"])
	}
}

func TestSSESendActivity_DataFormatting_String(t *testing.T) {
	act := &Activity{}

	tests := []struct {
		input    interface{}
		expected string
	}{
		{"hello", "hello"},
		{42, "42"},
		{true, "true"},
		{nil, ""},
	}

	for _, test := range tests {
		result := act.formatAsString(test.input)
		if result != test.expected {
			t.Errorf("For input %v, expected %s, got %s", test.input, test.expected, result)
		}
	}
}

func TestSSESendActivity_DataFormatting_Auto(t *testing.T) {
	act := &Activity{}

	// String should remain string
	result, err := act.autoFormatData("hello world")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if result != "hello world" {
		t.Errorf("Expected 'hello world', got: %s", result)
	}

	// Complex object should be JSON
	complexData := map[string]interface{}{"key": "value"}
	result, err = act.autoFormatData(complexData)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if result != `{"key":"value"}` {
		t.Errorf("Expected JSON string, got: %s", result)
	}

	// Simple types should be string
	result, err = act.autoFormatData(42)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if result != "42" {
		t.Errorf("Expected '42', got: %s", result)
	}
}

func TestSSESendActivity_ParseTarget(t *testing.T) {
	act := &Activity{}

	tests := []struct {
		input        string
		expectedType TargetType
		expectedID   string
		shouldError  bool
	}{
		{"all", TargetAll, "", false},
		{"", TargetAll, "", false},
		{"connection:conn123", TargetConnection, "conn123", false},
		{"topic:chat-room", TargetTopic, "chat-room", false},
		{"connection:", TargetConnection, "", true}, // Empty connection ID
		{"topic:", TargetTopic, "", true},           // Empty topic
		{"invalid:format", TargetAll, "", true},     // Invalid prefix
	}

	for _, test := range tests {
		target, err := act.parseTarget(test.input)

		if test.shouldError {
			if err == nil {
				t.Errorf("Expected error for input: %s", test.input)
			}
		} else {
			if err != nil {
				t.Errorf("Unexpected error for input: %s, error: %v", test.input, err)
			} else {
				if target.Type != test.expectedType {
					t.Errorf("For input %s, expected type %v, got %v", test.input, test.expectedType, target.Type)
				}
				if target.Identifier != test.expectedID {
					t.Errorf("For input %s, expected ID %s, got %s", test.input, test.expectedID, target.Identifier)
				}
			}
		}
	}
}

func TestSSESendActivity_InputValidation(t *testing.T) {
	act := &Activity{
		settings: &Settings{
			Retry:     3000,
			EventType: "message",
		},
	}

	// Valid input
	validInput := &Input{
		Target:           "all",
		Data:             "test data",
		Format:           "auto",
		EnableValidation: true,
	}
	err := act.validateInput(validInput)
	if err != nil {
		t.Errorf("Expected no error for valid input, got: %v", err)
	}

	// Invalid target
	invalidInput := &Input{
		Target:           "",
		Data:             "test data",
		EnableValidation: true,
	}
	err = act.validateInput(invalidInput)
	if err == nil {
		t.Errorf("Expected error for empty target")
	}

	// Invalid data
	invalidInput = &Input{
		Target:           "all",
		Data:             nil,
		EnableValidation: true,
	}
	err = act.validateInput(invalidInput)
	if err == nil {
		t.Errorf("Expected error for nil data")
	}

	// Invalid retry - test with negative retry in settings
	actWithNegativeRetry := &Activity{
		settings: &Settings{
			Retry:     -1,
			EventType: "message",
		},
	}
	invalidInputRetry := &Input{
		Target:           "all",
		Data:             "test",
		EnableValidation: true,
	}
	err = actWithNegativeRetry.validateInput(invalidInputRetry)
	if err == nil {
		t.Errorf("Expected error for negative retry")
	}
}

func TestSSESendActivity_CreateSSEEvent(t *testing.T) {
	act := &Activity{
		settings: &Settings{
			Retry:     5000,
			EventType: "default",
		},
	}

	input := &Input{
		EventID:   "test-123",
		EventType: "message",
		Data:      "Hello World",
		Format:    "string",
	}

	event, err := act.createSSEEvent(input)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if event.ID != "test-123" {
		t.Errorf("Expected ID 'test-123', got: %s", event.ID)
	}

	if event.Event != "message" {
		t.Errorf("Expected event type 'message', got: %s", event.Event)
	}

	if event.Data != "Hello World" {
		t.Errorf("Expected data 'Hello World', got: %s", event.Data)
	}

	if event.Retry != 5000 {
		t.Errorf("Expected retry 5000, got: %d", event.Retry)
	}
}

func TestSSESendActivity_IsComplexType(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected bool
	}{
		{map[string]interface{}{"key": "value"}, true},
		{[]interface{}{1, 2, 3}, true},
		{[]string{"a", "b", "c"}, true},
		{"simple string", false},
		{42, false},
		{true, false},
		{nil, false},
	}

	for _, test := range tests {
		result := isComplexType(test.input)
		if result != test.expected {
			t.Errorf("For input %v, expected %v, got %v", test.input, test.expected, result)
		}
	}
}

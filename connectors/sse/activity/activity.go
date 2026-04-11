package ssesend

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/log"

	// Import shared SSE types and registry from parent package
	"github.com/milindpandav/flogo-extensions/sse"
)

// Activity represents the SSE Send activity
// Uses shared SSE types and registry from parent sse package
type Activity struct {
	settings *Settings
	logger   log.Logger
}

// Metadata returns the activity metadata
func (a *Activity) Metadata() *activity.Metadata {
	return activityMd
}

// init initializes the activity
func init() {
	activity.Register(&Activity{}, New)
}

// New creates a new SSE Send activity instance
func New(ctx activity.InitContext) (activity.Activity, error) {
	settings := &Settings{}
	err := metadata.MapToStruct(ctx.Settings(), settings, true)
	if err != nil {
		return nil, err
	}

	logger := ctx.Logger()

	act := &Activity{
		settings: settings,
		logger:   logger,
	}

	logger.Info("SSE Send Activity initialized")
	return act, nil
}

// Eval executes the SSE Send activity
func (a *Activity) Eval(ctx activity.Context) (done bool, err error) {
	input := &Input{}
	err = ctx.GetInputObject(input)
	if err != nil {
		return false, fmt.Errorf("failed to get input: %v", err)
	}

	a.logger.Debugf("SSE Send Activity received input: data=%v, eventType=%s, target=%s, format=%s", 
		input.Data, input.EventType, input.Target, input.Format)

	// Auto-build target if not explicitly provided
	if input.Target == "" {
		input.Target = a.buildTargetFromInputs(input)
		a.logger.Debugf("Auto-built target from inputs: %s", input.Target)
	}

	// Validate inputs if enabled
	if input.EnableValidation {
		if err := a.validateInput(input); err != nil {
			return a.setErrorOutput(ctx, fmt.Sprintf("Input validation failed: %v", err))
		}
	}

	// Parse target
	target, err := a.parseTarget(input.Target)
	if err != nil {
		return a.setErrorOutput(ctx, fmt.Sprintf("Invalid target format: %v", err))
	}
	a.logger.Debugf("Parsed target: type=%s, identifier=%s", target.Type, target.Identifier)

	// Get SSE server
	server, err := a.getSSEServer()
	if err != nil {
		return a.setErrorOutput(ctx, fmt.Sprintf("SSE server not available: %v", err))
	}
	a.logger.Debugf("Successfully found SSE server: %s", a.settings.SSEServerRef)

	// Create SSE event
	event, err := a.createSSEEvent(input)
	if err != nil {
		return a.setErrorOutput(ctx, fmt.Sprintf("Failed to create SSE event: %v", err))
	}
	a.logger.Debugf("Created SSE event: id=%s, type=%s, dataLength=%d", 
		event.ID, event.Event, len(event.Data))

	// Send event based on target type
	sentCount, err := a.sendEvent(server, target, event)
	if err != nil {
		return a.setErrorOutput(ctx, fmt.Sprintf("Failed to send event: %v", err))
	}
	a.logger.Infof("Successfully sent SSE event (id=%s) to %d clients via %s target", 
		event.ID, sentCount, target.Type)

	// Set success output
	output := &Output{
		Success:   true,
		SentCount: sentCount,
		EventID:   event.ID,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	err = ctx.SetOutputObject(output)
	if err != nil {
		a.logger.Errorf("Failed to set output: %v", err)
		return false, err
	}

	a.logger.Debugf("Successfully sent SSE event to %d clients", sentCount)
	return true, nil
}

// buildTargetFromInputs automatically builds target from connectionId or topic inputs and settings
func (a *Activity) buildTargetFromInputs(input *Input) string {
	// Priority: connectionId > input topic > setting topic > default to "all"
	if input.ConnectionID != "" {
		return fmt.Sprintf("connection:%s", input.ConnectionID)
	}

	if input.Topic != "" {
		return fmt.Sprintf("topic:%s", input.Topic)
	}

	if a.settings.Topic != "" {
		return fmt.Sprintf("topic:%s", a.settings.Topic)
	}

	// Default fallback
	return "all"
} // validateInput validates the activity input and settings
func (a *Activity) validateInput(input *Input) error {
	// Target can be empty if connectionId or topic is provided (input or settings)
	if input.Target == "" && input.ConnectionID == "" && input.Topic == "" && a.settings.Topic == "" {
		return fmt.Errorf("either target, connectionId, or topic (input or setting) must be provided")
	}

	if input.Data == nil {
		return fmt.Errorf("data cannot be nil")
	}

	// Validate format
	if input.Format != "" && input.Format != "json" && input.Format != "string" && input.Format != "auto" {
		return fmt.Errorf("invalid format: %s. Must be 'json', 'string', or 'auto'", input.Format)
	}

	// Validate retry value from settings
	if a.settings.Retry < 0 {
		return fmt.Errorf("retry cannot be negative")
	}

	// Validate eventType (use input override if provided, otherwise setting)
	eventType := input.EventType
	if eventType == "" {
		eventType = a.settings.EventType
	}
	if eventType != "" {
		validTypes := []string{"message", "notification", "update", "alert", "status", "data", "event", "error", "warning", "info", "heartbeat", "custom"}
		isValid := false
		for _, validType := range validTypes {
			if eventType == validType {
				isValid = true
				break
			}
		}
		if !isValid {
			return fmt.Errorf("invalid event type: %s", eventType)
		}
	}

	// Validate event ID format if provided
	if input.EventID != "" {
		if len(input.EventID) > 255 {
			return fmt.Errorf("event ID too long (max 255 characters)")
		}
		if strings.ContainsAny(input.EventID, "\n\r") {
			return fmt.Errorf("event ID cannot contain newlines")
		}
	}

	// Validate event type format if provided
	if input.EventType != "" {
		if len(input.EventType) > 255 {
			return fmt.Errorf("event type too long (max 255 characters)")
		}
		if strings.ContainsAny(input.EventType, "\n\r") {
			return fmt.Errorf("event type cannot contain newlines")
		}
	}

	return nil
}

// parseTarget parses the target string into a structured target
func (a *Activity) parseTarget(targetStr string) (*ParsedTarget, error) {
	if targetStr == "" || targetStr == "all" {
		return &ParsedTarget{Type: TargetAll}, nil
	}

	if strings.HasPrefix(targetStr, "connection:") {
		connID := strings.TrimPrefix(targetStr, "connection:")
		if connID == "" {
			return nil, fmt.Errorf("connection ID cannot be empty")
		}
		return &ParsedTarget{Type: TargetConnection, Identifier: connID}, nil
	}

	if strings.HasPrefix(targetStr, "topic:") {
		topic := strings.TrimPrefix(targetStr, "topic:")
		if topic == "" {
			return nil, fmt.Errorf("topic name cannot be empty")
		}
		return &ParsedTarget{Type: TargetTopic, Identifier: topic}, nil
	}

	return nil, fmt.Errorf("invalid target format. Use 'all', 'connection:ID', or 'topic:NAME'")
}

// getSSEServer gets the appropriate SSE server instance
func (a *Activity) getSSEServer() (sse.SSEServerInterface, error) {
	// Use the server reference from settings
	serverRef := a.settings.SSEServerRef
	if serverRef == "" {
		serverRef = "default"
	}

	// Try to get the specified server using the shared registry
	server, exists := sse.GetSSEServer(serverRef)
	if !exists {
		// List available servers for debugging
		availableServers := sse.ListRegisteredServers()
		return nil, fmt.Errorf("SSE server '%s' not found. Available servers: %v. Make sure the SSE trigger with name '%s' is running",
			serverRef, availableServers, serverRef)
	}

	return server, nil
}

// createSSEEvent creates an SSE event from input and settings
func (a *Activity) createSSEEvent(input *Input) (*sse.SSEEventData, error) {
	// Generate event ID if not provided
	eventID := input.EventID
	if eventID == "" {
		eventID = fmt.Sprintf("evt_%d_%d", time.Now().UnixNano(), time.Now().Unix()%1000)
	}

	// Use input eventType if provided, otherwise use setting (input takes precedence)
	eventType := input.EventType
	if eventType == "" {
		eventType = a.settings.EventType
	}
	if eventType == "" {
		eventType = "message" // Final fallback to default
	}

	// Use setting retry value
	retry := a.settings.Retry

	// Format data based on specified format
	var dataStr string
	var err error

	switch input.Format {
	case "json":
		dataStr, err = a.formatAsJSON(input.Data)
	case "string":
		dataStr = a.formatAsString(input.Data)
	case "auto", "":
		dataStr, err = a.autoFormatData(input.Data)
	default:
		return nil, fmt.Errorf("unsupported format: %s", input.Format)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to format data: %v", err)
	}

	event := &sse.SSEEventData{
		ID:    eventID,
		Event: eventType,
		Data:  dataStr,
		Retry: retry,
	}

	return event, nil
}

// formatAsJSON formats data as JSON string
func (a *Activity) formatAsJSON(data interface{}) (string, error) {
	if data == nil {
		return "null", nil
	}

	if str, ok := data.(string); ok {
		// If it's already a string, try to parse it as JSON to validate
		var temp interface{}
		if json.Unmarshal([]byte(str), &temp) == nil {
			return str, nil
		}
		// If not valid JSON, encode the string
		bytes, err := json.Marshal(str)
		return string(bytes), err
	}

	bytes, err := json.Marshal(data)
	return string(bytes), err
}

// formatAsString formats data as string
func (a *Activity) formatAsString(data interface{}) string {
	if data == nil {
		return ""
	}

	if str, ok := data.(string); ok {
		return str
	}

	return fmt.Sprintf("%v", data)
}

// autoFormatData automatically determines the best format for data
func (a *Activity) autoFormatData(data interface{}) (string, error) {
	if data == nil {
		return "null", nil
	}

	// If it's a string, return as-is
	if str, ok := data.(string); ok {
		return str, nil
	}

	// For complex types, use JSON
	if isComplexType(data) {
		return a.formatAsJSON(data)
	}

	// For simple types, use string representation
	return a.formatAsString(data), nil
}

// isComplexType checks if data is a complex type that should be JSON encoded
func isComplexType(data interface{}) bool {
	switch data.(type) {
	case map[string]interface{}, []interface{}, map[interface{}]interface{}:
		return true
	case []string, []int, []float64, []bool:
		return true
	default:
		return false
	}
}

// sendEvent sends the event to the appropriate target
func (a *Activity) sendEvent(server sse.SSEServerInterface, target *ParsedTarget, event *sse.SSEEventData) (int, error) {
	switch target.Type {
	case TargetAll:
		a.logger.Debugf("Broadcasting event to all connected clients")
		err := server.BroadcastEvent(event)
		if err != nil {
			return 0, err
		}
		// Count active connections
		connections := server.GetActiveConnections()
		a.logger.Debugf("Broadcast sent to %d active connections", len(connections))
		return len(connections), nil

	case TargetConnection:
		a.logger.Debugf("Sending event to specific connection: %s", target.Identifier)
		err := server.SendEventToConnection(target.Identifier, event)
		if err != nil {
			a.logger.Errorf("Failed to send to connection %s: %v", target.Identifier, err)
			return 0, err
		}
		return 1, nil

	case TargetTopic:
		a.logger.Debugf("Broadcasting event to topic: %s", target.Identifier)
		err := server.BroadcastEventToTopic(target.Identifier, event)
		if err != nil {
			return 0, err
		}
		// Count connections subscribed to this topic
		connections := server.GetActiveConnections()
		count := 0
		for _, conn := range connections {
			if conn.Topic == target.Identifier || conn.Topic == "" {
				count++
			}
		}
		a.logger.Debugf("Topic broadcast sent to %d matching connections", count)
		return count, nil

	default:
		return 0, fmt.Errorf("unsupported target type")
	}
}

// setErrorOutput sets error output and returns appropriate values
func (a *Activity) setErrorOutput(ctx activity.Context, errorMsg string) (bool, error) {
	a.logger.Error(errorMsg)

	output := &Output{
		Success:   false,
		SentCount: 0,
		Error:     errorMsg,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	err := ctx.SetOutputObject(output)
	if err != nil {
		a.logger.Errorf("Failed to set error output: %v", err)
		return false, err
	}

	return true, nil
}

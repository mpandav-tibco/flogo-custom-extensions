package dynamiclog

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/coerce"
	"github.com/project-flogo/core/engine"
)

// Constants for identifying settings and inputs
const (
	sLogLevel        = "logLevel"
	sLogAsJson       = "logAsJson" // New constant for the setting
	sIncludeFlowInfo = "includeFlowInfo"
	ivLogObject      = "logObject"
)

// activityMd is the metadata for the activity.
var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})

// Activity is the structure for your activity.
type Activity struct {
	logLevel        string
	logAsJson       bool // New field to hold the setting value
	includeFlowInfo bool
}

func init() {
	_ = activity.Register(&Activity{}, New)
}

// Metadata returns the activity's metadata.
func (a *Activity) Metadata() *activity.Metadata {
	return activityMd
}

// New creates a new instance of the Activity.
func New(ctx activity.InitContext) (activity.Activity, error) {
	s := &Settings{}
	err := s.FromMap(ctx.Settings())
	if err != nil {
		return nil, err
	}

	act := &Activity{
		logLevel:        s.LogLevel,
		logAsJson:       s.LogAsJson, // Store the setting value
		includeFlowInfo: s.IncludeFlowInfo,
	}
	return act, nil
}

// Eval executes the main logic of the Activity.
func (a *Activity) Eval(ctx activity.Context) (done bool, err error) {
	logger := ctx.Logger()

	// --- 1. Get Inputs ---
	input := &Input{}
	err = ctx.GetInputObject(input)
	if err != nil {
		return false, err
	}

	if input.LogObject == nil {
		logger.Warn("Input 'logObject' is empty. Nothing to log.")
		return true, nil
	}

	// --- 2. Build the Structured Log Entry ---
	finalLogEntry := make(map[string]interface{})

	if a.includeFlowInfo {
		addStandardFields(finalLogEntry, ctx)
	}

	for key, value := range input.LogObject {
		finalLogEntry[key] = value
	}

	var finalLogMessage string

	// --- 3. Format the Log Message based on the setting ---
	if a.logAsJson {
		// Ensure essential fields for JSON format exist
		if _, ok := finalLogEntry["@timestamp"]; !ok {
			finalLogEntry["@timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
		}
		if _, ok := finalLogEntry["log.level"]; !ok {
			finalLogEntry["log.level"] = strings.ToLower(a.logLevel)
		}
		// Marshal the map to a JSON string
		logBytes, err := json.Marshal(finalLogEntry)
		if err != nil {
			logger.Errorf("Failed to marshal structured log entry to JSON: %v", err)
			return false, err
		}
		finalLogMessage = string(logBytes)
	} else {
		// Format the map into a simple, readable key=value string
		finalLogMessage = formatAsSimpleString(finalLogEntry)
	}

	// --- 4. Log the final message ---
	switch strings.ToUpper(a.logLevel) {
	case "INFO":
		logger.Info(finalLogMessage)
	case "DEBUG":
		logger.Debug(finalLogMessage)
	case "ERROR":
		logger.Error(finalLogMessage)
	case "WARN":
		logger.Warn(finalLogMessage)
	default:
		logger.Infof("Unknown log level '%s', defaulting to INFO. Message: %s", a.logLevel, finalLogMessage)
	}

	return true, nil
}

// formatAsSimpleString converts a map to a sorted, readable key="value" string.
func formatAsSimpleString(logEntry map[string]interface{}) string {
	// Prioritize the 'message' field if it exists
	primaryMessage := ""
	if msg, ok := logEntry["message"]; ok {
		primaryMessage = fmt.Sprintf("%v", msg)
		delete(logEntry, "message") // Remove from map to avoid duplication
	}

	var parts []string
	// Sort keys for consistent log output
	keys := make([]string, 0, len(logEntry))
	for k := range logEntry {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=\"%v\"", k, logEntry[k]))
	}

	if primaryMessage != "" {
		if len(parts) > 0 {
			return fmt.Sprintf("%s | %s", primaryMessage, strings.Join(parts, " "))
		}
		return primaryMessage
	}
	return strings.Join(parts, " ")
}

// addStandardFields populates a map with ECS-compliant context.
func addStandardFields(logEntry map[string]interface{}, ctx activity.Context) {
	logEntry["@timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	logEntry["ecs.version"] = "8.4" // Example ECS version

	// Service and Process details
	logEntry["service.name"] = engine.GetAppName()
	logEntry["service.version"] = engine.GetAppVersion()
	logEntry["process.name"] = ctx.ActivityHost().Name()

	// Event details
	logEntry["event.action"] = ctx.Name()
	logEntry["event.kind"] = "event"
}

// --- Supporting Structs ---

type Settings struct {
	LogLevel        string `md:"logLevel"`
	LogAsJson       bool   `md:"logAsJson"` // New field for the setting
	IncludeFlowInfo bool   `md:"includeFlowInfo"`
}

// FromMap populates the struct from a map.
func (s *Settings) FromMap(values map[string]interface{}) error {
	if values == nil {
		s.LogLevel = "INFO"
		s.IncludeFlowInfo = true
		s.LogAsJson = false
		return nil
	}

	var err error

	if val, ok := values[sLogLevel]; ok && val != nil {
		s.LogLevel, err = coerce.ToString(val)
		if err != nil {
			return err
		}
	}
	if s.LogLevel == "" {
		s.LogLevel = "INFO"
	}

	if val, ok := values[sIncludeFlowInfo]; ok && val != nil {
		s.IncludeFlowInfo, err = coerce.ToBool(val)
		if err != nil {
			s.IncludeFlowInfo = true // Default on error
		}
	} else {
		s.IncludeFlowInfo = true // Default if not present
	}

	// Coerce the new logAsJson setting
	if val, ok := values[sLogAsJson]; ok && val != nil {
		s.LogAsJson, err = coerce.ToBool(val)
		if err != nil {
			s.LogAsJson = false // Default on error
		}
	} else {
		s.LogAsJson = false // Default if not present
	}

	return nil
}

type Input struct {
	LogObject map[string]interface{} `md:"logObject"`
}

// FromMap populates the struct from the activity's inputs.
func (i *Input) FromMap(values map[string]interface{}) error {
	if values == nil {
		return nil
	}

	var err error
	i.LogObject, err = coerce.ToObject(values[ivLogObject])
	if err != nil {
		return err
	}
	return nil
}

// ToMap converts the struct to a map.
func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		ivLogObject: i.LogObject,
	}
}

// Output is an empty struct since this activity has no outputs.
type Output struct{}

// ToMap is required by the data.StructValue interface.
func (o *Output) ToMap() map[string]interface{} {
	return nil
}

// FromMap is required by the data.StructValue interface.
func (o *Output) FromMap(values map[string]interface{}) error {
	return nil
}

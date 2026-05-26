package ssesend

import (
	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/coerce"
)

var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})

// Settings hold activity-level configuration set at design time.
type Settings struct {
	// SSEServerRef is the registry name of the SSE trigger to send events to.
	// Use "default" for single-server setups (the default).
	SSEServerRef string `md:"sseServerRef"`
	// Topic is the default topic/channel for events (overridable per input).
	Topic string `md:"topic"`
	// EventType is the default SSE event type (overridable per input).
	EventType string `md:"eventType"`
	// Retry is the SSE client reconnection timeout in milliseconds.
	Retry int `md:"retry"`
}

// TargetType distinguishes how an event is dispatched.
type TargetType int

const (
	TargetAll        TargetType = iota // broadcast to all connections
	TargetConnection                   // send to one specific connection ID
	TargetTopic                        // broadcast to connections on a topic
)

// ParsedTarget holds the decoded routing directive.
type ParsedTarget struct {
	Type       TargetType
	Identifier string
}

// Input holds the per-invocation inputs for the SSE Send activity.
type Input struct {
	// ConnectionID targets a specific SSE connection (used when target is omitted).
	ConnectionID string `md:"connectionId"`
	// Target explicitly sets the dispatch target: "all", "connection:ID", "topic:NAME".
	Target string `md:"target"`
	// EventID is an optional unique event identifier.
	EventID string `md:"eventId"`
	// Topic overrides the default topic setting.
	Topic string `md:"topic"`
	// EventType overrides the default event type setting.
	EventType string `md:"eventType"`
	// Data is the event payload (object or string).
	Data interface{} `md:"data"`
	// Format controls data serialisation: "json", "string", or "auto" (default).
	Format string `md:"format"`
	// EnableValidation enables input validation before sending.
	EnableValidation bool `md:"enableValidation"`
}

// ToMap serialises Input for the Flogo engine.
func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"connectionId":     i.ConnectionID,
		"target":           i.Target,
		"eventId":          i.EventID,
		"topic":            i.Topic,
		"eventType":        i.EventType,
		"data":             i.Data,
		"format":           i.Format,
		"enableValidation": i.EnableValidation,
	}
}

// FromMap deserialises Input from the Flogo engine map.
func (i *Input) FromMap(values map[string]interface{}) error {
	var err error
	if i.ConnectionID, err = coerce.ToString(values["connectionId"]); err != nil {
		return err
	}
	if i.Target, err = coerce.ToString(values["target"]); err != nil {
		return err
	}
	if i.EventID, err = coerce.ToString(values["eventId"]); err != nil {
		return err
	}
	if i.Topic, err = coerce.ToString(values["topic"]); err != nil {
		return err
	}
	if i.EventType, err = coerce.ToString(values["eventType"]); err != nil {
		return err
	}
	i.Data = values["data"]
	if i.Format, err = coerce.ToString(values["format"]); err != nil {
		return err
	}
	i.EnableValidation, err = coerce.ToBool(values["enableValidation"])
	return err
}

// Output holds the per-invocation outputs of the SSE Send activity.
type Output struct {
	// Success indicates whether the event was dispatched without error.
	Success bool `md:"success"`
	// SentCount is the number of clients that received the event.
	SentCount int `md:"sentCount"`
	// EventID is the event identifier that was sent (generated if not provided).
	EventID string `md:"eventId"`
	// Timestamp is the ISO-8601 time at which the event was sent.
	Timestamp string `md:"timestamp"`
	// Error contains the error message when Success is false.
	Error string `md:"error"`
}

// ToMap serialises Output for the Flogo engine.
func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"success":   o.Success,
		"sentCount": o.SentCount,
		"eventId":   o.EventID,
		"timestamp": o.Timestamp,
		"error":     o.Error,
	}
}

// FromMap deserialises Output from the Flogo engine map.
func (o *Output) FromMap(values map[string]interface{}) error {
	var err error
	if o.Success, err = coerce.ToBool(values["success"]); err != nil {
		return err
	}
	sc, err := coerce.ToInt(values["sentCount"])
	if err != nil {
		return err
	}
	o.SentCount = sc
	if o.EventID, err = coerce.ToString(values["eventId"]); err != nil {
		return err
	}
	if o.Timestamp, err = coerce.ToString(values["timestamp"]); err != nil {
		return err
	}
	o.Error, err = coerce.ToString(values["error"])
	return err
}

package ssesend

import (
	"github.com/project-flogo/core/data/coerce"
)

// Settings represents the activity settings
type Settings struct {
	SSEServerRef string `md:"sseServerRef"`
	Retry        int    `md:"retry"`
	Topic        string `md:"topic"`
	EventType    string `md:"eventType"`
}

// Input represents the activity input
type Input struct {
	ConnectionID     string      `md:"connectionId"`
	Target           string      `md:"target"`
	EventID          string      `md:"eventId"`
	Topic            string      `md:"topic"`
	EventType        string      `md:"eventType"`
	Data             interface{} `md:"data,required"`
	Format           string      `md:"format"`
	EnableValidation bool        `md:"enableValidation"`
}

// Output represents the activity output
type Output struct {
	Success   bool   `md:"success"`
	SentCount int    `md:"sentCount"`
	EventID   string `md:"eventId"`
	Timestamp string `md:"timestamp"`
	Error     string `md:"error"`
}

// ToMap converts Input to map
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

// FromMap populates Input from map
func (i *Input) FromMap(values map[string]interface{}) error {
	var err error

	i.ConnectionID, err = coerce.ToString(values["connectionId"])
	if err != nil {
		return err
	}

	i.Target, err = coerce.ToString(values["target"])
	if err != nil {
		return err
	}

	i.EventID, err = coerce.ToString(values["eventId"])
	if err != nil {
		return err
	}

	i.Topic, err = coerce.ToString(values["topic"])
	if err != nil {
		return err
	}

	i.EventType, err = coerce.ToString(values["eventType"])
	if err != nil {
		return err
	}

	i.Data = values["data"]

	i.Format, err = coerce.ToString(values["format"])
	if err != nil {
		return err
	}

	i.EnableValidation, err = coerce.ToBool(values["enableValidation"])
	if err != nil {
		return err
	}

	return nil
}

// ToMap converts Output to map
func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"success":   o.Success,
		"sentCount": o.SentCount,
		"eventId":   o.EventID,
		"timestamp": o.Timestamp,
		"error":     o.Error,
	}
}

// FromMap populates Output from map
func (o *Output) FromMap(values map[string]interface{}) error {
	var err error

	o.Success, err = coerce.ToBool(values["success"])
	if err != nil {
		return err
	}

	o.SentCount, err = coerce.ToInt(values["sentCount"])
	if err != nil {
		return err
	}

	o.EventID, err = coerce.ToString(values["eventId"])
	if err != nil {
		return err
	}

	o.Timestamp, err = coerce.ToString(values["timestamp"])
	if err != nil {
		return err
	}

	o.Error, err = coerce.ToString(values["error"])
	if err != nil {
		return err
	}

	return nil
}

// SSEEvent represents an SSE event structure
type SSEEvent struct {
	ID    string `json:"id,omitempty"`
	Event string `json:"event,omitempty"`
	Data  string `json:"data"`
	Retry int    `json:"retry,omitempty"`
}

// TargetType represents the type of target for sending events
type TargetType int

const (
	TargetAll TargetType = iota
	TargetConnection
	TargetTopic
)

// ParsedTarget represents a parsed target specification
type ParsedTarget struct {
	Type       TargetType
	Identifier string
}

package sse

import (
	"github.com/project-flogo/core/data/coerce"
)

// Settings represents the trigger settings
type Settings struct {
	Port              int    `md:"port,required"`
	Path              string `md:"path,required"`
	MaxConnections    int    `md:"maxConnections"`
	EnableCORS        bool   `md:"enableCORS"`
	CORSOrigins       string `md:"corsOrigins"`
	KeepAliveInterval int    `md:"keepAliveInterval"`
	EnableEventStore  bool   `md:"enableEventStore"`
	EventStoreSize    int    `md:"eventStoreSize"`
	EventTTL          int    `md:"eventTTL"`
}

// HandlerSettings represents the handler-specific settings
type HandlerSettings struct {
	Topic     string `md:"topic"`
	EventType string `md:"eventType"`
}

// Output represents the data sent to the flow when a new connection is established
type Output struct {
	ConnectionID string                 `md:"connectionId"`
	ClientIP     string                 `md:"clientIP"`
	UserAgent    string                 `md:"userAgent"`
	Headers      map[string]interface{} `md:"headers"`
	QueryParams  map[string]interface{} `md:"queryParams"`
	Topic        string                 `md:"topic"`
	LastEventID  string                 `md:"lastEventId"`
	Timestamp    string                 `md:"timestamp"`
}

// ToMap converts Output to map
func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"connectionId": o.ConnectionID,
		"clientIP":     o.ClientIP,
		"userAgent":    o.UserAgent,
		"headers":      o.Headers,
		"queryParams":  o.QueryParams,
		"topic":        o.Topic,
		"lastEventId":  o.LastEventID,
		"timestamp":    o.Timestamp,
	}
}

// FromMap populates Output from map
func (o *Output) FromMap(values map[string]interface{}) error {
	var err error

	o.ConnectionID, err = coerce.ToString(values["connectionId"])
	if err != nil {
		return err
	}

	o.ClientIP, err = coerce.ToString(values["clientIP"])
	if err != nil {
		return err
	}

	o.UserAgent, err = coerce.ToString(values["userAgent"])
	if err != nil {
		return err
	}

	if headers, ok := values["headers"]; ok {
		o.Headers, err = coerce.ToObject(headers)
		if err != nil {
			return err
		}
	}

	if queryParams, ok := values["queryParams"]; ok {
		o.QueryParams, err = coerce.ToObject(queryParams)
		if err != nil {
			return err
		}
	}

	o.Topic, err = coerce.ToString(values["topic"])
	if err != nil {
		return err
	}

	o.LastEventID, err = coerce.ToString(values["lastEventId"])
	if err != nil {
		return err
	}

	o.Timestamp, err = coerce.ToString(values["timestamp"])
	if err != nil {
		return err
	}

	return nil
}

// SSEEvent represents an event to be sent to SSE clients
type SSEEvent struct {
	ID    string `json:"id,omitempty"`
	Event string `json:"event,omitempty"`
	Data  string `json:"data"`
	Retry int    `json:"retry,omitempty"`
}

// ConnectionInfo represents information about an active SSE connection
type ConnectionInfo struct {
	ID          string
	ClientIP    string
	UserAgent   string
	Topic       string
	LastEventID string
	ConnectedAt string
	IsActive    bool
}

// Metrics represents SSE trigger metrics
type Metrics struct {
	ActiveConnections int64
	TotalConnections  int64
	EventsSent        int64
	EventsBuffered    int64
	BytesSent         int64
	ErrorCount        int64
}

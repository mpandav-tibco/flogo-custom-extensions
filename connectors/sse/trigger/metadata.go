package sse

import "github.com/project-flogo/core/data/coerce"

// Settings hold trigger-level configuration.
type Settings struct {
	// Port is the HTTP server port for SSE connections.
	Port int `md:"port,required"`
	// Path is the endpoint path for SSE connections (default: /events).
	Path string `md:"path"`
	// CORSEnabled enables Cross-Origin Resource Sharing headers.
	CORSEnabled bool `md:"corsEnabled"`
	// HeartbeatInterval is the keep-alive heartbeat interval in seconds (0 = disabled).
	HeartbeatInterval int `md:"heartbeatInterval"`
}

// HandlerSettings define per-handler configuration.
type HandlerSettings struct {
	// EventType is the default SSE event type for this handler (default: message).
	EventType string `md:"eventType"`
}

// Output is emitted to the Flogo flow for each new SSE client connection.
type Output struct {
	// ConnectionID is a unique identifier for this SSE connection.
	ConnectionID string `md:"connectionId"`
	// ClientIP is the remote address of the connecting client.
	ClientIP string `md:"clientIP"`
	// UserAgent is the HTTP User-Agent header sent by the client.
	UserAgent string `md:"userAgent"`
	// Headers contains all HTTP request headers as a key-value map.
	Headers map[string]interface{} `md:"headers"`
	// QueryParams contains all URL query parameters.
	QueryParams map[string]interface{} `md:"queryParams"`
	// Topic is the topic the client subscribed to (from ?topic= query param).
	Topic string `md:"topic"`
	// LastEventID is the Last-Event-ID header sent for reconnection resumption.
	LastEventID string `md:"lastEventId"`
	// Timestamp is the ISO-8601 connection time.
	Timestamp string `md:"timestamp"`
}

// ToMap serialises Output for the Flogo trigger handler.
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

// FromMap deserialises Output from the Flogo trigger handler map.
func (o *Output) FromMap(values map[string]interface{}) error {
	var err error
	if o.ConnectionID, err = coerce.ToString(values["connectionId"]); err != nil {
		return err
	}
	if o.ClientIP, err = coerce.ToString(values["clientIP"]); err != nil {
		return err
	}
	if o.UserAgent, err = coerce.ToString(values["userAgent"]); err != nil {
		return err
	}
	o.Headers, _ = coerce.ToObject(values["headers"])
	o.QueryParams, _ = coerce.ToObject(values["queryParams"])
	if o.Topic, err = coerce.ToString(values["topic"]); err != nil {
		return err
	}
	if o.LastEventID, err = coerce.ToString(values["lastEventId"]); err != nil {
		return err
	}
	o.Timestamp, err = coerce.ToString(values["timestamp"])
	return err
}

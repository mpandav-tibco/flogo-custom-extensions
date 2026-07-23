package mongodbcdclistener

import (
	"fmt"
	"strings"

	"github.com/project-flogo/core/data/coerce"
)

// Settings contains the MongoDB connection configuration for change streams.
type Settings struct {
	// Connection — either provide a full connectionURI, or host/port (+ credentials).
	ConnectionURI string `md:"connectionURI"` // Full mongodb:// or mongodb+srv:// URI (takes precedence when set)
	Host          string `md:"host"`          // MongoDB host (used when connectionURI is empty)
	Port          int    `md:"port"`          // MongoDB port (default 27017)
	Username      string `md:"username"`      // Authentication user
	Password      string `md:"password"`      // Authentication password
	AuthSource    string `md:"authSource"`    // Authentication database (default admin)
	AuthMechanism string `md:"authMechanism"` // Optional auth mechanism (e.g. SCRAM-SHA-256)
	ReplicaSet    string `md:"replicaSet"`    // Replica set name (change streams require a replica set)

	// TLS
	TLSConfig             bool   `md:"tlsConfig"`             // Enable TLS/SSL
	TLSCAFile             string `md:"tlsCAFile"`             // CA certificate file path
	TLSCertificateKeyFile string `md:"tlsCertificateKeyFile"` // Client certificate + key file path (PEM)
	TLSInsecure           bool   `md:"tlsInsecure"`           // Skip certificate/hostname verification

	// Connection management
	ConnectionTimeout int    `md:"connectionTimeout"` // Connection timeout in seconds (default 30)
	MaxRetryAttempts  int    `md:"maxRetryAttempts"`  // Max reconnect attempts (default 5, negative = infinite)
	RetryDelay        string `md:"retryDelay"`        // Delay between reconnects (default "5s")
}

// HandlerSettings contains per-handler change stream configuration.
type HandlerSettings struct {
	Database                 string `md:"database"`                 // Database to watch (empty = whole deployment)
	Collection               string `md:"collection"`               // Collection to watch (empty = whole database)
	OperationTypes           string `md:"operationTypes"`           // ALL, INSERT, UPDATE, REPLACE, DELETE (comma separated)
	FullDocument             string `md:"fullDocument"`             // default, updateLookup, whenAvailable, required
	FullDocumentBeforeChange string `md:"fullDocumentBeforeChange"` // off, whenAvailable, required (pre-image; MongoDB 6.0+)
	BatchSize                int    `md:"batchSize"`                // Change stream cursor batch size (0 = server default)
	MaxAwaitTime             string `md:"maxAwaitTime"`             // Max time the server waits for new changes (Go duration)
}

// Output represents a single change event delivered to a Flogo flow.
type Output struct {
	EventID       string                 `md:"eventID"`       // Unique event identifier
	EventType     string                 `md:"eventType"`     // INSERT, UPDATE, REPLACE, DELETE, DROP, RENAME, INVALIDATE
	Database      string                 `md:"database"`      // Source database name
	Collection    string                 `md:"collection"`    // Source collection name
	DocumentKey   map[string]interface{} `md:"documentKey"`   // Identifier of the changed document (typically {_id})
	Timestamp     string                 `md:"timestamp"`     // Cluster time of the change (RFC3339)
	Data          map[string]interface{} `md:"data"`          // Full document (INSERT/REPLACE; UPDATE with fullDocument lookup)
	OldData       map[string]interface{} `md:"oldData"`       // Pre-image (requires fullDocumentBeforeChange)
	UpdatedFields map[string]interface{} `md:"updatedFields"` // Fields changed by an UPDATE
	RemovedFields []string               `md:"removedFields"` // Fields removed by an UPDATE
	ResumeToken   string                 `md:"resumeToken"`   // Resume token for restarting the stream
	CorrelationID string                 `md:"correlationID"` // Correlation ID for tracing
}

// Validate checks the trigger connection settings.
func (s *Settings) Validate() error {
	if s.ConnectionURI == "" && s.Host == "" {
		return fmt.Errorf("either connectionURI or host is required")
	}
	if s.ConnectionURI == "" && s.Port <= 0 {
		return fmt.Errorf("port must be a positive integer when host is used")
	}
	return nil
}

// Validate checks the per-handler settings.
func (h *HandlerSettings) Validate() error {
	if h.Collection != "" && h.Database == "" {
		return fmt.Errorf("database is required when collection is set")
	}
	switch strings.ToLower(strings.TrimSpace(h.FullDocument)) {
	case "", "default", "updatelookup", "whenavailable", "required":
	default:
		return fmt.Errorf("invalid fullDocument %q (expected default, updateLookup, whenAvailable, or required)", h.FullDocument)
	}
	switch strings.ToLower(strings.TrimSpace(h.FullDocumentBeforeChange)) {
	case "", "off", "whenavailable", "required":
	default:
		return fmt.Errorf("invalid fullDocumentBeforeChange %q (expected off, whenAvailable, or required)", h.FullDocumentBeforeChange)
	}
	return nil
}

// OperationTypeSet returns the set of enabled operation types.
// An empty result or a set containing "ALL" means all operation types are enabled.
func (h *HandlerSettings) OperationTypeSet() map[string]bool {
	set := make(map[string]bool)
	raw := strings.TrimSpace(h.OperationTypes)
	if raw == "" {
		set["ALL"] = true
		return set
	}
	for _, part := range strings.Split(raw, ",") {
		t := strings.ToUpper(strings.TrimSpace(part))
		if t != "" {
			set[t] = true
		}
	}
	if len(set) == 0 {
		set["ALL"] = true
	}
	return set
}

// ToMap converts Output to a map for Flogo compatibility.
func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"eventID":       o.EventID,
		"eventType":     o.EventType,
		"database":      o.Database,
		"collection":    o.Collection,
		"documentKey":   o.DocumentKey,
		"timestamp":     o.Timestamp,
		"data":          o.Data,
		"oldData":       o.OldData,
		"updatedFields": o.UpdatedFields,
		"removedFields": o.RemovedFields,
		"resumeToken":   o.ResumeToken,
		"correlationID": o.CorrelationID,
	}
}

// FromMap populates Output from a map for Flogo compatibility.
func (o *Output) FromMap(values map[string]interface{}) error {
	var err error
	if o.EventID, err = coerce.ToString(values["eventID"]); err != nil {
		return err
	}
	if o.EventType, err = coerce.ToString(values["eventType"]); err != nil {
		return err
	}
	if o.Database, err = coerce.ToString(values["database"]); err != nil {
		return err
	}
	if o.Collection, err = coerce.ToString(values["collection"]); err != nil {
		return err
	}
	if o.DocumentKey, err = coerce.ToObject(values["documentKey"]); err != nil {
		return err
	}
	if o.Timestamp, err = coerce.ToString(values["timestamp"]); err != nil {
		return err
	}
	if o.Data, err = coerce.ToObject(values["data"]); err != nil {
		return err
	}
	if o.OldData, err = coerce.ToObject(values["oldData"]); err != nil {
		return err
	}
	if o.UpdatedFields, err = coerce.ToObject(values["updatedFields"]); err != nil {
		return err
	}
	removed, err := coerce.ToArray(values["removedFields"])
	if err != nil {
		return err
	}
	o.RemovedFields = nil
	for _, item := range removed {
		s, convErr := coerce.ToString(item)
		if convErr != nil {
			return convErr
		}
		o.RemovedFields = append(o.RemovedFields, s)
	}
	if o.ResumeToken, err = coerce.ToString(values["resumeToken"]); err != nil {
		return err
	}
	if o.CorrelationID, err = coerce.ToString(values["correlationID"]); err != nil {
		return err
	}
	return nil
}

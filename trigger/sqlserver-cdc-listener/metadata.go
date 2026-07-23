package sqlservercdclistener

import (
	"fmt"
	"strings"

	"github.com/project-flogo/core/data/coerce"
)

// Settings contains the SQL Server connection configuration.
type Settings struct {
	// Connection
	Host         string `md:"host,required"`         // SQL Server host
	Port         int    `md:"port,required"`         // SQL Server port (default 1433)
	User         string `md:"user,required"`         // Database user
	Password     string `md:"password,required"`     // Database password
	DatabaseName string `md:"databaseName,required"` // Database to capture changes from (must have CDC enabled)

	// TLS
	Encrypt                string `md:"encrypt"`                // disable, false, true, strict (go-mssqldb encrypt option)
	TrustServerCertificate bool   `md:"trustServerCertificate"` // Skip server certificate verification
	Certificate            string `md:"certificate"`            // Path to server CA certificate (PEM)

	// Connection management
	ConnectionTimeout int    `md:"connectionTimeout"` // Connection timeout in seconds (default 30)
	PollInterval      string `md:"pollInterval"`      // How often to poll for new changes (default "2s")
	MaxRetryAttempts  int    `md:"maxRetryAttempts"`  // Max reconnect attempts (default 5, negative = infinite)
	RetryDelay        string `md:"retryDelay"`        // Delay between reconnects (default "5s")
}

// HandlerSettings contains per-handler CDC capture configuration.
type HandlerSettings struct {
	Schema             string `md:"schema"`             // Source table schema (default dbo)
	Table              string `md:"table,required"`     // Source table name
	CaptureInstance    string `md:"captureInstance"`    // CDC capture instance (default <schema>_<table>)
	EventTypes         string `md:"eventTypes"`         // ALL, INSERT, UPDATE, DELETE (comma separated)
	StartFromBeginning bool   `md:"startFromBeginning"` // Replay all retained changes (from min LSN) on start
}

// Output represents a single change event delivered to a Flogo flow.
type Output struct {
	EventID       string                 `md:"eventID"`       // Unique event identifier
	EventType     string                 `md:"eventType"`     // INSERT, UPDATE, DELETE
	Database      string                 `md:"database"`      // Source database name
	Schema        string                 `md:"schema"`        // Source schema name
	Table         string                 `md:"table"`         // Source table name
	Timestamp     string                 `md:"timestamp"`     // Commit time of the change (RFC3339)
	Data          map[string]interface{} `md:"data"`          // New row image (INSERT/UPDATE)
	OldData       map[string]interface{} `md:"oldData"`       // Previous row image (UPDATE/DELETE)
	LSN           string                 `md:"lsn"`           // Commit LSN (hex) of the change
	SeqVal        string                 `md:"seqVal"`        // Sequence value within the transaction (hex)
	Operation     int                    `md:"operation"`     // Raw CDC operation code (1=delete,2=insert,3=update-before,4=update-after)
	CorrelationID string                 `md:"correlationID"` // Correlation ID for tracing
}

// Validate checks the trigger connection settings.
func (s *Settings) Validate() error {
	if s.Host == "" {
		return fmt.Errorf("host is required")
	}
	if s.Port <= 0 {
		return fmt.Errorf("port must be a positive integer")
	}
	if s.User == "" {
		return fmt.Errorf("user is required")
	}
	if s.DatabaseName == "" {
		return fmt.Errorf("databaseName is required")
	}
	return nil
}

// Validate checks the per-handler settings.
func (h *HandlerSettings) Validate() error {
	if h.Table == "" {
		return fmt.Errorf("table is required")
	}
	if !isValidSQLIdentifier(h.Table) {
		return fmt.Errorf("table %q contains invalid characters", h.Table)
	}
	if h.Schema != "" && !isValidSQLIdentifier(h.Schema) {
		return fmt.Errorf("schema %q contains invalid characters", h.Schema)
	}
	if h.CaptureInstance != "" && !isValidSQLIdentifier(h.CaptureInstance) {
		return fmt.Errorf("captureInstance %q contains invalid characters", h.CaptureInstance)
	}
	return nil
}

// ResolvedSchema returns the schema, defaulting to dbo.
func (h *HandlerSettings) ResolvedSchema() string {
	if strings.TrimSpace(h.Schema) == "" {
		return "dbo"
	}
	return h.Schema
}

// ResolvedCaptureInstance returns the capture instance, defaulting to <schema>_<table>.
func (h *HandlerSettings) ResolvedCaptureInstance() string {
	if strings.TrimSpace(h.CaptureInstance) != "" {
		return h.CaptureInstance
	}
	return fmt.Sprintf("%s_%s", h.ResolvedSchema(), h.Table)
}

// EventTypeSet returns the set of enabled event types (INSERT/UPDATE/DELETE).
// An empty result or a set containing "ALL" means all event types are enabled.
func (h *HandlerSettings) EventTypeSet() map[string]bool {
	set := make(map[string]bool)
	raw := strings.TrimSpace(h.EventTypes)
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

// isValidSQLIdentifier restricts identifiers to a safe character set, preventing
// SQL injection when the capture-instance function name is built into a query.
func isValidSQLIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '_' {
			return false
		}
	}
	return true
}

// ToMap converts Output to a map for Flogo compatibility.
func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"eventID":       o.EventID,
		"eventType":     o.EventType,
		"database":      o.Database,
		"schema":        o.Schema,
		"table":         o.Table,
		"timestamp":     o.Timestamp,
		"data":          o.Data,
		"oldData":       o.OldData,
		"lsn":           o.LSN,
		"seqVal":        o.SeqVal,
		"operation":     o.Operation,
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
	if o.Schema, err = coerce.ToString(values["schema"]); err != nil {
		return err
	}
	if o.Table, err = coerce.ToString(values["table"]); err != nil {
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
	if o.LSN, err = coerce.ToString(values["lsn"]); err != nil {
		return err
	}
	if o.SeqVal, err = coerce.ToString(values["seqVal"]); err != nil {
		return err
	}
	if o.Operation, err = coerce.ToInt(values["operation"]); err != nil {
		return err
	}
	if o.CorrelationID, err = coerce.ToString(values["correlationID"]); err != nil {
		return err
	}
	return nil
}

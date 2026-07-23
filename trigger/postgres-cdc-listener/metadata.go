package postgrescdclistener

import (
	"fmt"
	"strings"

	"github.com/project-flogo/core/data/coerce"
)

// Settings contains the PostgreSQL connection configuration for logical replication.
type Settings struct {
	// Connection
	Host         string `md:"host,required"`         // PostgreSQL server host
	Port         int    `md:"port,required"`         // PostgreSQL server port (default 5432)
	User         string `md:"user,required"`         // Database user with REPLICATION privilege
	Password     string `md:"password,required"`     // Database password
	DatabaseName string `md:"databaseName,required"` // Database name to replicate from

	// SSL/TLS
	TLSConfig bool   `md:"tlsConfig"` // Enable TLS/SSL for the connection
	SSLMode   string `md:"sslMode"`   // require, verify-ca, verify-full (used when tlsConfig=true)
	SSLCA     string `md:"sslCA"`     // CA certificate file path
	SSLCert   string `md:"sslCert"`   // Client certificate file path
	SSLKey    string `md:"sslKey"`    // Client private key file path

	// Connection management
	ConnectionTimeout     int    `md:"connectionTimeout"`     // Connection timeout in seconds (default 30)
	MaxRetryAttempts      int    `md:"maxRetryAttempts"`      // Max reconnect attempts (default 5, negative = infinite)
	RetryDelay            string `md:"retryDelay"`            // Delay between reconnects (default "5s")
	StandbyMessageTimeout string `md:"standbyMessageTimeout"` // Standby status update interval (default "10s")
}

// HandlerSettings contains per-handler logical replication configuration.
type HandlerSettings struct {
	SlotName                     string   `md:"slotName,required"`            // Replication slot name
	PublicationName              string   `md:"publicationName,required"`     // pgoutput publication name
	CreateSlotIfNotExists        bool     `md:"createSlotIfNotExists"`        // Auto-create the slot (default true)
	TemporarySlot                bool     `md:"temporarySlot"`                // Slot is dropped on disconnect (no persistence)
	CreatePublicationIfNotExists bool     `md:"createPublicationIfNotExists"` // Auto-create the publication
	Tables                       []string `md:"tables"`                       // schema.table list for auto CREATE PUBLICATION (empty = FOR ALL TABLES)
	EventTypes                   string   `md:"eventTypes"`                   // ALL, INSERT, UPDATE, DELETE (comma separated)
	IncludeTransactionMarkers    bool     `md:"includeTransactionMarkers"`    // Emit BEGIN/COMMIT events in addition to row events
}

// Output represents a single change event delivered to a Flogo flow.
type Output struct {
	EventID       string                 `md:"eventID"`       // Unique event identifier
	EventType     string                 `md:"eventType"`     // INSERT, UPDATE, DELETE, TRUNCATE, BEGIN, COMMIT
	Database      string                 `md:"database"`      // Source database name
	Schema        string                 `md:"schema"`        // Source schema (namespace)
	Table         string                 `md:"table"`         // Source table name
	Timestamp     string                 `md:"timestamp"`     // Commit timestamp (RFC3339)
	Data          map[string]interface{} `md:"data"`          // New row image (INSERT/UPDATE)
	OldData       map[string]interface{} `md:"oldData"`       // Previous row image (UPDATE/DELETE with REPLICA IDENTITY FULL)
	LSN           string                 `md:"lsn"`           // WAL log sequence number of the change
	XID           int                    `md:"xid"`           // Transaction ID
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
	if s.TLSConfig {
		switch strings.ToLower(s.SSLMode) {
		case "", "require", "verify-ca", "verify-full":
		default:
			return fmt.Errorf("invalid sslMode %q (expected require, verify-ca, or verify-full)", s.SSLMode)
		}
	}
	return nil
}

// Validate checks the per-handler settings.
func (h *HandlerSettings) Validate() error {
	if h.SlotName == "" {
		return fmt.Errorf("slotName is required")
	}
	if !isValidPGIdentifier(h.SlotName) {
		return fmt.Errorf("slotName %q must contain only lowercase letters, digits, and underscores", h.SlotName)
	}
	if h.PublicationName == "" {
		return fmt.Errorf("publicationName is required")
	}
	if !isValidPGIdentifier(h.PublicationName) {
		return fmt.Errorf("publicationName %q must contain only lowercase letters, digits, and underscores", h.PublicationName)
	}
	return nil
}

// EventTypeSet returns the set of enabled row event types (INSERT/UPDATE/DELETE).
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

// isValidPGIdentifier restricts slot/publication names to a safe character set,
// preventing SQL injection when these values are interpolated into DDL.
func isValidPGIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' {
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
		"xid":           o.XID,
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
	if o.XID, err = coerce.ToInt(values["xid"]); err != nil {
		return err
	}
	if o.CorrelationID, err = coerce.ToString(values["correlationID"]); err != nil {
		return err
	}
	return nil
}

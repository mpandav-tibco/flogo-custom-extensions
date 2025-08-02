package mysqlbinloglistener

import (
	"context"
	"fmt"
	"time"

	"github.com/project-flogo/core/data/coerce"
)

// Settings contains the MySQL connection configuration
type Settings struct {
	Host                string `md:"host,required"`         // MySQL server hostname or IP
	Port                int    `md:"port"`                  // MySQL server port (default: 3306)
	User                string `md:"user,required"`         // MySQL username
	Password            string `md:"password,required"`     // MySQL password
	DatabaseName        string `md:"databaseName,required"` // MySQL database name
	ConnectionTimeout   string `md:"connectionTimeout"`     // Connection timeout (default: "30s")
	ReadTimeout         string `md:"readTimeout"`           // Read timeout (default: "30s")
	MaxRetryAttempts    string `md:"maxRetryAttempts"`      // Max retry attempts (default: "3")
	RetryDelay          string `md:"retryDelay"`            // Retry delay (default: "5s")
	HealthCheckInterval string `md:"healthCheckInterval"`   // Health check interval (default: "60s")
	EnableHeartbeat     bool   `md:"enableHeartbeat"`       // Enable heartbeat for connection monitoring
	HeartbeatInterval   string `md:"heartbeatInterval"`     // Heartbeat interval (default: "30s")

	// SSL/TLS Configuration
	SSLMode       string `md:"sslMode"`       // SSL mode: disable, require, verify-ca, verify-full
	SSLCA         string `md:"sslCA"`         // SSL CA certificate file path
	SSLCert       string `md:"sslCert"`       // SSL certificate file path
	SSLKey        string `md:"sslKey"`        // SSL private key file path
	SkipSSLVerify bool   `md:"skipSSLVerify"` // Skip SSL certificate verification
}

// HandlerSettings contains the configuration for each binlog stream handler
type HandlerSettings struct {
	ServerID      int      `md:"serverID,required"` // MySQL server ID for binlog replication (1001-4999 recommended)
	BinlogFile    string   `md:"binlogFile"`        // Starting binlog file (optional, uses current if empty)
	BinlogPos     int      `md:"binlogPos"`         // Starting binlog position (optional, uses current if 0)
	Tables        []string `md:"tables"`            // Tables to monitor (optional, monitors all if empty)
	IncludeGTID   bool     `md:"includeGtid"`       // Include GTID information in events
	IncludeSchema bool     `md:"includeSchema"`     // Include schema information (column names, types) in events
	EventTypes    string   `md:"eventTypes"`        // Event types to capture: ALL, INSERT, UPDATE, DELETE
}

// Output represents the event data sent to Flogo flows
type Output struct {
	EventID       string                 `md:"eventID"`       // Unique event identifier
	EventType     string                 `md:"eventType"`     // INSERT, UPDATE, DELETE
	Database      string                 `md:"database"`      // Source database name
	Table         string                 `md:"table"`         // Source table name
	Timestamp     string                 `md:"timestamp"`     // Event timestamp (ISO format)
	Data          map[string]interface{} `md:"data"`          // Row data (column_index -> value or column_name -> value)
	Schema        map[string]interface{} `md:"schema"`        // Schema information (when includeSchema=true)
	BinlogFile    string                 `md:"binlogFile"`    // Binlog file name
	BinlogPos     int                    `md:"binlogPos"`     // Binlog position
	ServerID      int                    `md:"serverID"`      // MySQL server ID
	GTID          string                 `md:"gtid"`          // GTID (if enabled)
	CorrelationID string                 `md:"correlationID"` // Correlation ID for tracing
}

// ToMap converts Output to map[string]interface{} for Flogo compatibility
func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"eventID":       o.EventID,
		"eventType":     o.EventType,
		"database":      o.Database,
		"table":         o.Table,
		"timestamp":     o.Timestamp,
		"data":          o.Data,
		"schema":        o.Schema,
		"binlogFile":    o.BinlogFile,
		"binlogPos":     o.BinlogPos,
		"serverID":      o.ServerID,
		"gtid":          o.GTID,
		"correlationID": o.CorrelationID,
	}
}

// FromMap populates Output from map[string]interface{} for Flogo compatibility
func (o *Output) FromMap(values map[string]interface{}) error {
	var err error

	if val, ok := values["eventID"]; ok {
		o.EventID, err = coerce.ToString(val)
		if err != nil {
			return err
		}
	}

	if val, ok := values["eventType"]; ok {
		o.EventType, err = coerce.ToString(val)
		if err != nil {
			return err
		}
	}

	if val, ok := values["database"]; ok {
		o.Database, err = coerce.ToString(val)
		if err != nil {
			return err
		}
	}

	if val, ok := values["table"]; ok {
		o.Table, err = coerce.ToString(val)
		if err != nil {
			return err
		}
	}

	if val, ok := values["timestamp"]; ok {
		o.Timestamp, err = coerce.ToString(val)
		if err != nil {
			return err
		}
	}

	if val, ok := values["data"]; ok {
		if dataMap, ok := val.(map[string]interface{}); ok {
			o.Data = dataMap
		}
	}

	if val, ok := values["schema"]; ok {
		if schemaMap, ok := val.(map[string]interface{}); ok {
			o.Schema = schemaMap
		}
	}

	if val, ok := values["binlogFile"]; ok {
		o.BinlogFile, err = coerce.ToString(val)
		if err != nil {
			return err
		}
	}

	if val, ok := values["binlogPos"]; ok {
		o.BinlogPos, err = coerce.ToInt(val)
		if err != nil {
			return err
		}
	}

	if val, ok := values["serverID"]; ok {
		o.ServerID, err = coerce.ToInt(val)
		if err != nil {
			return err
		}
	}

	if val, ok := values["gtid"]; ok {
		o.GTID, err = coerce.ToString(val)
		if err != nil {
			return err
		}
	}

	if val, ok := values["correlationID"]; ok {
		o.CorrelationID, err = coerce.ToString(val)
		if err != nil {
			return err
		}
	}

	return nil
}

// BinlogEvent represents a MySQL binlog event
type BinlogEvent struct {
	ID            string                 `json:"id"`
	Type          string                 `json:"type"`        // INSERT, UPDATE, DELETE
	Database      string                 `json:"database"`    // Database name
	Table         string                 `json:"table"`       // Table name
	Timestamp     time.Time              `json:"timestamp"`   // Event timestamp
	Data          map[string]interface{} `json:"data"`        // Row data
	Schema        map[string]interface{} `json:"schema"`      // Schema information (when includeSchema=true)
	BinlogFile    string                 `json:"binlog_file"` // Binlog file
	BinlogPos     uint32                 `json:"binlog_pos"`  // Binlog position (keep as uint32 for internal use)
	ServerID      uint32                 `json:"server_id"`   // MySQL server ID (keep as uint32 for internal use)
	GTID          string                 `json:"gtid"`        // GTID information
	CorrelationID string                 `json:"correlation_id"`
}

// EventHandler defines how binlog events are processed
type EventHandler interface {
	HandleEvent(ctx context.Context, event *BinlogEvent) error
}

// Validate validates the settings
func (s *Settings) Validate() error {
	if s.Host == "" {
		return fmt.Errorf("host is required")
	}
	if s.User == "" {
		return fmt.Errorf("user is required")
	}
	if s.Password == "" {
		return fmt.Errorf("password is required")
	}
	if s.DatabaseName == "" {
		return fmt.Errorf("databaseName is required")
	}
	// Set default port if not specified or invalid
	if s.Port <= 0 || s.Port > 65535 {
		s.Port = 3306 // Default MySQL port
	}
	if s.ConnectionTimeout == "" {
		s.ConnectionTimeout = "30s"
	}
	if s.ReadTimeout == "" {
		s.ReadTimeout = "30s"
	}
	if s.MaxRetryAttempts == "" {
		s.MaxRetryAttempts = "3"
	}
	if s.RetryDelay == "" {
		s.RetryDelay = "5s"
	}
	if s.HealthCheckInterval == "" {
		s.HealthCheckInterval = "60s"
	}
	if s.HeartbeatInterval == "" {
		s.HeartbeatInterval = "30s"
	}

	// SSL/TLS Validation
	if s.SSLMode == "" {
		s.SSLMode = "disable"
	}

	// Validate SSL mode
	validSSLModes := map[string]bool{
		"disable":     true,
		"require":     true,
		"verify-ca":   true,
		"verify-full": true,
	}
	if !validSSLModes[s.SSLMode] {
		return fmt.Errorf("invalid sslMode: %s (valid: disable, require, verify-ca, verify-full)", s.SSLMode)
	}

	// Validate SSL certificate configuration
	if s.SSLMode != "disable" {
		if s.SSLMode == "verify-ca" || s.SSLMode == "verify-full" {
			if s.SSLCA == "" {
				return fmt.Errorf("sslCA is required when sslMode is %s", s.SSLMode)
			}
		}

		// If client certificate is provided, key must also be provided
		if s.SSLCert != "" && s.SSLKey == "" {
			return fmt.Errorf("sslKey is required when sslCert is provided")
		}

		// If client key is provided, certificate must also be provided
		if s.SSLKey != "" && s.SSLCert == "" {
			return fmt.Errorf("sslCert is required when sslKey is provided")
		}
	}

	return nil
}

// Validate validates the handler settings
func (h *HandlerSettings) Validate() error {
	if h.ServerID <= 0 {
		return fmt.Errorf("serverID is required for MySQL binlog streaming (recommended range: 1001-4999)")
	}
	if h.BinlogPos < 0 {
		return fmt.Errorf("binlogPos must be >= 0")
	}
	if h.EventTypes == "" {
		h.EventTypes = "ALL" // Default to all
	}

	// Validate event types
	validTypes := map[string]bool{"ALL": true, "INSERT": true, "UPDATE": true, "DELETE": true}
	if !validTypes[h.EventTypes] {
		return fmt.Errorf("invalid event type: %s (valid: ALL, INSERT, UPDATE, DELETE)", h.EventTypes)
	}

	return nil
}

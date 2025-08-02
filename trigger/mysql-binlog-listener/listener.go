package mysqlbinloglistener

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/project-flogo/core/support/log"
	"github.com/project-flogo/core/support/trace"
)

// MySQLBinlogListener implements MySQL binlog streaming for real-time change data capture
type MySQLBinlogListener struct {
	settings          *Settings
	db                *sql.DB
	logger            log.Logger
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
	isRunning         bool
	mutex             sync.Mutex
	binlogSyncer      *replication.BinlogSyncer
	binlogStreamer    *replication.BinlogStreamer
	mysqlVersion      string
	isMySQL8Plus      bool
	currentBinlogFile string
	schemaCache       map[string]map[string]interface{} // Cache for table schemas: "db.table" -> schema info
	schemaMutex       sync.RWMutex                      // Protects schema cache access
}

// NewMySQLBinlogListener creates a new MySQL binlog listener
func NewMySQLBinlogListener(settings *Settings, logger log.Logger) *MySQLBinlogListener {
	return &MySQLBinlogListener{
		settings:    settings,
		logger:      logger,
		schemaCache: make(map[string]map[string]interface{}),
	}
}

// Initialize sets up the MySQL connection and validates configuration
func (m *MySQLBinlogListener) Initialize(ctx context.Context) error {
	m.logger.Debugf("Initializing MySQL binlog listener for database: %s", m.settings.DatabaseName)

	if err := m.settings.Validate(); err != nil {
		m.logger.Errorf("Invalid settings: %v", err)
		return fmt.Errorf("invalid settings: %v", err)
	}

	m.ctx, m.cancel = context.WithCancel(ctx)

	m.logger.Debugf("Building MySQL connection string for host: %s:%d, database: %s",
		m.settings.Host, m.settings.Port, m.settings.DatabaseName)

	// Build connection string
	connStr, err := m.buildConnectionString()
	if err != nil {
		m.logger.Errorf("Failed to build MySQL connection string: %v", err)
		return fmt.Errorf("failed to build MySQL connection string: %v", err)
	}

	m.logger.Debugf("Attempting to connect to MySQL with connection string: %s",
		m.maskPassword(connStr))

	// Test database connection
	db, err := sql.Open("mysql", connStr)
	if err != nil {
		m.logger.Errorf("Failed to open MySQL connection: %v", err)
		return fmt.Errorf("failed to open MySQL connection: %v", err)
	}

	m.logger.Debug("Testing MySQL database connectivity with ping retry mechanism")

	// Test connectivity with retry
	err = m.pingWithRetry(db)
	if err != nil {
		db.Close()
		m.logger.Errorf("MySQL connection test failed after retries: %v", err)
		return fmt.Errorf("MySQL connection test failed: %v", err)
	}

	m.db = db

	// Detect MySQL version for compatibility
	if err := m.detectMySQLVersion(); err != nil {
		m.logger.Warnf("Failed to detect MySQL version, assuming legacy syntax: %v", err)
		m.isMySQL8Plus = false
	}

	m.logger.Infof("MySQL binlog listener initialized successfully for database: %s (Version: %s, MySQL 8.0+: %t)",
		m.settings.DatabaseName, m.mysqlVersion, m.isMySQL8Plus)

	return nil
}

// detectMySQLVersion detects the MySQL version and sets compatibility flags
func (m *MySQLBinlogListener) detectMySQLVersion() error {
	row := m.db.QueryRow("SELECT VERSION()")

	var version string
	if err := row.Scan(&version); err != nil {
		return fmt.Errorf("failed to query MySQL version: %v", err)
	}

	m.mysqlVersion = version
	m.logger.Debugf("Detected MySQL version: %s", version)

	// Parse version to determine if it's MySQL 8.0.22+
	// Version format is typically: "8.4.6" or "5.7.44-log" or "10.5.8-MariaDB-1:10.5.8+maria~focal"
	if strings.Contains(strings.ToLower(version), "mariadb") {
		// MariaDB - uses legacy syntax
		m.isMySQL8Plus = false
		m.logger.Debugf("Detected MariaDB, using legacy SQL syntax")
	} else {
		// Parse MySQL version
		versionParts := strings.Split(version, ".")
		if len(versionParts) >= 2 {
			majorVersion, err1 := strconv.Atoi(versionParts[0])
			minorVersion, err2 := strconv.Atoi(versionParts[1])

			if err1 == nil && err2 == nil {
				// MySQL 8.0.22+ uses new syntax
				if majorVersion > 8 || (majorVersion == 8 && minorVersion > 0) {
					m.isMySQL8Plus = true
					m.logger.Debugf("Detected MySQL 8.0+, using new SQL syntax")
				} else if majorVersion == 8 && minorVersion == 0 && len(versionParts) >= 3 {
					// Check patch version for 8.0.x
					patchPart := strings.Split(versionParts[2], "-")[0] // Remove any suffix like "-log"
					patchVersion, err3 := strconv.Atoi(patchPart)
					if err3 == nil && patchVersion >= 22 {
						m.isMySQL8Plus = true
						m.logger.Debugf("Detected MySQL 8.0.22+, using new SQL syntax")
					} else {
						m.isMySQL8Plus = false
						m.logger.Debugf("Detected MySQL 8.0.%d (< 22), using legacy SQL syntax", patchVersion)
					}
				} else {
					m.isMySQL8Plus = false
					m.logger.Debugf("Detected MySQL %d.%d, using legacy SQL syntax", majorVersion, minorVersion)
				}
			} else {
				// Fallback: assume new syntax for anything that looks like MySQL 8+
				m.isMySQL8Plus = strings.HasPrefix(version, "8.")
				m.logger.Debugf("Could not parse version numbers, assuming MySQL 8.0+ based on prefix: %t", m.isMySQL8Plus)
			}
		} else {
			// Fallback: assume new syntax for anything that looks like MySQL 8+
			m.isMySQL8Plus = strings.HasPrefix(version, "8.")
			m.logger.Debugf("Unexpected version format, assuming MySQL 8.0+ based on prefix: %t", m.isMySQL8Plus)
		}
	}

	return nil
}

// StartListening begins MySQL binlog streaming
func (m *MySQLBinlogListener) StartListening(ctx context.Context, handlerSettings *HandlerSettings, eventHandler EventHandler) error {
	// Check for existing trace context first
	var tracingCtx trace.TracingContext
	var isNewTrace bool

	if trace.Enabled() {
		// Try to extract existing trace context first
		existingCtx := trace.ExtractTracingContext(ctx)
		if existingCtx != nil {
			// Reuse existing trace context
			tracingCtx = existingCtx
			isNewTrace = false
			m.logger.Debugf("Reusing existing trace for MySQL binlog listening: %s", tracingCtx.TraceID())

			// Add additional tags to existing trace
			if tracingCtx.SetTags(map[string]interface{}{
				"server.id":    handlerSettings.ServerID,
				"tables":       handlerSettings.Tables,
				"binlog.file":  handlerSettings.BinlogFile,
				"binlog.pos":   handlerSettings.BinlogPos,
				"include.gtid": handlerSettings.IncludeGTID,
				"event.types":  handlerSettings.EventTypes,
			}) {
				m.logger.Debugf("Added MySQL binlog listening metadata to existing trace: %s", tracingCtx.TraceID())
			}
		} else {
			// Create new trace only if no existing trace context
			tracer := trace.GetTracer()
			if tracer != nil {
				traceConfig := trace.Config{
					Operation: "mysql-binlog-start-listening",
					Tags: map[string]interface{}{
						"server.id":    handlerSettings.ServerID,
						"tables":       handlerSettings.Tables,
						"binlog.file":  handlerSettings.BinlogFile,
						"binlog.pos":   handlerSettings.BinlogPos,
						"include.gtid": handlerSettings.IncludeGTID,
						"event.types":  handlerSettings.EventTypes,
					},
					Logger: m.logger,
				}

				var err error
				tracingCtx, err = tracer.StartTrace(traceConfig, nil)
				if err != nil {
					m.logger.Warnf("Failed to start new trace for binlog listening: %v", err)
				} else {
					isNewTrace = true
					m.logger.Debugf("Started new trace for MySQL binlog listening: %s", tracingCtx.TraceID())
				}
			}
		}
	}

	m.logger.Debugf("Starting MySQL binlog listening with settings: ServerID=%d, Tables=%v (TraceID: %s)",
		handlerSettings.ServerID, handlerSettings.Tables,
		func() string {
			if tracingCtx != nil {
				return tracingCtx.TraceID()
			}
			return "none"
		}())

	if err := handlerSettings.Validate(); err != nil {
		m.logger.Errorf("Invalid handler settings: %v", err)
		// Finish trace with error only if we created a new trace
		if tracingCtx != nil && isNewTrace {
			trace.GetTracer().FinishTrace(tracingCtx, err)
		} else if tracingCtx != nil {
			tracingCtx.LogKV(map[string]interface{}{
				"mysql.listener.status": "validation_error",
				"mysql.listener.error":  err.Error(),
			})
		}
		return fmt.Errorf("invalid handler settings: %v", err)
	}

	m.mutex.Lock()
	if m.isRunning {
		m.mutex.Unlock()
		m.logger.Warn("MySQL binlog listener is already running")
		err := fmt.Errorf("MySQL binlog listener is already running")
		// Finish trace with error only if we created a new trace
		if tracingCtx != nil && isNewTrace {
			trace.GetTracer().FinishTrace(tracingCtx, err)
		} else if tracingCtx != nil {
			tracingCtx.LogKV(map[string]interface{}{
				"mysql.listener.status": "already_running",
				"mysql.listener.error":  err.Error(),
			})
		}
		return err
	}
	m.isRunning = true
	m.mutex.Unlock()

	m.logger.Infof("Starting MySQL binlog streaming with ServerID: %d", handlerSettings.ServerID)

	// Start binlog streaming
	m.wg.Add(1)
	go m.streamBinlog(ctx, handlerSettings, eventHandler)

	// Finish trace successfully only if we created a new trace
	if tracingCtx != nil && isNewTrace {
		if err := trace.GetTracer().FinishTrace(tracingCtx, nil); err != nil {
			m.logger.Warnf("Failed to finish trace for binlog listening: %v", err)
		}
	} else if tracingCtx != nil {
		// For reused traces, just log success information
		tracingCtx.LogKV(map[string]interface{}{
			"mysql.listener.status": "started_successfully",
			"mysql.listener.tables": handlerSettings.Tables,
		})
	}

	m.logger.Infof("MySQL binlog listener started for tables: %v", handlerSettings.Tables)
	return nil
}

// Stop gracefully shuts down the MySQL binlog listener
func (m *MySQLBinlogListener) Stop() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if !m.isRunning {
		m.logger.Debug("MySQL binlog listener is already stopped")
		return nil
	}

	m.logger.Info("Stopping MySQL binlog listener...")

	// Cancel context
	if m.cancel != nil {
		m.cancel()
	}

	// Close binlog syncer
	if m.binlogSyncer != nil {
		m.logger.Info("Closing binlog syncer...")
		m.binlogSyncer.Close()
		m.binlogSyncer = nil
	}

	// Wait for streaming goroutines to finish
	m.wg.Wait()

	// Close database connection
	if m.db != nil {
		if err := m.db.Close(); err != nil {
			m.logger.Warnf("Error closing MySQL database connection: %v", err)
		}
		m.db = nil
	}

	m.isRunning = false
	m.logger.Info("MySQL binlog listener stopped successfully")

	return nil
}

// HealthCheck verifies the MySQL connection is still active
func (m *MySQLBinlogListener) HealthCheck() error {
	if m.db == nil {
		return fmt.Errorf("MySQL database connection is nil")
	}

	if err := m.db.Ping(); err != nil {
		return fmt.Errorf("MySQL database ping failed: %v", err)
	}

	return nil
}

// streamBinlog implements MySQL binlog streaming for real-time change detection
func (m *MySQLBinlogListener) streamBinlog(ctx context.Context, handlerSettings *HandlerSettings, eventHandler EventHandler) {
	defer m.wg.Done()

	m.logger.Debugf("Configuring binlog syncer with ServerID: %d", handlerSettings.ServerID)

	// Configure binlog syncer
	cfg := replication.BinlogSyncerConfig{
		ServerID: uint32(handlerSettings.ServerID),
		Flavor:   "mysql",
		Host:     m.settings.Host,
		Port:     uint16(m.settings.Port),
		User:     m.settings.User,
		Password: m.settings.Password,
	}

	// Add TLS configuration for binlog syncer
	if err := m.configureBinlogTLS(&cfg); err != nil {
		m.logger.Errorf("Failed to configure TLS for binlog syncer: %v", err)
		return
	}

	m.binlogSyncer = replication.NewBinlogSyncer(cfg)

	// Determine starting position
	var pos mysql.Position
	if handlerSettings.BinlogFile != "" {
		pos = mysql.Position{
			Name: handlerSettings.BinlogFile,
			Pos:  uint32(handlerSettings.BinlogPos),
		}
		m.currentBinlogFile = handlerSettings.BinlogFile
		m.logger.Infof("Starting from specified position: %s:%d", pos.Name, pos.Pos)
	} else {
		var err error
		pos, err = m.getCurrentBinlogPosition()
		if err != nil {
			m.logger.Errorf("Failed to get current binlog position: %v", err)
			return
		}
		m.currentBinlogFile = pos.Name
		m.logger.Infof("Starting from current position: %s:%d", pos.Name, pos.Pos)
	}

	// Start binlog streaming
	streamer, err := m.binlogSyncer.StartSync(pos)
	if err != nil {
		m.logger.Errorf("Failed to start binlog sync: %v", err)
		return
	}
	m.binlogStreamer = streamer

	m.logger.Info("Binlog streaming started successfully")

	// Create table filter
	var tableFilter map[string]bool
	if len(handlerSettings.Tables) > 0 {
		tableFilter = make(map[string]bool)
		for _, table := range handlerSettings.Tables {
			tableFilter[table] = true
		}
		m.logger.Debugf("Table filter configured for tables: %v", handlerSettings.Tables)
	} else {
		m.logger.Debug("No table filter configured - monitoring all tables")
	}

	// Create event type filter
	eventTypeFilter := make(map[string]bool)
	if handlerSettings.EventTypes == "ALL" || handlerSettings.EventTypes == "" {
		eventTypeFilter["INSERT"] = true
		eventTypeFilter["UPDATE"] = true
		eventTypeFilter["DELETE"] = true
	} else {
		eventTypeFilter[handlerSettings.EventTypes] = true
	}
	m.logger.Debugf("Event type filter configured: %s -> %v", handlerSettings.EventTypes, eventTypeFilter)

	eventCount := 0
	lastLogTime := time.Now()

	// Process binlog events
	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Binlog streaming stopping due to context cancellation")
			return
		default:
			ev, err := streamer.GetEvent(ctx)
			if err != nil {
				if ctx.Err() != nil {
					// Context cancelled, normal shutdown
					return
				}
				m.logger.Errorf("Error reading binlog event: %v", err)
				continue
			}

			eventCount++

			// Log periodic statistics
			if time.Since(lastLogTime) >= time.Minute {
				m.logger.Infof("Processed %d binlog events in the last minute", eventCount)
				eventCount = 0
				lastLogTime = time.Now()
			}

			// Process the binlog event
			if err := m.processBinlogEvent(ev, tableFilter, eventTypeFilter, handlerSettings, eventHandler, ctx); err != nil {
				m.logger.Errorf("Error processing binlog event: %v", err)
			}
		}
	}
}

// getCurrentBinlogPosition gets the current master binlog position
func (m *MySQLBinlogListener) getCurrentBinlogPosition() (mysql.Position, error) {
	m.logger.Debug("Querying current master binlog position")

	// Use appropriate SQL syntax based on MySQL version
	var query string
	if m.isMySQL8Plus {
		query = "SHOW BINARY LOG STATUS"
		m.logger.Debug("Using MySQL 8.0+ syntax: SHOW BINARY LOG STATUS")
	} else {
		query = "SHOW MASTER STATUS"
		m.logger.Debug("Using MySQL 5.7/MariaDB syntax: SHOW MASTER STATUS")
	}

	rows, err := m.db.Query(query)
	if err != nil {
		// If the new syntax fails on MySQL 8+, fall back to legacy syntax
		if m.isMySQL8Plus && strings.Contains(err.Error(), "syntax") {
			m.logger.Warnf("New syntax failed, falling back to legacy syntax: %v", err)
			query = "SHOW MASTER STATUS"
			rows, err = m.db.Query(query)
			if err == nil {
				// Update the flag since legacy syntax worked
				m.isMySQL8Plus = false
				m.logger.Info("Successfully used legacy syntax, updating compatibility flag")
			}
		}

		if err != nil {
			return mysql.Position{}, fmt.Errorf("failed to query master status: %v", err)
		}
	}
	defer rows.Close()

	if !rows.Next() {
		return mysql.Position{}, fmt.Errorf("no master status found - binary logging may not be enabled")
	}

	var file string
	var position uint32
	var binlogDoDB, binlogIgnoreDB, executedGtidSet sql.NullString

	err = rows.Scan(&file, &position, &binlogDoDB, &binlogIgnoreDB, &executedGtidSet)
	if err != nil {
		return mysql.Position{}, fmt.Errorf("failed to scan master status: %v", err)
	}

	m.logger.Debugf("Current master binlog position: %s:%d (using %s)", file, position, query)

	return mysql.Position{
		Name: file,
		Pos:  position,
	}, nil
}

// processBinlogEvent processes a single binlog event and generates database events
func (m *MySQLBinlogListener) processBinlogEvent(ev *replication.BinlogEvent, tableFilter map[string]bool, eventTypeFilter map[string]bool, handlerSettings *HandlerSettings, eventHandler EventHandler, ctx context.Context) error {
	switch e := ev.Event.(type) {
	case *replication.RotateEvent:
		// Update current binlog file when rotation occurs
		m.currentBinlogFile = string(e.NextLogName)
		m.logger.Debugf("Binlog rotated to: %s", m.currentBinlogFile)
		return nil

	case *replication.RowsEvent:
		// Only process table events we're interested in
		tableName := string(e.Table.Table)
		if tableFilter != nil && !tableFilter[tableName] {
			m.logger.Debugf("Skipping table %s (not in filter)", tableName)
			return nil // Skip tables not in filter
		}

		databaseName := string(e.Table.Schema)

		// Determine event type
		var eventType string
		switch ev.Header.EventType {
		case replication.WRITE_ROWS_EVENTv1, replication.WRITE_ROWS_EVENTv2:
			eventType = "INSERT"
		case replication.UPDATE_ROWS_EVENTv1, replication.UPDATE_ROWS_EVENTv2:
			eventType = "UPDATE"
		case replication.DELETE_ROWS_EVENTv1, replication.DELETE_ROWS_EVENTv2:
			eventType = "DELETE"
		default:
			m.logger.Debugf("Skipping unsupported event type: %v", ev.Header.EventType)
			return nil // Skip other event types
		}

		// Check if this event type is enabled
		if len(eventTypeFilter) > 0 && !eventTypeFilter[eventType] {
			m.logger.Debugf("Skipping event type %s (not in filter)", eventType)
			return nil
		}

		// Process each row in the event
		if eventType == "UPDATE" {
			// For UPDATE events, rows come in pairs: [before, after, before, after, ...]
			// We only want to process the "after" rows (odd indices: 1, 3, 5, ...)
			for i := 1; i < len(e.Rows); i += 2 {
				row := e.Rows[i] // This is the "after" row
				var rowData map[string]interface{}
				var schemaInfo map[string]interface{}

				// Handle schema information if enabled
				if handlerSettings.IncludeSchema {
					schema, err := m.getTableSchema(databaseName, tableName)
					if err != nil {
						m.logger.Warnf("Failed to get schema for table %s.%s: %v, falling back to column indices",
							databaseName, tableName, err)
						// Fallback to column indices
						rowData = make(map[string]interface{})
						for j, value := range row {
							columnKey := fmt.Sprintf("col_%d", j)
							rowData[columnKey] = value
						}
					} else {
						// Use column names from schema
						rowData = m.formatRowDataWithSchema(row, schema)
						schemaInfo = schema
					}
				} else {
					// Convert row data to map with column indices (default behavior)
					rowData = make(map[string]interface{})
					for j, value := range row {
						columnKey := fmt.Sprintf("col_%d", j)
						rowData[columnKey] = value
					}
				}

				// Create and send the event
				binlogEvent := &BinlogEvent{
					ID:            m.generateCorrelationID(),
					Type:          eventType,
					Database:      databaseName,
					Table:         tableName,
					Timestamp:     time.Unix(int64(ev.Header.Timestamp), 0),
					Data:          rowData,
					Schema:        schemaInfo,          // Will be nil if includeSchema is false
					BinlogFile:    m.currentBinlogFile, // Use tracked binlog filename
					BinlogPos:     ev.Header.LogPos,
					ServerID:      ev.Header.ServerID,
					CorrelationID: m.generateCorrelationID(),
				}

				// Add GTID if enabled
				if handlerSettings.IncludeGTID {
					// GTID handling would go here if needed
					binlogEvent.GTID = ""
				}

				m.logger.Debugf("Generated UPDATE binlog event: ID=%s, Type=%s, Table=%s.%s (after row)",
					binlogEvent.ID, binlogEvent.Type, binlogEvent.Database, binlogEvent.Table)

				// Convert to output format and trigger handler
				if err := eventHandler.HandleEvent(ctx, binlogEvent); err != nil {
					m.logger.Errorf("Error handling binlog event: %v", err)
				}
			}
		} else {
			// For INSERT and DELETE events, process each row normally
			for _, row := range e.Rows {
				var rowData map[string]interface{}
				var schemaInfo map[string]interface{}

				// Handle schema information if enabled
				if handlerSettings.IncludeSchema {
					schema, err := m.getTableSchema(databaseName, tableName)
					if err != nil {
						m.logger.Warnf("Failed to get schema for table %s.%s: %v, falling back to column indices",
							databaseName, tableName, err)
						// Fallback to column indices
						rowData = make(map[string]interface{})
						for i, value := range row {
							columnKey := fmt.Sprintf("col_%d", i)
							rowData[columnKey] = value
						}
					} else {
						// Use column names from schema
						rowData = m.formatRowDataWithSchema(row, schema)
						schemaInfo = schema
					}
				} else {
					// Convert row data to map with column indices (default behavior)
					rowData = make(map[string]interface{})
					for i, value := range row {
						columnKey := fmt.Sprintf("col_%d", i)
						rowData[columnKey] = value
					}
				}

				// Create binlog event
				binlogEvent := &BinlogEvent{
					ID:            m.generateCorrelationID(),
					Type:          eventType,
					Database:      databaseName,
					Table:         tableName,
					Timestamp:     time.Unix(int64(ev.Header.Timestamp), 0),
					Data:          rowData,
					Schema:        schemaInfo,          // Will be nil if includeSchema is false
					BinlogFile:    m.currentBinlogFile, // Use tracked binlog filename
					BinlogPos:     ev.Header.LogPos,
					ServerID:      ev.Header.ServerID,
					CorrelationID: m.generateCorrelationID(),
				}

				// Add GTID if enabled
				if handlerSettings.IncludeGTID {
					// GTID handling would go here if needed
					binlogEvent.GTID = ""
				}

				m.logger.Debugf("Generated INSERT/DELETE binlog event: ID=%s, Type=%s, Table=%s.%s",
					binlogEvent.ID, binlogEvent.Type, binlogEvent.Database, binlogEvent.Table)

				// Convert to output format and trigger handler
				if err := eventHandler.HandleEvent(ctx, binlogEvent); err != nil {
					m.logger.Errorf("Error handling binlog event: %v", err)
				}
			}
		}

	case *replication.QueryEvent:
		// Handle DDL events (CREATE, ALTER, DROP, etc.)
		query := string(e.Query)
		databaseName := string(e.Schema)

		m.logger.Debugf("Query event: %s on database %s", query, databaseName)
		// DDL event handling can be added here if needed

	default:
		// Skip other event types (format description, etc.)
		m.logger.Debugf("Skipping event type: %T", ev.Event)
		return nil
	}

	return nil
}

// buildConnectionString creates a MySQL connection string
func (m *MySQLBinlogListener) buildConnectionString() (string, error) {
	m.logger.Debug("Building MySQL connection string")

	// Register custom TLS config if SSL is enabled
	if err := m.registerTLSConfig(); err != nil {
		return "", fmt.Errorf("failed to register TLS config: %v", err)
	}

	// Build MySQL DSN
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
		m.settings.User,
		m.settings.Password,
		m.settings.Host,
		m.settings.Port,
		m.settings.DatabaseName)

	// Add SSL/TLS configuration
	if err := m.addSSLConfiguration(&dsn); err != nil {
		return "", fmt.Errorf("failed to configure SSL: %v", err)
	}

	// Add connection timeout
	if timeout, err := time.ParseDuration(m.settings.ConnectionTimeout); err == nil {
		dsn += fmt.Sprintf("&timeout=%s", timeout.String())
		m.logger.Debugf("Connection timeout set to: %s", timeout.String())
	}

	// Add read timeout
	if timeout, err := time.ParseDuration(m.settings.ReadTimeout); err == nil {
		dsn += fmt.Sprintf("&readTimeout=%s", timeout.String())
		m.logger.Debugf("Read timeout set to: %s", timeout.String())
	}

	return dsn, nil
}

// registerTLSConfig registers a custom TLS configuration for MySQL connections
func (m *MySQLBinlogListener) registerTLSConfig() error {
	if !m.isSSLRequired() {
		return nil // No SSL configuration needed
	}

	tlsConfig, err := m.buildTLSConfig()
	if err != nil {
		return fmt.Errorf("failed to build TLS config: %v", err)
	}

	if tlsConfig != nil {
		// Register the custom TLS config with a unique name
		configName := fmt.Sprintf("mysql-binlog-%s-%d", m.settings.Host, m.settings.Port)
		err = mysqldriver.RegisterTLSConfig(configName, tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to register TLS config: %v", err)
		}
		m.logger.Debugf("Registered TLS config: %s", configName)
	}

	return nil
}

// buildTLSConfig creates a TLS configuration based on settings
func (m *MySQLBinlogListener) buildTLSConfig() (*tls.Config, error) {
	if !m.isSSLRequired() {
		return nil, nil
	}

	tlsConfig := &tls.Config{}

	// Set InsecureSkipVerify based on settings
	if m.settings.SkipSSLVerify {
		tlsConfig.InsecureSkipVerify = true
		m.logger.Debug("TLS config: Skip SSL verification enabled")
	} else {
		tlsConfig.InsecureSkipVerify = false
	}

	// Configure SSL mode
	switch m.settings.SSLMode {
	case "require":
		// Require SSL but don't verify certificates
		if !m.settings.SkipSSLVerify {
			tlsConfig.InsecureSkipVerify = false
		}
		m.logger.Debug("TLS config: SSL required")
	case "verify-ca", "verify-full":
		// Verify certificates
		tlsConfig.InsecureSkipVerify = false

		// Load CA certificate
		if err := m.loadCACertificate(tlsConfig); err != nil {
			return nil, fmt.Errorf("failed to load CA certificate: %v", err)
		}

		if m.settings.SSLMode == "verify-full" {
			// Verify hostname
			tlsConfig.ServerName = m.settings.Host
			m.logger.Debug("TLS config: Full certificate verification with hostname")
		} else {
			m.logger.Debug("TLS config: CA certificate verification")
		}
	}

	// Load client certificate if provided
	if err := m.loadClientCertificate(tlsConfig); err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %v", err)
	}

	return tlsConfig, nil
}

// loadCACertificate loads the CA certificate into the TLS config
func (m *MySQLBinlogListener) loadCACertificate(tlsConfig *tls.Config) error {
	var caCertData []byte
	var err error

	if m.settings.SSLCA != "" {
		// Read CA certificate from file
		caCertData, err = ioutil.ReadFile(m.settings.SSLCA)
		if err != nil {
			return fmt.Errorf("failed to read CA certificate file %s: %v", m.settings.SSLCA, err)
		}
		m.logger.Debugf("Using CA certificate file: %s", m.settings.SSLCA)
	} else {
		return fmt.Errorf("CA certificate is required for SSL mode %s", m.settings.SSLMode)
	}

	// Create certificate pool and add CA certificate
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCertData) {
		return fmt.Errorf("failed to parse CA certificate")
	}

	tlsConfig.RootCAs = caCertPool
	m.logger.Debug("CA certificate loaded successfully")
	return nil
}

// loadClientCertificate loads the client certificate into the TLS config
func (m *MySQLBinlogListener) loadClientCertificate(tlsConfig *tls.Config) error {
	var clientCertData, clientKeyData []byte
	var err error

	// Check if client certificate is provided
	hasClientCert := m.settings.SSLCert != ""
	hasClientKey := m.settings.SSLKey != ""

	if !hasClientCert && !hasClientKey {
		return nil // No client certificate configured
	}

	if !hasClientCert || !hasClientKey {
		return fmt.Errorf("both client certificate and key must be provided")
	}

	// Read client certificate and key from files
	clientCertData, err = ioutil.ReadFile(m.settings.SSLCert)
	if err != nil {
		return fmt.Errorf("failed to read client certificate file %s: %v", m.settings.SSLCert, err)
	}

	clientKeyData, err = ioutil.ReadFile(m.settings.SSLKey)
	if err != nil {
		return fmt.Errorf("failed to read client key file %s: %v", m.settings.SSLKey, err)
	}
	m.logger.Debugf("Using client certificate file: %s, key file: %s", m.settings.SSLCert, m.settings.SSLKey)

	// Load client certificate and key
	clientCert, err := tls.X509KeyPair(clientCertData, clientKeyData)
	if err != nil {
		return fmt.Errorf("failed to load client certificate and key: %v", err)
	}

	tlsConfig.Certificates = []tls.Certificate{clientCert}
	m.logger.Debug("Client certificate loaded successfully")
	return nil
}

// addSSLConfiguration adds SSL parameters to the DSN
func (m *MySQLBinlogListener) addSSLConfiguration(dsn *string) error {
	if !m.isSSLRequired() {
		m.logger.Debug("SSL disabled, no SSL configuration added")
		return nil
	}

	// Determine TLS parameter value
	var tlsValue string

	if m.settings.SSLMode == "disable" {
		tlsValue = "false"
	} else if m.settings.SSLMode == "require" && m.settings.SkipSSLVerify {
		tlsValue = "skip-verify"
	} else if m.needsCustomTLSConfig() {
		// Use custom TLS config name
		tlsValue = fmt.Sprintf("mysql-binlog-%s-%d", m.settings.Host, m.settings.Port)
	} else {
		tlsValue = "true"
	}

	*dsn += fmt.Sprintf("&tls=%s", tlsValue)
	m.logger.Debugf("SSL/TLS configuration added: tls=%s", tlsValue)

	return nil
}

// isSSLRequired checks if SSL is required based on settings
func (m *MySQLBinlogListener) isSSLRequired() bool {
	return m.settings.SSLMode != "disable" && m.settings.SSLMode != ""
}

// needsCustomTLSConfig checks if custom TLS configuration is needed
func (m *MySQLBinlogListener) needsCustomTLSConfig() bool {
	if !m.isSSLRequired() {
		return false
	}

	// Custom TLS config is needed for:
	// 1. Certificate-based authentication (client certs)
	// 2. Custom CA certificates
	// 3. verify-ca or verify-full modes
	return m.settings.SSLCert != "" ||
		m.settings.SSLCA != "" ||
		(m.settings.SSLMode == "verify-ca" || m.settings.SSLMode == "verify-full")
}

// configureBinlogTLS configures TLS for the binlog syncer
func (m *MySQLBinlogListener) configureBinlogTLS(cfg *replication.BinlogSyncerConfig) error {
	if !m.isSSLRequired() {
		m.logger.Debug("SSL disabled for binlog syncer")
		return nil
	}

	m.logger.Debugf("Configuring SSL for binlog syncer, mode: %s", m.settings.SSLMode)

	// Build TLS config for binlog syncer
	tlsConfig, err := m.buildTLSConfig()
	if err != nil {
		return fmt.Errorf("failed to build TLS config for binlog syncer: %v", err)
	}

	if tlsConfig != nil {
		cfg.TLSConfig = tlsConfig
		m.logger.Debug("TLS configuration applied to binlog syncer")
	} else if m.settings.SSLMode == "require" {
		// For basic SSL requirement without custom config, enable SSL
		cfg.TLSConfig = &tls.Config{
			InsecureSkipVerify: m.settings.SkipSSLVerify,
		}
		m.logger.Debug("Basic SSL enabled for binlog syncer")
	}

	return nil
}

// pingWithRetry attempts to ping the database with retries
func (m *MySQLBinlogListener) pingWithRetry(db *sql.DB) error {
	maxRetries, _ := strconv.Atoi(m.settings.MaxRetryAttempts)
	if maxRetries <= 0 {
		maxRetries = 3
	}

	retryDelay, _ := time.ParseDuration(m.settings.RetryDelay)
	if retryDelay <= 0 {
		retryDelay = 5 * time.Second
	}

	m.logger.Debugf("Starting ping retry with maxRetries=%d, retryDelay=%v", maxRetries, retryDelay)

	var lastErr error
	for i := 0; i <= maxRetries; i++ {
		if err := db.Ping(); err == nil {
			if i > 0 {
				m.logger.Infof("MySQL connection successful on attempt %d", i+1)
			}
			return nil
		} else {
			lastErr = err
			if i < maxRetries {
				m.logger.Warnf("MySQL ping failed (attempt %d/%d): %v. Retrying in %v...",
					i+1, maxRetries+1, err, retryDelay)
				time.Sleep(retryDelay)
			}
		}
	}

	return fmt.Errorf("failed to connect to MySQL after %d attempts: %v", maxRetries+1, lastErr)
}

// generateCorrelationID creates a unique correlation ID
func (m *MySQLBinlogListener) generateCorrelationID() string {
	return fmt.Sprintf("mysql_%d_%d", time.Now().UnixNano(), time.Now().Nanosecond())
}

// getTableSchema fetches and caches table schema information
func (m *MySQLBinlogListener) getTableSchema(database, table string) (map[string]interface{}, error) {
	tableKey := fmt.Sprintf("%s.%s", database, table)

	// Check cache first
	m.schemaMutex.RLock()
	if schema, exists := m.schemaCache[tableKey]; exists {
		m.schemaMutex.RUnlock()
		return schema, nil
	}
	m.schemaMutex.RUnlock()

	// Fetch schema from database
	query := `
		SELECT 
			COLUMN_NAME,
			DATA_TYPE,
			IS_NULLABLE,
			COLUMN_DEFAULT,
			COLUMN_KEY,
			EXTRA,
			ORDINAL_POSITION
		FROM INFORMATION_SCHEMA.COLUMNS 
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? 
		ORDER BY ORDINAL_POSITION`

	rows, err := m.db.Query(query, database, table)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch table schema: %v", err)
	}
	defer rows.Close()

	columns := make([]map[string]interface{}, 0)
	columnNames := make([]string, 0)

	for rows.Next() {
		var columnName, dataType, isNullable, columnKey, extra string
		var columnDefault sql.NullString
		var ordinalPosition int

		err := rows.Scan(&columnName, &dataType, &isNullable, &columnDefault, &columnKey, &extra, &ordinalPosition)
		if err != nil {
			return nil, fmt.Errorf("failed to scan column info: %v", err)
		}

		columnInfo := map[string]interface{}{
			"name":             columnName,
			"type":             dataType,
			"nullable":         isNullable == "YES",
			"key":              columnKey,
			"extra":            extra,
			"ordinal_position": ordinalPosition,
		}

		if columnDefault.Valid {
			columnInfo["default"] = columnDefault.String
		}

		columns = append(columns, columnInfo)
		columnNames = append(columnNames, columnName)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over schema rows: %v", err)
	}

	schema := map[string]interface{}{
		"database":     database,
		"table":        table,
		"columns":      columns,
		"column_names": columnNames,
	}

	// Cache the schema
	m.schemaMutex.Lock()
	m.schemaCache[tableKey] = schema
	m.schemaMutex.Unlock()

	m.logger.Debugf("Cached schema for table %s.%s with %d columns", database, table, len(columns))
	return schema, nil
}

// formatRowDataWithSchema converts row data using column names when schema is available
func (m *MySQLBinlogListener) formatRowDataWithSchema(row []interface{}, schema map[string]interface{}) map[string]interface{} {
	rowData := make(map[string]interface{})

	if columnNames, ok := schema["column_names"].([]string); ok && len(columnNames) > 0 {
		// Use column names from schema
		for i, value := range row {
			if i < len(columnNames) {
				rowData[columnNames[i]] = value
			} else {
				// Fallback to col_X format for extra columns
				columnKey := fmt.Sprintf("col_%d", i)
				rowData[columnKey] = value
			}
		}
	} else {
		// Fallback to col_X format if schema is not available
		for i, value := range row {
			columnKey := fmt.Sprintf("col_%d", i)
			rowData[columnKey] = value
		}
	}

	return rowData
}

// maskPassword masks the password in connection strings for logging
func (m *MySQLBinlogListener) maskPassword(dsn string) string {
	// Simple regex replacement to mask password in DSN
	// Format: user:password@tcp(host:port)/database
	if idx := strings.Index(dsn, ":"); idx > 0 {
		if idx2 := strings.Index(dsn[idx+1:], "@"); idx2 > 0 {
			return dsn[:idx+1] + "***" + dsn[idx+1+idx2:]
		}
	}
	return dsn
}

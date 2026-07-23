package sqlservercdclistener

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "github.com/microsoft/go-mssqldb"
	"github.com/project-flogo/core/support/log"
)

// ChangeEvent is a database-agnostic representation of a single SQL Server change.
type ChangeEvent struct {
	ID            string
	Type          string
	Database      string
	Schema        string
	Table         string
	Timestamp     time.Time
	Data          map[string]interface{}
	OldData       map[string]interface{}
	LSN           string
	SeqVal        string
	Operation     int
	CorrelationID string
}

// EventHandler consumes decoded change events. Implemented by the Flogo adapter
// and by tests, so the listener can be exercised without the full Flogo runtime.
type EventHandler interface {
	HandleEvent(ctx context.Context, event *ChangeEvent) error
}

// SQLServerCDCListener polls SQL Server CDC change tables and emits change events.
type SQLServerCDCListener struct {
	settings        *Settings
	handlerSettings *HandlerSettings
	logger          log.Logger

	db           *sql.DB
	pollInterval time.Duration
	fromLSN      []byte // next LSN to read from (nil until first poll)
}

// NewSQLServerCDCListener creates a listener from validated settings.
func NewSQLServerCDCListener(s *Settings, hs *HandlerSettings, logger log.Logger) *SQLServerCDCListener {
	return &SQLServerCDCListener{
		settings:        s,
		handlerSettings: hs,
		logger:          logger,
	}
}

// buildDSN assembles the go-mssqldb connection string from settings.
func (l *SQLServerCDCListener) buildDSN() string {
	query := url.Values{}
	query.Set("database", l.settings.DatabaseName)

	encrypt := strings.TrimSpace(l.settings.Encrypt)
	if encrypt == "" {
		encrypt = "disable"
	}
	query.Set("encrypt", encrypt)
	if l.settings.TrustServerCertificate {
		query.Set("trustservercertificate", "true")
	}
	if strings.TrimSpace(l.settings.Certificate) != "" {
		query.Set("certificate", l.settings.Certificate)
	}
	ct := l.settings.ConnectionTimeout
	if ct <= 0 {
		ct = 30
	}
	query.Set("connection timeout", fmt.Sprintf("%d", ct))
	query.Set("dial timeout", fmt.Sprintf("%d", ct))

	u := &url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(l.settings.User, l.settings.Password),
		Host:     fmt.Sprintf("%s:%d", l.settings.Host, l.settings.Port),
		RawQuery: query.Encode(),
	}
	return u.String()
}

// Prepare opens the database, verifies CDC is enabled, and initialises the LSN cursor.
func (l *SQLServerCDCListener) Prepare(ctx context.Context) error {
	pi := 2 * time.Second
	if d, err := time.ParseDuration(strings.TrimSpace(l.settings.PollInterval)); err == nil && d > 0 {
		pi = d
	}
	l.pollInterval = pi

	db, err := sql.Open("sqlserver", l.buildDSN())
	if err != nil {
		return fmt.Errorf("failed to open SQL Server connection: %w", err)
	}
	db.SetMaxOpenConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to connect to SQL Server: %w", err)
	}
	l.db = db

	// Verify CDC is enabled on the database.
	var cdcEnabled bool
	if err := db.QueryRowContext(ctx,
		"SELECT is_cdc_enabled FROM sys.databases WHERE name = @p1",
		l.settings.DatabaseName).Scan(&cdcEnabled); err != nil {
		return fmt.Errorf("failed to check CDC status: %w", err)
	}
	if !cdcEnabled {
		return fmt.Errorf("change data capture is not enabled on database %q (run sys.sp_cdc_enable_db)", l.settings.DatabaseName)
	}

	// Verify the capture instance exists.
	instance := l.handlerSettings.ResolvedCaptureInstance()
	var count int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM cdc.change_tables WHERE capture_instance = @p1",
		instance).Scan(&count); err != nil {
		return fmt.Errorf("failed to verify capture instance %q: %w", instance, err)
	}
	if count == 0 {
		return fmt.Errorf("CDC capture instance %q not found (enable CDC on the table with sys.sp_cdc_enable_table)", instance)
	}

	l.logger.Infof("SQL Server CDC: connected (database=%q capture_instance=%q poll=%s)",
		l.settings.DatabaseName, instance, l.pollInterval)
	return nil
}

// Stream polls for changes until the context is cancelled.
func (l *SQLServerCDCListener) Stream(ctx context.Context, handler EventHandler) error {
	instance := l.handlerSettings.ResolvedCaptureInstance()

	l.logger.Infof("SQL Server CDC: polling started for capture_instance=%q", instance)

	ticker := time.NewTicker(l.pollInterval)
	defer ticker.Stop()

	// Poll once immediately, then on each tick.
	for {
		if err := l.poll(ctx, instance, handler); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// poll reads and dispatches any changes committed since the last LSN.
func (l *SQLServerCDCListener) poll(ctx context.Context, instance string, handler EventHandler) error {
	maxLSN, err := l.scanLSN(ctx, "SELECT sys.fn_cdc_get_max_lsn()")
	if err != nil {
		return fmt.Errorf("failed to get max LSN: %w", err)
	}
	if maxLSN == nil {
		// No changes captured yet.
		return nil
	}

	minLSN, err := l.scanLSN(ctx, "SELECT sys.fn_cdc_get_min_lsn(@p1)", instance)
	if err != nil {
		return fmt.Errorf("failed to get min LSN: %w", err)
	}

	if l.fromLSN == nil {
		// First poll: replay from the beginning, or start after the current max.
		if l.handlerSettings.StartFromBeginning {
			l.fromLSN = minLSN
		} else {
			next, err := l.incrementLSN(ctx, maxLSN)
			if err != nil {
				return err
			}
			l.fromLSN = next
		}
	}

	// Clamp the cursor to the minimum retained LSN (changes may have aged out).
	if minLSN != nil && bytes.Compare(l.fromLSN, minLSN) < 0 {
		l.fromLSN = minLSN
	}

	// Nothing new to read.
	if bytes.Compare(l.fromLSN, maxLSN) > 0 {
		return nil
	}

	if err := l.queryChanges(ctx, instance, l.fromLSN, maxLSN, handler); err != nil {
		return err
	}

	// Advance the cursor past the LSN we just consumed.
	next, err := l.incrementLSN(ctx, maxLSN)
	if err != nil {
		return err
	}
	l.fromLSN = next
	return nil
}

// queryChanges reads the change rows in (fromLSN, toLSN] and dispatches events.
func (l *SQLServerCDCListener) queryChanges(ctx context.Context, instance string, fromLSN, toLSN []byte, handler EventHandler) error {
	// instance is validated to a safe identifier; interpolation is safe here.
	fn := fmt.Sprintf("cdc.fn_cdc_get_all_changes_%s", instance)
	q := fmt.Sprintf(`SELECT sys.fn_cdc_map_lsn_to_time(__$start_lsn) AS __commit_time, *
FROM %s(@p1, @p2, N'all update old')
ORDER BY __$start_lsn, __$seqval, __$operation`, fn)

	rows, err := l.db.QueryContext(ctx, q, fromLSN, toLSN)
	if err != nil {
		return fmt.Errorf("failed to query changes for %q: %w", instance, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("failed to read columns: %w", err)
	}

	enabled := l.handlerSettings.EventTypeSet()
	schema := l.handlerSettings.ResolvedSchema()
	var pendingBefore map[string]interface{}

	for rows.Next() {
		values := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Errorf("failed to scan change row: %w", err)
		}

		var (
			operation  int
			startLSN   []byte
			seqVal     []byte
			commitTime time.Time
		)
		data := make(map[string]interface{})

		for i, col := range cols {
			switch col {
			case "__commit_time":
				if t, ok := values[i].(time.Time); ok {
					commitTime = t
				}
			case "__$operation":
				operation = toInt(values[i])
			case "__$start_lsn":
				if b, ok := values[i].([]byte); ok {
					startLSN = b
				}
			case "__$seqval":
				if b, ok := values[i].([]byte); ok {
					seqVal = b
				}
			case "__$end_lsn", "__$update_mask", "__$command_id":
				// CDC metadata columns — ignore.
			default:
				data[col] = normalizeSQLValue(values[i])
			}
		}

		switch operation {
		case 3: // update: before image
			pendingBefore = data
			continue
		case 4: // update: after image
			if !enabled["ALL"] && !enabled["UPDATE"] {
				pendingBefore = nil
				continue
			}
			event := l.newEvent("UPDATE", schema, commitTime, startLSN, seqVal, operation)
			event.Data = data
			event.OldData = pendingBefore
			pendingBefore = nil
			if err := handler.HandleEvent(ctx, event); err != nil {
				// Stop the batch without advancing the cursor so the change is
				// re-read on the next poll (at-least-once delivery).
				return fmt.Errorf("handler error: %w", err)
			}
		case 2: // insert
			if !enabled["ALL"] && !enabled["INSERT"] {
				continue
			}
			event := l.newEvent("INSERT", schema, commitTime, startLSN, seqVal, operation)
			event.Data = data
			if err := handler.HandleEvent(ctx, event); err != nil {
				// Stop the batch without advancing the cursor so the change is
				// re-read on the next poll (at-least-once delivery).
				return fmt.Errorf("handler error: %w", err)
			}
		case 1: // delete
			if !enabled["ALL"] && !enabled["DELETE"] {
				continue
			}
			event := l.newEvent("DELETE", schema, commitTime, startLSN, seqVal, operation)
			event.OldData = data
			if err := handler.HandleEvent(ctx, event); err != nil {
				// Stop the batch without advancing the cursor so the change is
				// re-read on the next poll (at-least-once delivery).
				return fmt.Errorf("handler error: %w", err)
			}
		}
	}

	return rows.Err()
}

// newEvent builds a ChangeEvent with common metadata populated.
func (l *SQLServerCDCListener) newEvent(eventType, schema string, commitTime time.Time, startLSN, seqVal []byte, operation int) *ChangeEvent {
	ts := commitTime
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	return &ChangeEvent{
		ID:            l.generateID(),
		Type:          eventType,
		Database:      l.settings.DatabaseName,
		Schema:        schema,
		Table:         l.handlerSettings.Table,
		Timestamp:     ts.UTC(),
		LSN:           hexLSN(startLSN),
		SeqVal:        hexLSN(seqVal),
		Operation:     operation,
		CorrelationID: l.generateID(),
	}
}

// scanLSN runs a query returning a single binary(10) LSN, which may be NULL.
func (l *SQLServerCDCListener) scanLSN(ctx context.Context, query string, args ...interface{}) ([]byte, error) {
	var lsn []byte
	err := l.db.QueryRowContext(ctx, query, args...).Scan(&lsn)
	if err != nil {
		return nil, err
	}
	return lsn, nil
}

// incrementLSN returns sys.fn_cdc_increment_lsn(lsn).
func (l *SQLServerCDCListener) incrementLSN(ctx context.Context, lsn []byte) ([]byte, error) {
	next, err := l.scanLSN(ctx, "SELECT sys.fn_cdc_increment_lsn(@p1)", lsn)
	if err != nil {
		return nil, fmt.Errorf("failed to increment LSN: %w", err)
	}
	return next, nil
}

// Close releases the database connection.
func (l *SQLServerCDCListener) Close(_ context.Context) error {
	if l.db != nil {
		err := l.db.Close()
		l.db = nil
		return err
	}
	return nil
}

// Cursor returns the next LSN to read from, so the trigger can persist the
// position reached before a reconnect.
func (l *SQLServerCDCListener) Cursor() []byte {
	return l.fromLSN
}

// SeedCursor sets the LSN to resume from. A nil cursor preserves the first-poll
// behaviour (start from beginning or after the current max LSN).
func (l *SQLServerCDCListener) SeedCursor(cursor []byte) {
	if cursor != nil {
		l.fromLSN = cursor
	}
}

// generateID returns a random hex identifier for events/correlation.
func (l *SQLServerCDCListener) generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("evt-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// hexLSN renders a binary LSN as a 0x-prefixed hex string.
func hexLSN(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return "0x" + hex.EncodeToString(b)
}

// normalizeSQLValue converts driver values into JSON-friendly types.
func normalizeSQLValue(v interface{}) interface{} {
	switch val := v.(type) {
	case nil:
		return nil
	case []byte:
		return "0x" + hex.EncodeToString(val)
	case time.Time:
		return val.UTC().Format(time.RFC3339Nano)
	default:
		return v
	}
}

// toInt coerces common integer driver types to int.
func toInt(v interface{}) int {
	switch n := v.(type) {
	case int64:
		return int(n)
	case int32:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

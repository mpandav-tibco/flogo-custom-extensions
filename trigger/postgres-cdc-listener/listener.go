package postgrescdclistener

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/project-flogo/core/support/log"
)

const outputPlugin = "pgoutput"

// ChangeEvent is a decoded logical-replication change.
type ChangeEvent struct {
	ID            string
	Type          string // INSERT, UPDATE, DELETE, TRUNCATE, BEGIN, COMMIT
	Database      string
	Schema        string
	Table         string
	Timestamp     time.Time
	Data          map[string]interface{}
	OldData       map[string]interface{}
	LSN           string
	XID           uint32
	CorrelationID string
}

// EventHandler consumes decoded change events. The Flogo trigger provides an
// implementation that dispatches to a flow; tests provide a collector.
type EventHandler interface {
	HandleEvent(ctx context.Context, event *ChangeEvent) error
}

// PostgresCDCListener streams changes from a PostgreSQL logical replication slot
// using the built-in pgoutput plugin.
type PostgresCDCListener struct {
	settings *Settings
	handler  *HandlerSettings
	logger   log.Logger

	replConn *pgconn.PgConn

	relations map[uint32]*pglogrepl.RelationMessage
	typeMap   *pgtype.Map

	// per-transaction context captured from the BEGIN message
	txXID        uint32
	txCommitTime time.Time

	// clientXLogPos is the last WAL position we have durably processed and may
	// acknowledge to the server. recordLSN is the start LSN of the change record
	// currently being dispatched (reported on the event).
	clientXLogPos  pglogrepl.LSN
	recordLSN      pglogrepl.LSN
	standbyTimeout time.Duration

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
}

// NewPostgresCDCListener creates a listener for the given settings.
func NewPostgresCDCListener(settings *Settings, handler *HandlerSettings, logger log.Logger) *PostgresCDCListener {
	return &PostgresCDCListener{
		settings:       settings,
		handler:        handler,
		logger:         logger,
		relations:      make(map[uint32]*pglogrepl.RelationMessage),
		typeMap:        pgtype.NewMap(),
		standbyTimeout: 10 * time.Second,
	}
}

// buildConnString builds a libpq-style connection URL. When replication is true,
// the "replication=database" runtime parameter is added so the backend starts a
// logical walsender.
func (l *PostgresCDCListener) buildConnString(replication bool) string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(l.settings.User, l.settings.Password),
		Host:   fmt.Sprintf("%s:%d", l.settings.Host, l.settings.Port),
		Path:   "/" + l.settings.DatabaseName,
	}
	q := u.Query()

	if l.settings.TLSConfig {
		mode := l.settings.SSLMode
		if mode == "" {
			mode = "require"
		}
		q.Set("sslmode", mode)
		if l.settings.SSLCA != "" {
			q.Set("sslrootcert", l.settings.SSLCA)
		}
		if l.settings.SSLCert != "" {
			q.Set("sslcert", l.settings.SSLCert)
		}
		if l.settings.SSLKey != "" {
			q.Set("sslkey", l.settings.SSLKey)
		}
	} else {
		q.Set("sslmode", "disable")
	}

	timeout := l.settings.ConnectionTimeout
	if timeout <= 0 {
		timeout = 30
	}
	q.Set("connect_timeout", strconv.Itoa(timeout))

	if replication {
		q.Set("replication", "database")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// Prepare validates settings, ensures the publication and slot exist, and opens
// the replication connection. It must be called before Stream.
func (l *PostgresCDCListener) Prepare(ctx context.Context) error {
	if l.settings.StandbyMessageTimeout != "" {
		if d, err := time.ParseDuration(l.settings.StandbyMessageTimeout); err == nil && d > 0 {
			l.standbyTimeout = d
		}
	}

	// Ensure publication (and, on demand, the slot check) over a regular SQL connection.
	if err := l.ensurePublication(ctx); err != nil {
		return err
	}

	// Open the dedicated replication connection.
	replConn, err := pgconn.Connect(ctx, l.buildConnString(true))
	if err != nil {
		return fmt.Errorf("failed to open replication connection: %w", err)
	}
	l.replConn = replConn

	sysident, err := pglogrepl.IdentifySystem(ctx, replConn)
	if err != nil {
		replConn.Close(ctx)
		return fmt.Errorf("IDENTIFY_SYSTEM failed: %w", err)
	}
	l.logger.Infof("PostgreSQL CDC: connected (systemID=%s timeline=%d xlogpos=%s db=%s)",
		sysident.SystemID, sysident.Timeline, sysident.XLogPos, sysident.DBName)
	// Deliberately do NOT seed clientXLogPos from the current WAL end. Starting
	// replication at LSN 0 makes the server resume from the slot's
	// confirmed_flush_lsn, so a persistent slot replays every change committed
	// while the trigger was down (at-least-once) instead of skipping to "now".

	if err := l.ensureSlot(ctx); err != nil {
		replConn.Close(ctx)
		return err
	}
	return nil
}

// ensurePublication opens a normal SQL connection to verify/create the publication.
func (l *PostgresCDCListener) ensurePublication(ctx context.Context) error {
	conn, err := pgconn.Connect(ctx, l.buildConnString(false))
	if err != nil {
		return fmt.Errorf("failed to open setup connection: %w", err)
	}
	defer conn.Close(ctx)

	exists, err := l.queryExists(ctx, conn,
		fmt.Sprintf("SELECT 1 FROM pg_publication WHERE pubname = '%s'", l.handler.PublicationName))
	if err != nil {
		return fmt.Errorf("failed to check publication: %w", err)
	}
	if exists {
		l.logger.Debugf("PostgreSQL CDC: publication %q already exists", l.handler.PublicationName)
		return nil
	}
	if !l.handler.CreatePublicationIfNotExists {
		return fmt.Errorf("publication %q does not exist (enable createPublicationIfNotExists or create it manually)", l.handler.PublicationName)
	}

	var ddl string
	if len(l.handler.Tables) == 0 {
		ddl = fmt.Sprintf("CREATE PUBLICATION %s FOR ALL TABLES", l.handler.PublicationName)
	} else {
		quoted := make([]string, 0, len(l.handler.Tables))
		for _, t := range l.handler.Tables {
			qt, err := quoteQualifiedTable(t)
			if err != nil {
				return err
			}
			quoted = append(quoted, qt)
		}
		ddl = fmt.Sprintf("CREATE PUBLICATION %s FOR TABLE %s", l.handler.PublicationName, strings.Join(quoted, ", "))
	}
	l.logger.Infof("PostgreSQL CDC: creating publication: %s", ddl)
	if err := conn.Exec(ctx, ddl).Close(); err != nil {
		return fmt.Errorf("failed to create publication: %w", err)
	}
	return nil
}

// ensureSlot verifies the replication slot exists, creating it when allowed.
func (l *PostgresCDCListener) ensureSlot(ctx context.Context) error {
	setupConn, err := pgconn.Connect(ctx, l.buildConnString(false))
	if err != nil {
		return fmt.Errorf("failed to open slot-check connection: %w", err)
	}
	exists, err := l.queryExists(ctx, setupConn,
		fmt.Sprintf("SELECT 1 FROM pg_replication_slots WHERE slot_name = '%s'", l.handler.SlotName))
	setupConn.Close(ctx)
	if err != nil {
		return fmt.Errorf("failed to check replication slot: %w", err)
	}
	if exists {
		l.logger.Infof("PostgreSQL CDC: using existing replication slot %q", l.handler.SlotName)
		return nil
	}
	if !l.handler.CreateSlotIfNotExists {
		return fmt.Errorf("replication slot %q does not exist (enable createSlotIfNotExists or create it manually)", l.handler.SlotName)
	}

	l.logger.Infof("PostgreSQL CDC: creating replication slot %q (temporary=%t)", l.handler.SlotName, l.handler.TemporarySlot)
	_, err = pglogrepl.CreateReplicationSlot(ctx, l.replConn, l.handler.SlotName, outputPlugin,
		pglogrepl.CreateReplicationSlotOptions{Temporary: l.handler.TemporarySlot})
	if err != nil {
		return fmt.Errorf("failed to create replication slot: %w", err)
	}
	return nil
}

// Stream starts logical replication and blocks, dispatching decoded events to the
// handler until the context is cancelled or a fatal error occurs.
func (l *PostgresCDCListener) Stream(ctx context.Context, handler EventHandler) error {
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return fmt.Errorf("listener already running")
	}
	l.running = true
	streamCtx, cancel := context.WithCancel(ctx)
	l.cancel = cancel
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		l.running = false
		l.mu.Unlock()
	}()

	pluginArgs := []string{
		"proto_version '1'",
		fmt.Sprintf("publication_names '%s'", l.handler.PublicationName),
	}
	if err := pglogrepl.StartReplication(streamCtx, l.replConn, l.handler.SlotName, l.clientXLogPos,
		pglogrepl.StartReplicationOptions{PluginArgs: pluginArgs}); err != nil {
		return fmt.Errorf("START_REPLICATION failed: %w", err)
	}
	l.logger.Infof("PostgreSQL CDC: logical replication started on slot %q, publication %q",
		l.handler.SlotName, l.handler.PublicationName)

	nextStandby := time.Now().Add(l.standbyTimeout)
	for {
		select {
		case <-streamCtx.Done():
			l.logger.Info("PostgreSQL CDC: stream context cancelled, stopping")
			return nil
		default:
		}

		if time.Now().After(nextStandby) {
			if err := pglogrepl.SendStandbyStatusUpdate(streamCtx, l.replConn,
				pglogrepl.StandbyStatusUpdate{WALWritePosition: l.clientXLogPos}); err != nil {
				return fmt.Errorf("SendStandbyStatusUpdate failed: %w", err)
			}
			l.logger.Debugf("PostgreSQL CDC: sent standby status update at %s", l.clientXLogPos)
			nextStandby = time.Now().Add(l.standbyTimeout)
		}

		recvCtx, cancelRecv := context.WithDeadline(streamCtx, nextStandby)
		rawMsg, err := l.replConn.ReceiveMessage(recvCtx)
		cancelRecv()
		if err != nil {
			if pgconn.Timeout(err) {
				continue
			}
			if streamCtx.Err() != nil {
				return nil
			}
			return fmt.Errorf("ReceiveMessage failed: %w", err)
		}

		if errMsg, ok := rawMsg.(*pgproto3.ErrorResponse); ok {
			return fmt.Errorf("postgres WAL error: %s", errMsg.Message)
		}

		copyData, ok := rawMsg.(*pgproto3.CopyData)
		if !ok {
			l.logger.Debugf("PostgreSQL CDC: ignoring non-CopyData message %T", rawMsg)
			continue
		}

		switch copyData.Data[0] {
		case pglogrepl.PrimaryKeepaliveMessageByteID:
			pkm, err := pglogrepl.ParsePrimaryKeepaliveMessage(copyData.Data[1:])
			if err != nil {
				return fmt.Errorf("ParsePrimaryKeepaliveMessage failed: %w", err)
			}
			if pkm.ServerWALEnd > l.clientXLogPos {
				l.clientXLogPos = pkm.ServerWALEnd
			}
			if pkm.ReplyRequested {
				nextStandby = time.Now()
			}
		case pglogrepl.XLogDataByteID:
			xld, err := pglogrepl.ParseXLogData(copyData.Data[1:])
			if err != nil {
				return fmt.Errorf("ParseXLogData failed: %w", err)
			}
			l.recordLSN = xld.WALStart
			if err := l.processWALData(streamCtx, xld.WALData, handler); err != nil {
				return err
			}
			// Only advance the acknowledged position after the change has been
			// successfully handled, so a failed flow is redelivered on reconnect.
			l.clientXLogPos = xld.WALStart + pglogrepl.LSN(len(xld.WALData))
		}
	}
}

// processWALData decodes a single pgoutput message and dispatches any resulting event.
func (l *PostgresCDCListener) processWALData(ctx context.Context, walData []byte, handler EventHandler) error {
	logicalMsg, err := pglogrepl.Parse(walData)
	if err != nil {
		return fmt.Errorf("failed to parse logical replication message: %w", err)
	}

	switch msg := logicalMsg.(type) {
	case *pglogrepl.RelationMessage:
		l.relations[msg.RelationID] = msg

	case *pglogrepl.BeginMessage:
		l.txXID = msg.Xid
		l.txCommitTime = msg.CommitTime
		if l.handler.IncludeTransactionMarkers {
			return handler.HandleEvent(ctx, l.newMarkerEvent("BEGIN", msg.FinalLSN))
		}

	case *pglogrepl.CommitMessage:
		if l.handler.IncludeTransactionMarkers {
			return handler.HandleEvent(ctx, l.newMarkerEvent("COMMIT", msg.CommitLSN))
		}

	case *pglogrepl.InsertMessage:
		return l.dispatchRow(ctx, handler, "INSERT", msg.RelationID, nil, msg.Tuple)

	case *pglogrepl.UpdateMessage:
		return l.dispatchRow(ctx, handler, "UPDATE", msg.RelationID, msg.OldTuple, msg.NewTuple)

	case *pglogrepl.DeleteMessage:
		return l.dispatchRow(ctx, handler, "DELETE", msg.RelationID, msg.OldTuple, nil)

	case *pglogrepl.TruncateMessage:
		if !l.eventTypeEnabled("TRUNCATE") {
			return nil
		}
		for _, relID := range msg.RelationIDs {
			if rel, ok := l.relations[relID]; ok {
				ev := l.baseEvent("TRUNCATE", rel)
				if err := handler.HandleEvent(ctx, ev); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// dispatchRow builds and delivers a row-level change event after applying the
// configured event-type filter.
func (l *PostgresCDCListener) dispatchRow(ctx context.Context, handler EventHandler, eventType string, relationID uint32, oldTuple, newTuple *pglogrepl.TupleData) error {
	if !l.eventTypeEnabled(eventType) {
		return nil
	}
	rel, ok := l.relations[relationID]
	if !ok {
		l.logger.Warnf("PostgreSQL CDC: received %s for unknown relation ID %d (skipping)", eventType, relationID)
		return nil
	}

	ev := l.baseEvent(eventType, rel)
	if newTuple != nil {
		ev.Data = l.decodeTuple(rel, newTuple)
	}
	if oldTuple != nil {
		ev.OldData = l.decodeTuple(rel, oldTuple)
	}
	return handler.HandleEvent(ctx, ev)
}

func (l *PostgresCDCListener) baseEvent(eventType string, rel *pglogrepl.RelationMessage) *ChangeEvent {
	return &ChangeEvent{
		ID:            generateID(),
		Type:          eventType,
		Database:      l.settings.DatabaseName,
		Schema:        rel.Namespace,
		Table:         rel.RelationName,
		Timestamp:     l.txCommitTime,
		LSN:           l.recordLSN.String(),
		XID:           l.txXID,
		CorrelationID: generateID(),
	}
}

func (l *PostgresCDCListener) newMarkerEvent(eventType string, lsn pglogrepl.LSN) *ChangeEvent {
	return &ChangeEvent{
		ID:            generateID(),
		Type:          eventType,
		Database:      l.settings.DatabaseName,
		Timestamp:     l.txCommitTime,
		LSN:           lsn.String(),
		XID:           l.txXID,
		CorrelationID: generateID(),
	}
}

// decodeTuple converts a pgoutput tuple into a column-name keyed map. Null columns
// map to nil; unchanged TOAST columns are omitted (only present in the change set
// when actually modified).
func (l *PostgresCDCListener) decodeTuple(rel *pglogrepl.RelationMessage, tuple *pglogrepl.TupleData) map[string]interface{} {
	result := make(map[string]interface{}, len(tuple.Columns))
	for idx, col := range tuple.Columns {
		if idx >= len(rel.Columns) {
			break
		}
		name := rel.Columns[idx].Name
		switch col.DataType {
		case 'n': // null
			result[name] = nil
		case 'u': // unchanged TOAST value — not part of this change
			continue
		case 't': // text
			result[name] = decodeValue(rel.Columns[idx].DataType, string(col.Data))
		default:
			result[name] = string(col.Data)
		}
	}
	return result
}

func (l *PostgresCDCListener) eventTypeEnabled(eventType string) bool {
	set := l.handler.EventTypeSet()
	if set["ALL"] {
		return true
	}
	return set[eventType]
}

// Close shuts down the replication connection.
func (l *PostgresCDCListener) Close(ctx context.Context) {
	l.mu.Lock()
	if l.cancel != nil {
		l.cancel()
	}
	l.mu.Unlock()
	if l.replConn != nil {
		_ = l.replConn.Close(ctx)
		l.replConn = nil
	}
}

// queryExists runs a "SELECT 1 ..." style query and reports whether a row was returned.
func (l *PostgresCDCListener) queryExists(ctx context.Context, conn *pgconn.PgConn, query string) (bool, error) {
	result := conn.Exec(ctx, query)
	results, err := result.ReadAll()
	if err != nil {
		return false, err
	}
	for _, r := range results {
		if len(r.Rows) > 0 {
			return true, nil
		}
	}
	return false, nil
}

// decodeValue converts a pgoutput text value into a JSON/Flogo-friendly Go value
// based on the column's PostgreSQL type OID. Unknown types are returned as strings,
// which is safe and predictable across all PostgreSQL versions.
func decodeValue(oid uint32, text string) interface{} {
	switch oid {
	case pgtype.BoolOID:
		return text == "t" || text == "true"
	case pgtype.Int2OID, pgtype.Int4OID, pgtype.Int8OID:
		if n, err := strconv.ParseInt(text, 10, 64); err == nil {
			return n
		}
	case pgtype.Float4OID, pgtype.Float8OID:
		if f, err := strconv.ParseFloat(text, 64); err == nil {
			return f
		}
	}
	return text
}

// quoteQualifiedTable validates and double-quotes a possibly schema-qualified table
// identifier for use in CREATE PUBLICATION DDL.
func quoteQualifiedTable(t string) (string, error) {
	parts := strings.Split(t, ".")
	if len(parts) > 2 {
		return "", fmt.Errorf("invalid table identifier %q", t)
	}
	for _, p := range parts {
		if !isValidPGIdentifier(strings.ToLower(p)) {
			return "", fmt.Errorf("invalid table identifier %q (only lowercase letters, digits, underscore allowed)", t)
		}
	}
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = `"` + p + `"`
	}
	return strings.Join(quoted, "."), nil
}

func generateID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}

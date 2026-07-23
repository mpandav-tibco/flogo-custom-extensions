package postgrescdclistener

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/project-flogo/core/support/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// collector is a test EventHandler that funnels events onto a channel.
type collector struct {
	ch chan *ChangeEvent
}

func (c *collector) HandleEvent(_ context.Context, event *ChangeEvent) error {
	c.ch <- event
	return nil
}

// testSettings builds Settings from environment variables, applying defaults that
// match the Docker container started by the integration test harness.
func testSettings() *Settings {
	port := 5432
	if v := os.Getenv("PG_CDC_TEST_PORT"); v != "" {
		fmt.Sscanf(v, "%d", &port)
	}
	get := func(env, def string) string {
		if v := os.Getenv(env); v != "" {
			return v
		}
		return def
	}
	return &Settings{
		Host:         get("PG_CDC_TEST_HOST", "localhost"),
		Port:         port,
		User:         get("PG_CDC_TEST_USER", "cdc"),
		Password:     get("PG_CDC_TEST_PASSWORD", "cdcpass"),
		DatabaseName: get("PG_CDC_TEST_DB", "cdcdb"),
	}
}

// waitForEvent reads the next event or fails on timeout.
func waitForEvent(t *testing.T, c *collector, timeout time.Duration) *ChangeEvent {
	t.Helper()
	select {
	case ev := <-c.ch:
		return ev
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for change event")
		return nil
	}
}

// TestPostgresCDCIntegration exercises the full logical-replication path against a
// live PostgreSQL instance. It is gated by PG_CDC_IT to keep plain `go test` fast.
func TestPostgresCDCIntegration(t *testing.T) {
	if os.Getenv("PG_CDC_IT") == "" {
		t.Skip("set PG_CDC_IT=1 to run the live PostgreSQL CDC integration test")
	}

	settings := testSettings()
	logger := log.RootLogger()
	ctx := context.Background()

	// Unique slot/publication per run to avoid collisions with leftover state.
	suffix := fmt.Sprintf("%d", time.Now().Unix())
	handler := &HandlerSettings{
		SlotName:                     "flogo_it_slot_" + suffix,
		PublicationName:              "flogo_it_pub_" + suffix,
		CreateSlotIfNotExists:        true,
		CreatePublicationIfNotExists: true,
		TemporarySlot:                false,
		EventTypes:                   "ALL",
	}

	// Setup connection for DDL/DML (non-replication).
	setupConn := mustConnect(t, ctx, settings, false)
	defer setupConn.Close(ctx)

	execSQL(t, ctx, setupConn, `DROP TABLE IF EXISTS cdc_test_items`)
	execSQL(t, ctx, setupConn, `CREATE TABLE cdc_test_items (id serial PRIMARY KEY, name text, qty int, active boolean)`)
	execSQL(t, ctx, setupConn, `ALTER TABLE cdc_test_items REPLICA IDENTITY FULL`)

	listener := NewPostgresCDCListener(settings, handler, logger)
	require.NoError(t, listener.Prepare(ctx), "Prepare should establish replication")

	// Cleanup slot/publication/table at the end.
	defer func() {
		listener.Close(context.Background())
		cleanupConn := mustConnect(t, context.Background(), settings, false)
		defer cleanupConn.Close(context.Background())
		_ = cleanupConn.Exec(context.Background(),
			fmt.Sprintf("SELECT pg_drop_replication_slot('%s')", handler.SlotName)).Close()
		_ = cleanupConn.Exec(context.Background(),
			fmt.Sprintf("DROP PUBLICATION IF EXISTS %s", handler.PublicationName)).Close()
		_ = cleanupConn.Exec(context.Background(), "DROP TABLE IF EXISTS cdc_test_items").Close()
	}()

	coll := &collector{ch: make(chan *ChangeEvent, 32)}
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	streamErr := make(chan error, 1)
	go func() { streamErr <- listener.Stream(streamCtx, coll) }()

	// Give the walsender a moment to enter streaming mode.
	time.Sleep(1 * time.Second)

	// --- INSERT ---
	execSQL(t, ctx, setupConn, `INSERT INTO cdc_test_items (name, qty, active) VALUES ('widget', 5, true)`)
	ins := waitForEvent(t, coll, 10*time.Second)
	assert.Equal(t, "INSERT", ins.Type)
	assert.Equal(t, "public", ins.Schema)
	assert.Equal(t, "cdc_test_items", ins.Table)
	assert.Equal(t, "widget", ins.Data["name"])
	assert.Equal(t, int64(5), ins.Data["qty"])
	assert.Equal(t, true, ins.Data["active"])

	// --- UPDATE ---
	execSQL(t, ctx, setupConn, `UPDATE cdc_test_items SET qty = 10 WHERE name = 'widget'`)
	upd := waitForEvent(t, coll, 10*time.Second)
	assert.Equal(t, "UPDATE", upd.Type)
	assert.Equal(t, int64(10), upd.Data["qty"])
	require.NotNil(t, upd.OldData, "REPLICA IDENTITY FULL should provide old row")
	assert.Equal(t, int64(5), upd.OldData["qty"])

	// --- DELETE ---
	execSQL(t, ctx, setupConn, `DELETE FROM cdc_test_items WHERE name = 'widget'`)
	del := waitForEvent(t, coll, 10*time.Second)
	assert.Equal(t, "DELETE", del.Type)
	require.NotNil(t, del.OldData)
	assert.Equal(t, "widget", del.OldData["name"])

	cancelStream()
	select {
	case err := <-streamErr:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not stop after cancel")
	}
}

func mustConnect(t *testing.T, ctx context.Context, s *Settings, replication bool) *pgconn.PgConn {
	t.Helper()
	l := NewPostgresCDCListener(s, &HandlerSettings{}, log.RootLogger())
	conn, err := pgconn.Connect(ctx, l.buildConnString(replication))
	require.NoError(t, err, "connect to test database")
	return conn
}

func execSQL(t *testing.T, ctx context.Context, conn *pgconn.PgConn, sql string) {
	t.Helper()
	require.NoError(t, conn.Exec(ctx, sql).Close(), "exec: %s", sql)
}

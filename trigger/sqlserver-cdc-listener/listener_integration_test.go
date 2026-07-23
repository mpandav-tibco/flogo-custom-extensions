package sqlservercdclistener

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	_ "github.com/microsoft/go-mssqldb"
	"github.com/project-flogo/core/support/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests require a live SQL Server with CDC + SQL Server Agent enabled and
// are gated behind MSSQL_CDC_IT.
//
//	MSSQL_CDC_IT=1 \
//	MSSQL_CDC_TEST_HOST=localhost MSSQL_CDC_TEST_PORT=51433 \
//	MSSQL_CDC_TEST_USER=sa MSSQL_CDC_TEST_PASSWORD='Str0ng!Passw0rd' \
//	go test -run TestSQLServerCDCIntegration -v ./...

// collector is a test EventHandler that funnels events to a channel.
type collector struct {
	events chan *ChangeEvent
}

func (c *collector) HandleEvent(_ context.Context, event *ChangeEvent) error {
	c.events <- event
	return nil
}

func testEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func dsn(host, port, user, password, database string) string {
	q := url.Values{}
	q.Set("database", database)
	q.Set("encrypt", "disable")
	q.Set("connection timeout", "30")
	u := &url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(user, password),
		Host:     fmt.Sprintf("%s:%s", host, port),
		RawQuery: q.Encode(),
	}
	return u.String()
}

func mustOpen(t *testing.T, dataSource string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlserver", dataSource)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	require.NoError(t, db.PingContext(ctx), "ping SQL Server")
	return db
}

func exec(t *testing.T, db *sql.DB, query string, args ...interface{}) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := db.ExecContext(ctx, query, args...)
	require.NoError(t, err, "exec: %s", query)
}

func waitForEvent(t *testing.T, events chan *ChangeEvent, wantType string, timeout time.Duration) *ChangeEvent {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-events:
			if ev.Type == wantType {
				return ev
			}
			t.Logf("skipping %s while waiting for %s", ev.Type, wantType)
		case <-deadline:
			t.Fatalf("timed out waiting for %s event", wantType)
			return nil
		}
	}
}

func TestSQLServerCDCIntegration(t *testing.T) {
	if os.Getenv("MSSQL_CDC_IT") == "" {
		t.Skip("set MSSQL_CDC_IT=1 and run a SQL Server with CDC + Agent to enable this test")
	}

	host := testEnv("MSSQL_CDC_TEST_HOST", "localhost")
	port := testEnv("MSSQL_CDC_TEST_PORT", "51433")
	user := testEnv("MSSQL_CDC_TEST_USER", "sa")
	password := testEnv("MSSQL_CDC_TEST_PASSWORD", "Str0ng!Passw0rd")
	dbName := "cdctest"

	// 1) Create a clean database via the master connection.
	master := mustOpen(t, dsn(host, port, user, password, "master"))
	defer master.Close()
	exec(t, master, fmt.Sprintf(
		"IF DB_ID('%s') IS NOT NULL BEGIN ALTER DATABASE [%s] SET SINGLE_USER WITH ROLLBACK IMMEDIATE; DROP DATABASE [%s]; END",
		dbName, dbName, dbName))
	exec(t, master, fmt.Sprintf("CREATE DATABASE [%s]", dbName))

	// 2) Enable CDC on the database and the table.
	db := mustOpen(t, dsn(host, port, user, password, dbName))
	defer db.Close()
	exec(t, db, "EXEC sys.sp_cdc_enable_db")
	exec(t, db, "CREATE TABLE dbo.items (id INT PRIMARY KEY, name NVARCHAR(100), qty INT, active BIT)")
	exec(t, db, "EXEC sys.sp_cdc_enable_table @source_schema=N'dbo', @source_name=N'items', @role_name=NULL, @supports_net_changes=0")

	// Wait for the capture instance to be registered.
	require.Eventually(t, func() bool {
		var n int
		_ = db.QueryRow("SELECT COUNT(*) FROM cdc.change_tables WHERE capture_instance = @p1", "dbo_items").Scan(&n)
		return n > 0
	}, 30*time.Second, time.Second, "capture instance not registered")

	// 3) Start the listener from the current position (only new changes).
	settings := &Settings{
		Host: host, Port: mustAtoi(t, port), User: user, Password: password,
		DatabaseName: dbName, Encrypt: "disable", PollInterval: "1s", ConnectionTimeout: 30,
	}
	handlerSettings := &HandlerSettings{Schema: "dbo", Table: "items", EventTypes: "ALL"}
	require.NoError(t, settings.Validate())
	require.NoError(t, handlerSettings.Validate())

	listener := NewSQLServerCDCListener(settings, handlerSettings, log.RootLogger())
	require.NoError(t, listener.Prepare(context.Background()), "prepare listener")
	defer listener.Close(context.Background())

	sink := &collector{events: make(chan *ChangeEvent, 64)}
	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	go func() { _ = listener.Stream(streamCtx, sink) }()

	// Give the poller a moment to record the starting LSN before mutating.
	time.Sleep(3 * time.Second)

	// The CDC capture job populates change tables asynchronously, so use a
	// generous timeout for each event.
	const evTimeout = 90 * time.Second

	// INSERT
	exec(t, db, "INSERT INTO dbo.items (id, name, qty, active) VALUES (1, N'widget', 5, 1)")
	ins := waitForEvent(t, sink.events, "INSERT", evTimeout)
	assert.Equal(t, dbName, ins.Database)
	assert.Equal(t, "dbo", ins.Schema)
	assert.Equal(t, "items", ins.Table)
	require.NotNil(t, ins.Data)
	assert.Equal(t, "widget", ins.Data["name"])
	assert.EqualValues(t, 5, ins.Data["qty"])

	// UPDATE
	exec(t, db, "UPDATE dbo.items SET qty = 10 WHERE id = 1")
	upd := waitForEvent(t, sink.events, "UPDATE", evTimeout)
	require.NotNil(t, upd.Data)
	assert.EqualValues(t, 10, upd.Data["qty"])
	require.NotNil(t, upd.OldData, "update before-image present")
	assert.EqualValues(t, 5, upd.OldData["qty"])

	// DELETE
	exec(t, db, "DELETE FROM dbo.items WHERE id = 1")
	del := waitForEvent(t, sink.events, "DELETE", evTimeout)
	require.NotNil(t, del.OldData)
	assert.Equal(t, "widget", del.OldData["name"])

	cancelStream()
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	require.NoError(t, err)
	return n
}

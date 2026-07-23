package mysqlbinloglistener

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/project-flogo/core/support/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Live, Docker-backed integration test for the MySQL binlog CDC listener.
//
// It is skipped unless MYSQL_CDC_IT is set, so normal `go test` runs stay hermetic.
//
// Bring up a binlog-enabled MySQL (or MariaDB) first, e.g.:
//
//	docker run -d --name flogo-cdc-mysql -p 13306:3306 \
//	  -e MYSQL_ROOT_PASSWORD=rootpass \
//	  mysql:8 --log-bin=mysql-bin --binlog-format=ROW --server-id=1
//
// then:
//
//	MYSQL_CDC_IT=1 MYSQL_CDC_TEST_PORT=13306 MYSQL_CDC_TEST_PASSWORD=rootpass \
//	  go test -run TestMySQLBinlogIntegration -v -timeout 180s ./...
func TestMySQLBinlogIntegration(t *testing.T) {
	if os.Getenv("MYSQL_CDC_IT") == "" {
		t.Skip("set MYSQL_CDC_IT=1 to run the live MySQL binlog integration test")
	}

	host := envOr("MYSQL_CDC_TEST_HOST", "127.0.0.1")
	port := envOr("MYSQL_CDC_TEST_PORT", "13306")
	user := envOr("MYSQL_CDC_TEST_USER", "root")
	pass := envOr("MYSQL_CDC_TEST_PASSWORD", "rootpass")
	dbName := envOr("MYSQL_CDC_TEST_DB", "cdctest")
	table := "items"

	portNum, err := strconv.Atoi(port)
	require.NoError(t, err, "invalid MYSQL_CDC_TEST_PORT")

	// Admin connection used to set up the schema and drive DML.
	adminDSN := fmt.Sprintf("%s:%s@tcp(%s:%s)/?multiStatements=true&parseTime=true", user, pass, host, port)
	admin, err := sql.Open("mysql", adminDSN)
	require.NoError(t, err)
	defer admin.Close()

	// Wait for the server to accept connections (container may still be starting).
	require.Eventually(t, func() bool { return admin.Ping() == nil }, 60*time.Second, 2*time.Second,
		"MySQL did not become reachable")

	// Confirm ROW binlog is actually enabled — the listener needs it.
	var varName, logBin string
	require.NoError(t, admin.QueryRow("SHOW VARIABLES LIKE 'log_bin'").Scan(&varName, &logBin))
	require.Equal(t, "ON", logBin, "log_bin must be ON (start MySQL with --log-bin --binlog-format=ROW)")

	// Fresh schema for the test.
	_, err = admin.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	require.NoError(t, err)
	_, err = admin.Exec(fmt.Sprintf("CREATE DATABASE %s", dbName))
	require.NoError(t, err)
	_, err = admin.Exec(fmt.Sprintf(
		"CREATE TABLE %s.%s (id INT PRIMARY KEY, name VARCHAR(64), qty INT)", dbName, table))
	require.NoError(t, err)

	// Collect events from the listener into a channel via the existing MockEventHandler.
	events := make(chan *BinlogEvent, 100)
	handler := &MockEventHandler{
		handleFunc: func(_ context.Context, ev *BinlogEvent) error {
			if ev.Database == dbName && ev.Table == table {
				events <- ev
			}
			return nil
		},
	}

	settings := &Settings{
		Host:         host,
		Port:         portNum,
		User:         user,
		Password:     pass,
		DatabaseName: dbName,
	}
	listener := NewMySQLBinlogListener(settings, log.RootLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, listener.Initialize(ctx), "listener Initialize failed")

	handlerSettings := &HandlerSettings{
		ServerID:      1001, // must differ from the server's --server-id
		Tables:        []string{table},
		IncludeSchema: true, // key row data by column name
		EventTypes:    "ALL",
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if serr := listener.StartListening(ctx, handlerSettings, handler); serr != nil {
			t.Logf("StartListening returned: %v", serr)
		}
	}()

	// Give the syncer time to register the current binlog position before we
	// generate changes (it starts from "now" when no file/pos is configured).
	time.Sleep(4 * time.Second)

	// Drive one of each DML type.
	_, err = admin.Exec(fmt.Sprintf("INSERT INTO %s.%s (id, name, qty) VALUES (1, 'widget', 5)", dbName, table))
	require.NoError(t, err)
	_, err = admin.Exec(fmt.Sprintf("UPDATE %s.%s SET qty = 9 WHERE id = 1", dbName, table))
	require.NoError(t, err)
	_, err = admin.Exec(fmt.Sprintf("DELETE FROM %s.%s WHERE id = 1", dbName, table))
	require.NoError(t, err)

	// Gather events until we've seen INSERT, UPDATE, DELETE (or time out).
	var insertEv, updateEv, deleteEv *BinlogEvent
	deadline := time.After(60 * time.Second)
	for insertEv == nil || updateEv == nil || deleteEv == nil {
		select {
		case ev := <-events:
			switch ev.Type {
			case "INSERT":
				if insertEv == nil {
					insertEv = ev
				}
			case "UPDATE":
				if updateEv == nil {
					updateEv = ev
				}
			case "DELETE":
				if deleteEv == nil {
					deleteEv = ev
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for CDC events (insert=%v update=%v delete=%v)",
				insertEv != nil, updateEv != nil, deleteEv != nil)
		}
	}

	cancel()
	wg.Wait()

	// INSERT: new row image present, keyed by column name.
	require.NotNil(t, insertEv)
	assert.Equal(t, dbName, insertEv.Database)
	assert.Equal(t, table, insertEv.Table)
	assert.EqualValues(t, 1, toInt64(insertEv.Data["id"]))
	assert.Equal(t, "widget", toString(insertEv.Data["name"]))
	assert.EqualValues(t, 5, toInt64(insertEv.Data["qty"]))

	// UPDATE: after-image reflects the new value.
	require.NotNil(t, updateEv)
	assert.EqualValues(t, 1, toInt64(updateEv.Data["id"]))
	assert.EqualValues(t, 9, toInt64(updateEv.Data["qty"]))

	// DELETE: row image of the removed row.
	require.NotNil(t, deleteEv)
	assert.EqualValues(t, 1, toInt64(deleteEv.Data["id"]))

	t.Logf("captured INSERT/UPDATE/DELETE for %s.%s", dbName, table)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// toInt64 best-effort converts a binlog numeric value to int64 for assertions.
func toInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case int:
		return int64(n)
	case uint64:
		return int64(n)
	case uint32:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return -1
	}
}

func toString(v interface{}) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return fmt.Sprintf("%v", v)
	}
}

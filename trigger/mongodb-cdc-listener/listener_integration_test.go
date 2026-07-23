package mongodbcdclistener

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/project-flogo/core/support/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// These tests require a live MongoDB replica set and are gated behind MONGO_CDC_IT.
//
//	MONGO_CDC_IT=1 \
//	MONGO_CDC_TEST_URI="mongodb://localhost:57017/?directConnection=true" \
//	go test -run TestMongoDBCDCIntegration -v ./...
//
// Change streams require a replica set (or sharded cluster); a single-node
// replica set with directConnection=true is sufficient for testing.

// collector is a test EventHandler that funnels events to a channel.
type collector struct {
	events chan *ChangeEvent
}

func (c *collector) HandleEvent(_ context.Context, event *ChangeEvent) error {
	c.events <- event
	return nil
}

func testURI() string {
	if v := os.Getenv("MONGO_CDC_TEST_URI"); v != "" {
		return v
	}
	return "mongodb://localhost:57017/?directConnection=true"
}

func mustConnect(t *testing.T, uri string) *mongo.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	require.NoError(t, err, "connect to MongoDB")
	require.NoError(t, client.Ping(ctx, nil), "ping MongoDB")
	return client
}

func waitForEvent(t *testing.T, events chan *ChangeEvent, wantType string) *ChangeEvent {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Type == wantType {
				return ev
			}
			t.Logf("skipping event of type %s while waiting for %s", ev.Type, wantType)
		case <-deadline:
			t.Fatalf("timed out waiting for %s event", wantType)
			return nil
		}
	}
}

func TestMongoDBCDCIntegration(t *testing.T) {
	if os.Getenv("MONGO_CDC_IT") == "" {
		t.Skip("set MONGO_CDC_IT=1 and run a MongoDB replica set to enable this test")
	}

	uri := testURI()
	dbName := "cdc_test"
	collName := "items"

	admin := mustConnect(t, uri)
	defer func() { _ = admin.Disconnect(context.Background()) }()

	ctx := context.Background()
	db := admin.Database(dbName)

	// Fresh collection with change-stream pre/post images enabled (for oldData).
	_ = db.Collection(collName).Drop(ctx)
	err := db.RunCommand(ctx, bson.D{
		{Key: "create", Value: collName},
		{Key: "changeStreamPreAndPostImages", Value: bson.D{{Key: "enabled", Value: true}}},
	}).Err()
	require.NoError(t, err, "create collection with pre/post images")
	coll := db.Collection(collName)

	// Start the listener watching this collection.
	settings := &Settings{ConnectionURI: uri, ConnectionTimeout: 15}
	handlerSettings := &HandlerSettings{
		Database:                 dbName,
		Collection:               collName,
		OperationTypes:           "ALL",
		FullDocument:             "updateLookup",
		FullDocumentBeforeChange: "whenAvailable",
	}
	require.NoError(t, settings.Validate())
	require.NoError(t, handlerSettings.Validate())

	listener := NewMongoDBCDCListener(settings, handlerSettings, log.RootLogger())
	require.NoError(t, listener.Prepare(ctx), "prepare listener")
	defer listener.Close(context.Background())

	sink := &collector{events: make(chan *ChangeEvent, 32)}
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	streamErr := make(chan error, 1)
	go func() { streamErr <- listener.Stream(streamCtx, sink) }()

	// Give the change stream a moment to open before generating changes.
	time.Sleep(2 * time.Second)

	// INSERT
	_, err = coll.InsertOne(ctx, bson.D{
		{Key: "_id", Value: "item-1"},
		{Key: "name", Value: "widget"},
		{Key: "qty", Value: 5},
		{Key: "active", Value: true},
	})
	require.NoError(t, err, "insert document")

	ins := waitForEvent(t, sink.events, "INSERT")
	assert.Equal(t, dbName, ins.Database)
	assert.Equal(t, collName, ins.Collection)
	require.NotNil(t, ins.Data, "insert data present")
	assert.Equal(t, "widget", ins.Data["name"])
	assert.EqualValues(t, 5, ins.Data["qty"])
	assert.Equal(t, true, ins.Data["active"])
	assert.Equal(t, "item-1", ins.DocumentKey["_id"])

	// UPDATE
	_, err = coll.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: "item-1"}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "qty", Value: 10}}}},
	)
	require.NoError(t, err, "update document")

	upd := waitForEvent(t, sink.events, "UPDATE")
	require.NotNil(t, upd.Data, "update full document present (updateLookup)")
	assert.EqualValues(t, 10, upd.Data["qty"])
	require.NotNil(t, upd.UpdatedFields, "update updatedFields present")
	assert.EqualValues(t, 10, upd.UpdatedFields["qty"])
	if upd.OldData != nil {
		assert.EqualValues(t, 5, upd.OldData["qty"], "pre-image qty before update")
	}

	// DELETE
	_, err = coll.DeleteOne(ctx, bson.D{{Key: "_id", Value: "item-1"}})
	require.NoError(t, err, "delete document")

	del := waitForEvent(t, sink.events, "DELETE")
	assert.Equal(t, "item-1", del.DocumentKey["_id"])
	if del.OldData != nil {
		assert.Equal(t, "widget", del.OldData["name"], "pre-image name before delete")
	}

	// Shut the stream down cleanly.
	cancelStream()
	select {
	case <-streamErr:
	case <-time.After(5 * time.Second):
		t.Log("stream did not return promptly after cancel")
	}

	// Cleanup.
	_ = coll.Drop(context.Background())
}

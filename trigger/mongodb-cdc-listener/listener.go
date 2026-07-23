package mongodbcdclistener

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/project-flogo/core/support/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ChangeEvent is a database-agnostic representation of a single MongoDB change.
type ChangeEvent struct {
	ID            string
	Type          string
	Database      string
	Collection    string
	DocumentKey   map[string]interface{}
	Timestamp     time.Time
	Data          map[string]interface{}
	OldData       map[string]interface{}
	UpdatedFields map[string]interface{}
	RemovedFields []string
	ResumeToken   string
	CorrelationID string
}

// EventHandler consumes decoded change events. Implemented by the Flogo adapter
// and by tests, so the listener can be exercised without the full Flogo runtime.
type EventHandler interface {
	HandleEvent(ctx context.Context, event *ChangeEvent) error
}

// MongoDBCDCListener streams changes from MongoDB using change streams.
type MongoDBCDCListener struct {
	settings        *Settings
	handlerSettings *HandlerSettings
	logger          log.Logger

	client      *mongo.Client
	stream      *mongo.ChangeStream
	resumeToken bson.Raw
}

// NewMongoDBCDCListener creates a listener from validated settings.
func NewMongoDBCDCListener(s *Settings, hs *HandlerSettings, logger log.Logger) *MongoDBCDCListener {
	return &MongoDBCDCListener{
		settings:        s,
		handlerSettings: hs,
		logger:          logger,
	}
}

// buildClientOptions assembles the mongo driver client options from settings.
func (l *MongoDBCDCListener) buildClientOptions() *options.ClientOptions {
	opts := options.Client()

	if strings.TrimSpace(l.settings.ConnectionURI) != "" {
		opts.ApplyURI(l.settings.ConnectionURI)
	} else {
		host := l.settings.Host
		port := l.settings.Port
		if port == 0 {
			port = 27017
		}
		uri := fmt.Sprintf("mongodb://%s:%d", host, port)
		opts.ApplyURI(uri)
		if l.settings.Username != "" {
			cred := options.Credential{
				Username: l.settings.Username,
				Password: l.settings.Password,
			}
			if l.settings.AuthSource != "" {
				cred.AuthSource = l.settings.AuthSource
			}
			if l.settings.AuthMechanism != "" {
				cred.AuthMechanism = l.settings.AuthMechanism
			}
			opts.SetAuth(cred)
		}
		if l.settings.ReplicaSet != "" {
			opts.SetReplicaSet(l.settings.ReplicaSet)
		}
	}

	if l.settings.TLSConfig {
		q := url.Values{}
		q.Set("tls", "true")
		if l.settings.TLSInsecure {
			q.Set("tlsInsecure", "true")
		}
		if l.settings.TLSCAFile != "" {
			q.Set("tlsCAFile", l.settings.TLSCAFile)
		}
		if l.settings.TLSCertificateKeyFile != "" {
			q.Set("tlsCertificateKeyFile", l.settings.TLSCertificateKeyFile)
		}
		// Re-apply as a URI query so the driver parses TLS files consistently.
		base := l.settings.ConnectionURI
		if base == "" {
			host := l.settings.Host
			port := l.settings.Port
			if port == 0 {
				port = 27017
			}
			base = fmt.Sprintf("mongodb://%s:%d", host, port)
		}
		sep := "?"
		if strings.Contains(base, "?") {
			sep = "&"
		}
		opts.ApplyURI(base + sep + q.Encode())
	}

	timeout := time.Duration(l.settings.ConnectionTimeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	opts.SetConnectTimeout(timeout)
	opts.SetServerSelectionTimeout(timeout)

	return opts
}

// Prepare connects the client and verifies connectivity.
func (l *MongoDBCDCListener) Prepare(ctx context.Context) error {
	client, err := mongo.Connect(ctx, l.buildClientOptions())
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	l.client = client
	l.logger.Infof("MongoDB CDC: connected (database=%q collection=%q)",
		l.handlerSettings.Database, l.handlerSettings.Collection)
	return nil
}

// buildChangeStreamOptions maps handler settings onto change stream options.
func (l *MongoDBCDCListener) buildChangeStreamOptions() *options.ChangeStreamOptions {
	opts := options.ChangeStream()

	switch strings.ToLower(strings.TrimSpace(l.handlerSettings.FullDocument)) {
	case "updatelookup":
		opts.SetFullDocument(options.UpdateLookup)
	case "whenavailable":
		opts.SetFullDocument(options.WhenAvailable)
	case "required":
		opts.SetFullDocument(options.Required)
	case "", "default":
		opts.SetFullDocument(options.Default)
	}

	switch strings.ToLower(strings.TrimSpace(l.handlerSettings.FullDocumentBeforeChange)) {
	case "whenavailable":
		opts.SetFullDocumentBeforeChange(options.WhenAvailable)
	case "required":
		opts.SetFullDocumentBeforeChange(options.Required)
	case "off":
		opts.SetFullDocumentBeforeChange(options.Off)
	}

	if l.handlerSettings.BatchSize > 0 {
		opts.SetBatchSize(int32(l.handlerSettings.BatchSize))
	}
	if d, err := time.ParseDuration(strings.TrimSpace(l.handlerSettings.MaxAwaitTime)); err == nil && d > 0 {
		opts.SetMaxAwaitTime(d)
	}
	if l.resumeToken != nil {
		opts.SetResumeAfter(l.resumeToken)
	}

	return opts
}

// buildPipeline builds an aggregation pipeline that filters on operation types.
func (l *MongoDBCDCListener) buildPipeline() mongo.Pipeline {
	set := l.handlerSettings.OperationTypeSet()
	if set["ALL"] {
		return mongo.Pipeline{}
	}
	ops := make(bson.A, 0, len(set))
	for op := range set {
		ops = append(ops, strings.ToLower(op))
	}
	return mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.D{{Key: "operationType", Value: bson.D{{Key: "$in", Value: ops}}}}}},
	}
}

// openStream opens the change stream at the configured scope.
func (l *MongoDBCDCListener) openStream(ctx context.Context) (*mongo.ChangeStream, error) {
	pipeline := l.buildPipeline()
	opts := l.buildChangeStreamOptions()

	db := strings.TrimSpace(l.handlerSettings.Database)
	coll := strings.TrimSpace(l.handlerSettings.Collection)

	switch {
	case coll != "":
		return l.client.Database(db).Collection(coll).Watch(ctx, pipeline, opts)
	case db != "":
		return l.client.Database(db).Watch(ctx, pipeline, opts)
	default:
		return l.client.Watch(ctx, pipeline, opts)
	}
}

// Stream opens the change stream and dispatches events until ctx is cancelled
// or an unrecoverable error occurs.
func (l *MongoDBCDCListener) Stream(ctx context.Context, handler EventHandler) error {
	stream, err := l.openStream(ctx)
	if err != nil {
		return fmt.Errorf("failed to open change stream: %w", err)
	}
	l.stream = stream
	defer func() { _ = stream.Close(context.Background()) }()

	l.logger.Infof("MongoDB CDC: change stream started (database=%q collection=%q)",
		l.handlerSettings.Database, l.handlerSettings.Collection)

	for stream.Next(ctx) {
		var raw bson.M
		if err := stream.Decode(&raw); err != nil {
			l.logger.Errorf("MongoDB CDC: failed to decode change event: %v", err)
			continue
		}

		event := l.toChangeEvent(raw)
		if event != nil {
			if err := handler.HandleEvent(ctx, event); err != nil {
				// Do not advance the resume token: the event is redelivered
				// when the trigger reconnects and resumes from the last token.
				return fmt.Errorf("handler error for %s on %s.%s: %w",
					event.Type, event.Database, event.Collection, err)
			}
		}
		// Advance the resume token only after the event has been handled
		// successfully (at-least-once delivery).
		l.resumeToken = stream.ResumeToken()
	}

	if err := stream.Err(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("change stream error: %w", err)
	}
	return ctx.Err()
}

// toChangeEvent converts a raw MongoDB change stream document into a ChangeEvent.
func (l *MongoDBCDCListener) toChangeEvent(raw bson.M) *ChangeEvent {
	opType, _ := raw["operationType"].(string)
	eventType := mapOperationType(opType)

	event := &ChangeEvent{
		ID:            l.generateID(),
		Type:          eventType,
		CorrelationID: l.generateID(),
	}

	if ns, ok := raw["ns"].(bson.M); ok {
		event.Database, _ = ns["db"].(string)
		event.Collection, _ = ns["coll"].(string)
	}

	if ct, ok := raw["clusterTime"].(primitive.Timestamp); ok {
		event.Timestamp = time.Unix(int64(ct.T), 0).UTC()
	} else {
		event.Timestamp = time.Now().UTC()
	}

	if dk, ok := raw["documentKey"].(bson.M); ok {
		event.DocumentKey = normalizeMap(dk)
	}

	if fd, ok := raw["fullDocument"].(bson.M); ok {
		event.Data = normalizeMap(fd)
	}
	if before, ok := raw["fullDocumentBeforeChange"].(bson.M); ok {
		event.OldData = normalizeMap(before)
	}

	if ud, ok := raw["updateDescription"].(bson.M); ok {
		if uf, ok := ud["updatedFields"].(bson.M); ok {
			event.UpdatedFields = normalizeMap(uf)
		}
		if rf, ok := ud["removedFields"].(primitive.A); ok {
			for _, f := range rf {
				if s, ok := f.(string); ok {
					event.RemovedFields = append(event.RemovedFields, s)
				}
			}
		}
	}

	if token := l.stream.ResumeToken(); token != nil {
		event.ResumeToken = token.String()
	}

	return event
}

// Close disconnects the MongoDB client.
func (l *MongoDBCDCListener) Close(ctx context.Context) error {
	if l.stream != nil {
		_ = l.stream.Close(ctx)
		l.stream = nil
	}
	if l.client != nil {
		err := l.client.Disconnect(ctx)
		l.client = nil
		return err
	}
	return nil
}

// ResumeToken returns the last successfully-processed resume token, so the
// trigger can persist it across reconnects.
func (l *MongoDBCDCListener) ResumeToken() bson.Raw {
	return l.resumeToken
}

// SeedResumeToken sets the resume token to start from, used by the trigger to
// resume a new listener from the position reached before a reconnect.
func (l *MongoDBCDCListener) SeedResumeToken(token bson.Raw) {
	if token != nil {
		l.resumeToken = token
	}
}

// mapOperationType normalizes a MongoDB operationType to an uppercase event type.
func mapOperationType(op string) string {
	switch strings.ToLower(op) {
	case "insert":
		return "INSERT"
	case "update":
		return "UPDATE"
	case "replace":
		return "REPLACE"
	case "delete":
		return "DELETE"
	case "drop":
		return "DROP"
	case "rename":
		return "RENAME"
	case "dropdatabase":
		return "DROP_DATABASE"
	case "invalidate":
		return "INVALIDATE"
	default:
		return strings.ToUpper(op)
	}
}

// normalizeMap recursively converts a BSON map into JSON-friendly Go types.
func normalizeMap(m bson.M) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = normalizeValue(v)
	}
	return out
}

// normalizeValue converts BSON primitive types into plain JSON-serialisable values.
func normalizeValue(v interface{}) interface{} {
	switch val := v.(type) {
	case bson.M:
		return normalizeMap(val)
	case bson.D:
		out := make(map[string]interface{}, len(val))
		for _, e := range val {
			out[e.Key] = normalizeValue(e.Value)
		}
		return out
	case primitive.A:
		arr := make([]interface{}, len(val))
		for i, e := range val {
			arr[i] = normalizeValue(e)
		}
		return arr
	case []interface{}:
		arr := make([]interface{}, len(val))
		for i, e := range val {
			arr[i] = normalizeValue(e)
		}
		return arr
	case primitive.ObjectID:
		return val.Hex()
	case primitive.DateTime:
		return val.Time().UTC().Format(time.RFC3339Nano)
	case primitive.Timestamp:
		return map[string]interface{}{"t": val.T, "i": val.I}
	case primitive.Binary:
		return base64.StdEncoding.EncodeToString(val.Data)
	case primitive.Decimal128:
		return val.String()
	case primitive.Null:
		return nil
	case primitive.Regex:
		return val.Pattern
	case primitive.Symbol:
		return string(val)
	case primitive.JavaScript:
		return string(val)
	default:
		return v
	}
}

// generateID returns a random hex identifier for events/correlation.
func (l *MongoDBCDCListener) generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("evt-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

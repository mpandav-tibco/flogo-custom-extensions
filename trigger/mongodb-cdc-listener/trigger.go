package mongodbcdclistener

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/log"
	"github.com/project-flogo/core/support/trace"
	"github.com/project-flogo/core/trigger"
	"go.mongodb.org/mongo-driver/bson"
)

var triggerMd = trigger.NewMetadata(&Settings{}, &HandlerSettings{}, &Output{})

func init() {
	_ = trigger.Register(&Trigger{}, &Factory{})
}

// Trigger is the MongoDB change-stream CDC trigger.
type Trigger struct {
	settings  *Settings
	handlers  []*handlerContext
	logger    log.Logger
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	mutex     sync.Mutex
	isStarted bool
}

type handlerContext struct {
	runner   trigger.Handler
	settings *HandlerSettings
	cursor   bson.Raw
}

// Factory creates MongoDB CDC triggers.
type Factory struct{}

// Metadata returns the trigger metadata.
func (*Factory) Metadata() *trigger.Metadata {
	return triggerMd
}

// New creates a new trigger instance from config.
func (*Factory) New(config *trigger.Config) (trigger.Trigger, error) {
	settings := &Settings{}
	if err := metadata.MapToStruct(config.Settings, settings, true); err != nil {
		return nil, fmt.Errorf("failed to parse trigger settings: %w", err)
	}
	if err := settings.Validate(); err != nil {
		return nil, fmt.Errorf("invalid trigger settings: %w", err)
	}
	return &Trigger{
		settings: settings,
		logger:   log.ChildLogger(log.RootLogger(), "mongodb-cdc-trigger"),
	}, nil
}

// Metadata returns the trigger metadata.
func (t *Trigger) Metadata() *trigger.Metadata {
	return triggerMd
}

// Initialize parses handler settings and prepares one listener per handler.
func (t *Trigger) Initialize(ctx trigger.InitContext) error {
	t.ctx, t.cancel = context.WithCancel(context.Background())
	if ctx.Logger() != nil {
		t.logger = ctx.Logger()
	}

	for _, h := range ctx.GetHandlers() {
		hs := &HandlerSettings{}
		if err := metadata.MapToStruct(h.Settings(), hs, true); err != nil {
			return fmt.Errorf("failed to parse handler settings: %w", err)
		}
		if err := hs.Validate(); err != nil {
			return fmt.Errorf("invalid handler settings: %w", err)
		}
		t.handlers = append(t.handlers, &handlerContext{
			runner:   h,
			settings: hs,
		})
	}

	if len(t.handlers) == 0 {
		return fmt.Errorf("mongodb-cdc-listener requires at least one handler")
	}

	t.logger.Infof("MongoDB CDC trigger initialized with %d handler(s)", len(t.handlers))
	return nil
}

// Start begins streaming for every handler.
func (t *Trigger) Start() error {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.isStarted {
		return fmt.Errorf("trigger already started")
	}

	for _, hc := range t.handlers {
		t.wg.Add(1)
		go t.runHandler(hc)
	}

	t.isStarted = true
	t.logger.Info("MongoDB CDC trigger started")
	return nil
}

// runHandler manages one handler's listener with automatic reconnection.
func (t *Trigger) runHandler(hc *handlerContext) {
	defer t.wg.Done()

	retryDelay := 5 * time.Second
	if d, err := time.ParseDuration(t.settings.RetryDelay); err == nil && d > 0 {
		retryDelay = d
	}
	maxAttempts := t.settings.MaxRetryAttempts
	if maxAttempts == 0 {
		maxAttempts = 5
	}

	flogoHandler := &flogoEventHandler{runner: hc.runner, logger: t.logger}
	attempt := 0

	for {
		select {
		case <-t.ctx.Done():
			return
		default:
		}

		listener := NewMongoDBCDCListener(t.settings, hc.settings, t.logger)
		// Resume from the last token reached before the previous disconnect so
		// no changes are missed across reconnects (at-least-once delivery).
		listener.SeedResumeToken(hc.cursor)

		err := func() error {
			if err := listener.Prepare(t.ctx); err != nil {
				return err
			}
			return listener.Stream(t.ctx, flogoHandler)
		}()
		hc.cursor = listener.ResumeToken()
		listener.Close(context.Background())

		if t.ctx.Err() != nil {
			return
		}
		if err == nil {
			return
		}

		attempt++
		t.logger.Errorf("MongoDB CDC: stream for database=%q collection=%q failed (attempt %d/%d): %v",
			hc.settings.Database, hc.settings.Collection, attempt, maxAttempts, err)
		if maxAttempts > 0 && attempt >= maxAttempts {
			t.logger.Errorf("MongoDB CDC: giving up after %d attempts", attempt)
			return
		}

		select {
		case <-t.ctx.Done():
			return
		case <-time.After(retryDelay):
		}
	}
}

// Stop gracefully shuts the trigger down.
func (t *Trigger) Stop() error {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if !t.isStarted {
		return nil
	}

	t.logger.Info("Stopping MongoDB CDC trigger...")
	if t.cancel != nil {
		t.cancel()
	}
	// The per-handler goroutines close their own listeners when Stream returns;
	// cancelling the context unblocks them and Wait joins them here. Closing the
	// listener from this goroutine would race the still-running change stream.
	t.wg.Wait()
	t.isStarted = false
	t.logger.Info("MongoDB CDC trigger stopped")
	return nil
}

// flogoEventHandler adapts decoded change events onto a Flogo flow invocation.
type flogoEventHandler struct {
	runner trigger.Handler
	logger log.Logger
}

// HandleEvent dispatches a change event to the associated Flogo flow.
func (h *flogoEventHandler) HandleEvent(ctx context.Context, event *ChangeEvent) error {
	var tracingCtx trace.TracingContext
	isNewTrace := false
	if trace.Enabled() {
		if existing := trace.ExtractTracingContext(ctx); existing != nil {
			tracingCtx = existing
		} else if tracer := trace.GetTracer(); tracer != nil {
			tc, err := tracer.StartTrace(trace.Config{
				Operation: "mongodb-cdc-event",
				Tags: map[string]interface{}{
					"event.type": event.Type,
					"database":   event.Database,
					"collection": event.Collection,
				},
				Logger: h.logger,
			}, nil)
			if err == nil {
				tracingCtx = tc
				ctx = trace.AppendTracingContext(ctx, tracingCtx)
				isNewTrace = true
			}
		}
	}

	output := &Output{
		EventID:       event.ID,
		EventType:     event.Type,
		Database:      event.Database,
		Collection:    event.Collection,
		DocumentKey:   event.DocumentKey,
		Timestamp:     event.Timestamp.Format(time.RFC3339Nano),
		Data:          event.Data,
		OldData:       event.OldData,
		UpdatedFields: event.UpdatedFields,
		RemovedFields: event.RemovedFields,
		ResumeToken:   event.ResumeToken,
		CorrelationID: event.CorrelationID,
	}

	h.logger.Debugf("MongoDB CDC: dispatching %s on %s.%s", event.Type, event.Database, event.Collection)

	_, err := h.runner.Handle(ctx, output)
	if tracingCtx != nil && isNewTrace {
		if finishErr := trace.GetTracer().FinishTrace(tracingCtx, err); finishErr != nil {
			h.logger.Warnf("failed to finish trace: %v", finishErr)
		}
	}
	if err != nil {
		return fmt.Errorf("failed to execute Flogo flow: %w", err)
	}
	return nil
}

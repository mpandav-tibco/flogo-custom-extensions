package mysqlbinloglistener

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/log"
	"github.com/project-flogo/core/support/trace"
	"github.com/project-flogo/core/trigger"
)

var triggerMd = trigger.NewMetadata(&Settings{}, &HandlerSettings{}, &Output{})

// Trigger implements the MySQL binlog streaming trigger
type Trigger struct {
	settings  *Settings
	handlers  []*Handler
	listener  *MySQLBinlogListener
	logger    log.Logger
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	isStarted bool
	mutex     sync.Mutex
	startTime time.Time // Trigger start time for metrics
}

// Handler represents a binlog stream handler
type Handler struct {
	runner   trigger.Handler
	settings *HandlerSettings
}

// FlogoEventHandler implements EventHandler interface for Flogo integration
type FlogoEventHandler struct {
	runner trigger.Handler
	logger log.Logger
}

// NewFlogoEventHandler creates a new Flogo event handler
func NewFlogoEventHandler(runner trigger.Handler, logger log.Logger) *FlogoEventHandler {
	return &FlogoEventHandler{
		runner: runner,
		logger: logger,
	}
}

// HandleEvent processes a binlog event and executes the associated Flogo flow
func (h *FlogoEventHandler) HandleEvent(ctx context.Context, event *BinlogEvent) error {
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
			h.logger.Debugf("Reusing existing trace for MySQL binlog event: %s", tracingCtx.TraceID())

			// Add additional tags to existing trace
			if tracingCtx.SetTags(map[string]interface{}{
				"event.type":     event.Type,
				"database":       event.Database,
				"table":          event.Table,
				"event.id":       event.ID,
				"binlog.file":    event.BinlogFile,
				"binlog.pos":     event.BinlogPos,
				"server.id":      event.ServerID,
				"correlation.id": event.CorrelationID,
			}) {
				h.logger.Debugf("Added MySQL binlog metadata to existing trace: %s", tracingCtx.TraceID())
			}
		} else {
			// Create new trace only if no existing trace context
			tracer := trace.GetTracer()
			if tracer != nil {
				traceConfig := trace.Config{
					Operation: "mysql-binlog-event",
					Tags: map[string]interface{}{
						"event.type":     event.Type,
						"database":       event.Database,
						"table":          event.Table,
						"event.id":       event.ID,
						"binlog.file":    event.BinlogFile,
						"binlog.pos":     event.BinlogPos,
						"server.id":      event.ServerID,
						"correlation.id": event.CorrelationID,
					},
					Logger: h.logger,
				}

				var err error
				tracingCtx, err = tracer.StartTrace(traceConfig, nil)
				if err != nil {
					h.logger.Warnf("Failed to start new trace: %v", err)
				} else {
					ctx = trace.AppendTracingContext(ctx, tracingCtx)
					isNewTrace = true
					h.logger.Debugf("Started new trace for MySQL binlog event: %s", tracingCtx.TraceID())
				}
			}
		}
	}

	// Convert BinlogEvent to Flogo Output
	// Ensure all values are JSON-serializable and Flogo-compatible
	output := &Output{
		EventID:       event.ID,
		EventType:     event.Type,
		Database:      event.Database,
		Table:         event.Table,
		Timestamp:     event.Timestamp.Format(time.RFC3339), // Convert time.Time to string in ISO format
		Data:          event.Data,
		Schema:        event.Schema, // Include schema information when available
		BinlogFile:    event.BinlogFile,
		BinlogPos:     int(event.BinlogPos), // Convert uint32 to int for Flogo output
		ServerID:      int(event.ServerID),  // Convert uint32 to int for Flogo output
		GTID:          event.GTID,
		CorrelationID: event.CorrelationID,
	}

	h.logger.Debugf("Processing MySQL binlog event: %s on %s.%s (TraceID: %s)",
		event.Type, event.Database, event.Table,
		func() string {
			if tracingCtx != nil {
				return tracingCtx.TraceID()
			}
			return "none"
		}())

	h.logger.Debugf("Sending trigger output data: eventID=%s, eventType=%s, database=%s, table=%s",
		output.EventID, output.EventType, output.Database, output.Table)

	// Execute the Flogo flow using Handle method
	_, err := h.runner.Handle(ctx, output)

	// Finish tracing only if we created a new trace (don't finish reused traces)
	if tracingCtx != nil && isNewTrace {
		if finishErr := trace.GetTracer().FinishTrace(tracingCtx, err); finishErr != nil {
			h.logger.Warnf("Failed to finish trace: %v", finishErr)
		}

		if err == nil {
			h.logger.Debugf("Flogo flow executed successfully for event %s (TraceID: %s)",
				event.ID, tracingCtx.TraceID())
		}
	} else if tracingCtx != nil {
		// For reused traces, just log additional information
		if err == nil {
			tracingCtx.LogKV(map[string]interface{}{
				"mysql.event.status": "success",
				"mysql.event.id":     event.ID,
			})
			h.logger.Debugf("Flogo flow executed successfully for event %s (Reused TraceID: %s)",
				event.ID, tracingCtx.TraceID())
		} else {
			tracingCtx.LogKV(map[string]interface{}{
				"mysql.event.status": "error",
				"mysql.event.error":  err.Error(),
				"mysql.event.id":     event.ID,
			})
		}
	}

	if err != nil {
		return fmt.Errorf("failed to execute Flogo flow: %v", err)
	}

	return nil
}

// Factory is a trigger factory
type Factory struct{}

// Metadata returns the trigger's metadata
func (*Factory) Metadata() *trigger.Metadata {
	return triggerMd
}

// New creates a new trigger instance
func (*Factory) New(config *trigger.Config) (trigger.Trigger, error) {
	settings := &Settings{}

	err := metadata.MapToStruct(config.Settings, settings, true)
	if err != nil {
		return nil, fmt.Errorf("failed to parse trigger settings: %v", err)
	}

	if err := settings.Validate(); err != nil {
		return nil, fmt.Errorf("invalid trigger settings: %v", err)
	}

	return &Trigger{
		settings: settings,
		logger:   log.ChildLogger(log.RootLogger(), "mysql-binlog-trigger"),
	}, nil
}

// Initialize initializes the trigger
func (t *Trigger) Initialize(ctx trigger.InitContext) error {
	t.ctx, t.cancel = context.WithCancel(context.Background())

	// Store the handlers from the init context
	for _, handler := range ctx.GetHandlers() {
		t.handlers = append(t.handlers, &Handler{
			runner: handler,
		})
	}

	// Create MySQL binlog listener
	t.listener = NewMySQLBinlogListener(t.settings, t.logger)

	// Initialize the listener
	if err := t.listener.Initialize(t.ctx); err != nil {
		return fmt.Errorf("failed to initialize MySQL binlog listener: %v", err)
	}

	// Parse handlers
	for _, h := range t.handlers {
		handlerSettings := &HandlerSettings{}
		err := metadata.MapToStruct(h.runner.Settings(), handlerSettings, true)
		if err != nil {
			return fmt.Errorf("failed to parse handler settings: %v", err)
		}

		if err := handlerSettings.Validate(); err != nil {
			return fmt.Errorf("invalid handler settings: %v", err)
		}

		h.settings = handlerSettings
	}

	t.logger.Info("MySQL binlog trigger initialized successfully")
	return nil
}

// Metadata returns the trigger metadata
func (t *Trigger) Metadata() *trigger.Metadata {
	return triggerMd
}

// Start starts the trigger
func (t *Trigger) Start() error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if t.isStarted {
		return fmt.Errorf("MySQL binlog trigger is already started")
	}

	// Record start time for metrics
	t.startTime = time.Now()

	// Start tracing for trigger lifecycle
	var tracingCtx trace.TracingContext
	if trace.Enabled() {
		tracer := trace.GetTracer()
		if tracer != nil {
			traceConfig := trace.Config{
				Operation: "mysql-binlog-trigger-start",
				Tags: map[string]interface{}{
					"trigger.type":   "mysql-binlog-listener",
					"host":           t.settings.Host,
					"port":           t.settings.Port,
					"database":       t.settings.DatabaseName,
					"handlers.count": len(t.handlers),
				},
				Logger: t.logger,
			}

			var err error
			tracingCtx, err = tracer.StartTrace(traceConfig, nil)
			if err != nil {
				t.logger.Warnf("Failed to start trace for trigger start: %v", err)
			} else {
				t.logger.Debugf("Started trace for MySQL binlog trigger start: %s", tracingCtx.TraceID())
			}
		}
	}

	t.logger.Info("Starting MySQL binlog trigger...")

	// Start binlog streaming for each handler
	for _, h := range t.handlers {
		eventHandler := NewFlogoEventHandler(h.runner, t.logger)

		// Start listening with this handler's settings
		if err := t.listener.StartListening(t.ctx, h.settings, eventHandler); err != nil {
			// Finish trace with error
			if tracingCtx != nil {
				trace.GetTracer().FinishTrace(tracingCtx, err)
			}
			return fmt.Errorf("failed to start binlog streaming for handler: %v", err)
		}
	}

	// Start health monitoring
	t.wg.Add(1)
	go t.monitorHealth()

	// Start heartbeat if enabled
	if t.settings.EnableHeartbeat {
		t.wg.Add(1)
		go t.heartbeat()
	}

	t.isStarted = true

	// Finish trace successfully
	if tracingCtx != nil {
		if err := trace.GetTracer().FinishTrace(tracingCtx, nil); err != nil {
			t.logger.Warnf("Failed to finish trace for trigger start: %v", err)
		}
	}

	t.logger.Info("MySQL binlog trigger started successfully")

	return nil
}

// Stop stops the trigger
func (t *Trigger) Stop() error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if !t.isStarted {
		return nil
	}

	t.logger.Info("Stopping MySQL binlog trigger...")

	// Cancel context
	if t.cancel != nil {
		t.cancel()
	}

	// Stop the listener
	if t.listener != nil {
		if err := t.listener.Stop(); err != nil {
			t.logger.Warnf("Error stopping MySQL binlog listener: %v", err)
		}
	}

	// Wait for monitoring goroutines to finish
	t.wg.Wait()

	t.isStarted = false
	t.logger.Info("MySQL binlog trigger stopped successfully")

	return nil
}

// monitorHealth periodically checks the health of the MySQL connection
func (t *Trigger) monitorHealth() {
	defer t.wg.Done()

	healthCheckInterval, err := time.ParseDuration(t.settings.HealthCheckInterval)
	if err != nil {
		healthCheckInterval = 60 * time.Second
	}

	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	healthCheckCount := 0
	consecutiveFailures := 0

	t.logger.Infof("Health monitoring started with interval: %v", healthCheckInterval)

	for {
		select {
		case <-ticker.C:
			healthCheckCount++

			if err := t.listener.HealthCheck(); err != nil {
				consecutiveFailures++
				t.logger.Errorf("MySQL health check failed (attempt %d): %v", consecutiveFailures, err)
			} else {
				if consecutiveFailures > 0 {
					// Log recovery after failures
					t.logger.Infof("MySQL health check recovered after %d failures", consecutiveFailures)
					consecutiveFailures = 0
				} else {
					// Log debug for successful checks
					t.logger.Debugf("MySQL health check passed")
				}

				// Periodic health status summary (every 10 checks)
				if healthCheckCount%10 == 0 {
					memoryStats := getMemoryStats()
					uptime := time.Since(t.startTime)
					t.logger.Infof("MySQL connection healthy - completed %d health checks [uptime=%v memory_mb=%d goroutines=%d gc_runs=%d]",
						healthCheckCount,
						uptime.Truncate(time.Second),
						memoryStats["alloc_mb"],
						memoryStats["goroutines"],
						memoryStats["gc_runs"])
				}
			}

		case <-t.ctx.Done():
			t.logger.Info("Health monitoring stopped")
			return
		}
	}
}

// heartbeat sends periodic heartbeat signals to verify trigger is alive
func (t *Trigger) heartbeat() {
	defer t.wg.Done()

	heartbeatInterval, err := time.ParseDuration(t.settings.HeartbeatInterval)
	if err != nil {
		heartbeatInterval = 30 * time.Second // Default 30 seconds to match metadata
	}

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	t.logger.Infof("Heartbeat monitoring started with interval: %v", heartbeatInterval)

	for {
		select {
		case <-ticker.C:
			memoryStats := getMemoryStats()
			uptime := time.Since(t.startTime)
			t.logger.Infof("MySQL binlog trigger heartbeat - trigger is alive [uptime=%v memory_mb=%d goroutines=%d]",
				uptime.Truncate(time.Second),
				memoryStats["alloc_mb"],
				memoryStats["goroutines"])

		case <-t.ctx.Done():
			t.logger.Info("Heartbeat monitoring stopped")
			return
		}
	}
}

// getMemoryStats returns current memory usage statistics
func getMemoryStats() map[string]interface{} {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return map[string]interface{}{
		"alloc_mb":       bToMb(m.Alloc),
		"total_alloc_mb": bToMb(m.TotalAlloc),
		"sys_mb":         bToMb(m.Sys),
		"gc_runs":        uint64(m.NumGC), // Convert uint32 to uint64
		"goroutines":     runtime.NumGoroutine(),
	}
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}

// init registers the trigger factory
func init() {
	_ = trigger.Register(&Trigger{}, &Factory{})
}

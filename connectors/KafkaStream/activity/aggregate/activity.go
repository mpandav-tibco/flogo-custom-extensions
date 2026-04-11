package aggregate

import (
	"fmt"
	"strconv"
	"time"

	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/coerce"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/log"

	kafkastream "github.com/milindpandav/flogo-extensions/kafkastream"
	"github.com/milindpandav/flogo-extensions/kafkastream/window"
)

var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})

func init() {
	_ = activity.Register(&Activity{}, New)
}

// Activity implements the Kafka Stream Aggregate activity.
// It feeds each incoming Kafka message's numeric field into a named window store
// and emits an aggregated result (sum/count/avg/min/max) when the window closes.
// Enterprise features: event-time processing, late-event DLQ routing, overflow
// back-pressure, MessageID deduplication, idle-timeout, keyed cardinality limits.
type Activity struct {
	settings *Settings
	logger   log.Logger
}

// Metadata returns the activity's metadata.
func (a *Activity) Metadata() *activity.Metadata {
	return activityMd
}

// New creates and initialises a new aggregate activity instance.
func New(ctx activity.InitContext) (activity.Activity, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(ctx.Settings(), s, true); err != nil {
		return nil, fmt.Errorf("kafka-stream/aggregate: failed to map settings: %w", err)
	}

	validTypes := map[string]bool{
		"TumblingTime": true, "TumblingCount": true,
		"SlidingTime": true, "SlidingCount": true,
	}
	if !validTypes[s.WindowType] {
		return nil, fmt.Errorf("kafka-stream/aggregate: unsupported windowType %q", s.WindowType)
	}

	validFuncs := map[string]bool{
		"sum": true, "count": true, "avg": true, "min": true, "max": true,
	}
	if !validFuncs[s.Function] {
		return nil, fmt.Errorf("kafka-stream/aggregate: unsupported function %q", s.Function)
	}

	if s.WindowSize <= 0 {
		return nil, fmt.Errorf("kafka-stream/aggregate: windowSize must be > 0, got %d", s.WindowSize)
	}

	validOverflow := map[string]bool{
		"": true, "drop_oldest": true, "drop_newest": true, "error": true,
	}
	if !validOverflow[s.OverflowPolicy] {
		return nil, fmt.Errorf("kafka-stream/aggregate: unsupported overflowPolicy %q", s.OverflowPolicy)
	}

	// Pre-register the base window store
	cfg := buildWindowConfig(s, s.WindowName)
	if _, err := kafkastream.GetOrCreateWindowStore(cfg); err != nil {
		return nil, fmt.Errorf("kafka-stream/aggregate: failed to initialise window %q: %w", s.WindowName, err)
	}

	// Restore persisted state if a persist path is configured.
	if s.PersistPath != "" {
		if err := kafkastream.RestoreStateFrom(s.PersistPath); err != nil {
			// Log and continue — bad persist file should not prevent startup.
			logger := ctx.Logger()
			logger.Warnf("kafka-stream/aggregate: state restore from %q failed (ignored): %v", s.PersistPath, err)
		}
	}

	logger := ctx.Logger()
	logger.Infof("Kafka Stream Aggregate initialised: window=%q type=%s size=%d fn=%s eventTimeField=%q overflow=%s persistPath=%q",
		s.WindowName, s.WindowType, s.WindowSize, s.Function, s.EventTimeField, s.OverflowPolicy, s.PersistPath)

	return &Activity{settings: s, logger: logger}, nil
}

// Eval feeds the current message into the appropriate window store and emits
// an aggregate result when the window closes.
func (a *Activity) Eval(ctx activity.Context) (done bool, err error) {
	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("kafka-stream/aggregate: failed to get input: %w", err)
	}

	// --- Extract numeric value ---
	if input.ValueField == "" {
		return false, fmt.Errorf("kafka-stream/aggregate: valueField input must not be empty")
	}
	rawValue, ok := input.Message[input.ValueField]
	if !ok {
		return false, fmt.Errorf("kafka-stream/aggregate: field %q not found in message", input.ValueField)
	}
	value, err := coerce.ToFloat64(rawValue)
	if err != nil {
		return false, fmt.Errorf("kafka-stream/aggregate: field %q cannot be coerced to float64: %w", input.ValueField, err)
	}

	// --- Extract grouping key ---
	key := ""
	if input.KeyField != "" {
		if rawKey, exists := input.Message[input.KeyField]; exists {
			key, _ = coerce.ToString(rawKey)
		}
	}

	// --- Extract event time ---
	// Priority: eventTimestamp input (pre-resolved int64 Unix-ms)
	//           > eventTimeField input
	//           > eventTimeField setting
	//           > wall-clock (fallback)
	var eventTime time.Time
	if input.EventTimestamp > 0 {
		eventTime = time.UnixMilli(input.EventTimestamp)
	} else {
		eventTimeField := input.EventTimeField
		if eventTimeField == "" {
			eventTimeField = a.settings.EventTimeField
		}
		eventTime = extractEventTime(input.Message, eventTimeField)
	}

	// --- Extract MessageID for deduplication ---
	messageID := ""
	if input.MessageIDField != "" {
		if raw, exists := input.Message[input.MessageIDField]; exists {
			messageID, _ = coerce.ToString(raw)
		}
	}

	// --- Resolve effective window name (keyed) ---
	windowName := a.settings.WindowName
	if key != "" {
		windowName = windowName + ":" + key
	}

	// --- Retrieve or lazily create the window store ---
	store, storeExists := kafkastream.GetWindowStore(windowName)
	if !storeExists {
		cfg := buildWindowConfig(a.settings, windowName)
		store, err = kafkastream.GetOrCreateWindowStore(cfg)
		if err != nil {
			return false, fmt.Errorf("kafka-stream/aggregate: failed to create keyed window %q: %w", windowName, err)
		}
	}

	// --- Idle-timeout check (emit partial result before processing new event) ---
	// If the window has been idle longer than idleTimeoutMs, CheckIdle resets the
	// buffer and returns the accumulated partial result. We emit that result now
	// and add the current event as the first event of the fresh window.
	if idleResult, idleClosed := store.CheckIdle(); idleClosed {
		a.logger.Infof("Window %q auto-closed due to idle timeout: %s=%.4f count=%d key=%q",
			windowName, a.settings.Function, idleResult.Value, idleResult.Count, key)
		// Seed the fresh window with the incoming event; propagate late/error signals.
		_, _, idleLate, idleAddErr := store.Add(window.WindowEvent{
			Value:     value,
			Timestamp: eventTime,
			Key:       key,
			MessageID: messageID,
		})
		if idleAddErr != nil {
			return false, fmt.Errorf("kafka-stream/aggregate: window.Add after idle-close failed for %q: %w", windowName, idleAddErr)
		}
		idleOutput := &Output{
			WindowClosed: true,
			Result:       idleResult.Value,
			Count:        idleResult.Count,
			WindowName:   windowName,
			Key:          key,
			DroppedCount: idleResult.DroppedCount,
		}
		if idleLate != nil {
			a.logger.Warnf("Late event after idle-close for %q: %s", windowName, idleLate.Reason)
			idleOutput.LateEvent = true
			idleOutput.LateReason = idleLate.Reason
		}
		if err := ctx.SetOutputObject(idleOutput); err != nil {
			return false, fmt.Errorf("kafka-stream/aggregate: failed to set idle-timeout output: %w", err)
		}
		a.setTracingTags(ctx, idleOutput)
		return true, nil
	}

	// --- Add event to window ---
	result, closed, late, addErr := store.Add(window.WindowEvent{
		Value:     value,
		Timestamp: eventTime,
		Key:       key,
		MessageID: messageID,
	})

	output := &Output{
		WindowClosed: closed,
		WindowName:   windowName,
		Key:          key,
	}

	// --- Late-event DLQ routing ---
	if late != nil {
		a.logger.Warnf("Late event routed to DLQ: window=%q watermark=%s eventTime=%s reason=%q",
			windowName, late.Watermark, eventTime, late.Reason)
		output.LateEvent = true
		output.LateReason = late.Reason
		if err := ctx.SetOutputObject(output); err != nil {
			return false, fmt.Errorf("kafka-stream/aggregate: failed to set output for late event: %w", err)
		}
		a.setTracingTags(ctx, output)
		return true, nil
	}

	// --- Hard error (e.g. overflow=error policy) ---
	if addErr != nil {
		return false, fmt.Errorf("kafka-stream/aggregate: window.Add failed for %q: %w", windowName, addErr)
	}

	if result != nil {
		output.Result = result.Value
		output.Count = result.Count
		output.DroppedCount = result.DroppedCount
		if closed {
			a.logger.Infof("Window %q closed: %s=%.4f count=%d droppedCount=%d lateCount=%d key=%q",
				windowName, a.settings.Function, result.Value, result.Count,
				result.DroppedCount, result.LateEventCount, key)
		}
	}

	if err := ctx.SetOutputObject(output); err != nil {
		return false, fmt.Errorf("kafka-stream/aggregate: failed to set output: %w", err)
	}
	a.setTracingTags(ctx, output)

	// Periodic state persistence (opt-in, only when PersistPath is configured).
	// Input path overrides settings path if provided.
	persistPath := input.PersistPath
	if persistPath == "" {
		persistPath = a.settings.PersistPath
	}
	if persistPath != "" && a.settings.PersistEveryN > 0 {
		n := kafkastream.IncrPersistCounter()
		if n%a.settings.PersistEveryN == 0 {
			if err := kafkastream.SaveStateTo(persistPath); err != nil {
				a.logger.Warnf("kafka-stream/aggregate: periodic state save failed: %v", err)
			} else if a.logger.DebugEnabled() {
				a.logger.Debugf("kafka-stream/aggregate: state persisted to %q (n=%d)", persistPath, n)
			}
		}
	}

	return true, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// setTracingTags attaches aggregate-specific tags to the engine-managed span.
func (a *Activity) setTracingTags(ctx activity.Context, out *Output) {
	if tc := ctx.GetTracingContext(); tc != nil {
		tc.SetTag("aggregate.window", out.WindowName)
		tc.SetTag("aggregate.function", a.settings.Function)
		tc.SetTag("aggregate.window_closed", out.WindowClosed)
		if out.Key != "" {
			tc.SetTag("aggregate.key", out.Key)
		}
		if out.WindowClosed {
			tc.SetTag("aggregate.result", out.Result)
			tc.SetTag("aggregate.count", out.Count)
		}
		if out.LateEvent {
			tc.SetTag("aggregate.late_event", true)
			tc.SetTag("aggregate.late_reason", out.LateReason)
		}
	}
}

// buildWindowConfig constructs a WindowConfig from an activity settings struct.
func buildWindowConfig(s *Settings, name string) window.WindowConfig {
	return window.WindowConfig{
		Name:            name,
		Type:            window.WindowType(s.WindowType),
		Size:            s.WindowSize,
		Function:        window.AggregateFunc(s.Function),
		EventTimeField:  s.EventTimeField,
		AllowedLateness: s.AllowedLateness,
		MaxBufferSize:   s.MaxBufferSize,
		OverflowPolicy:  window.OverflowPolicy(s.OverflowPolicy),
		IdleTimeoutMs:   s.IdleTimeoutMs,
		MaxKeys:         s.MaxKeys,
	}
}

// extractEventTime returns the event timestamp from the message field identified
// by fieldName. Supports Unix-ms int64/float64 and RFC-3339 string.
// Falls back to wall-clock time when fieldName is empty or the field is absent.
func extractEventTime(msg map[string]interface{}, fieldName string) time.Time {
	if fieldName == "" {
		return time.Now()
	}
	raw, ok := msg[fieldName]
	if !ok {
		return time.Now()
	}
	switch v := raw.(type) {
	case int64:
		return time.UnixMilli(v)
	case float64:
		return time.UnixMilli(int64(v))
	case string:
		// Try RFC-3339 first
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
		// Try Unix-ms as string
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			return time.UnixMilli(ms)
		}
	}
	return time.Now()
}

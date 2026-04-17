package aggregate

import "github.com/project-flogo/core/data/coerce"

// Settings hold per-activity-instance configuration, set at design time.
type Settings struct {
	WindowName string `md:"windowName,required"`
	WindowType string `md:"windowType,required"`
	WindowSize int64  `md:"windowSize,required"`
	Function   string `md:"function,required"`

	// Enterprise settings -----------------------------------------------

	// EventTimeField is the message field that holds the event timestamp.
	// Accepted formats: Unix-ms (int64 or float64) or RFC-3339 string.
	// When empty the activity falls back to wall-clock time (dev/test only).
	EventTimeField string `md:"eventTimeField"`

	// AllowedLateness is the maximum age (ms) of a late event that is still
	// accepted into the window. Events older than this are routed to the
	// lateEvent output. 0 = reject all late events immediately.
	AllowedLateness int64 `md:"allowedLateness"`

	// MaxBufferSize caps the number of values buffered per window. 0 = unlimited.
	MaxBufferSize int64 `md:"maxBufferSize"`

	// OverflowPolicy controls what to do when MaxBufferSize is reached.
	// Accepted values: "drop_oldest" (default), "drop_newest", "error".
	OverflowPolicy string `md:"overflowPolicy"`

	// IdleTimeoutMs: a keyed sub-window that receives no event for this many
	// milliseconds is auto-closed and its partial result emitted. 0 = disabled.
	IdleTimeoutMs int64 `md:"idleTimeoutMs"`

	// MaxKeys caps the number of distinct keyed sub-windows. 0 = unlimited.
	MaxKeys int64 `md:"maxKeys"`

	// PersistPath is the file path for window state snapshots (gob-encoded).
	// On startup the activity restores from this file if it exists.
	// On shutdown (or every PersistEveryN messages) the state is flushed.
	// Leave empty to disable persistence (default).
	PersistPath string `md:"persistPath"`

	// PersistEveryN triggers a background snapshot every N messages added to
	// any window. 0 = flush only on graceful shutdown. Default: 0.
	PersistEveryN int64 `md:"persistEveryN"`
}

// Input is the per-execution input, mapped from the Flogo flow.
type Input struct {
	Message        map[string]interface{} `md:"message"`
	ValueField     string                 `md:"valueField"`
	KeyField       string                 `md:"keyField"`
	EventTimeField string                 `md:"eventTimeField"`
	// EventTimestamp is a resolved Unix-ms timestamp (int64). When non-zero it
	// takes priority over EventTimeField and the eventTimeField setting.
	// Map any external timestamp here — e.g. a value extracted from Kafka
	// headers, a flow variable, or any pre-computed int64 expression.
	EventTimestamp int64  `md:"eventTimestamp"`
	MessageIDField string `md:"messageIDField"`
	PersistPath    string `md:"persistPath"`
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"message":        i.Message,
		"valueField":     i.ValueField,
		"keyField":       i.KeyField,
		"eventTimeField": i.EventTimeField,
		"eventTimestamp": i.EventTimestamp,
		"messageIDField": i.MessageIDField,
		"persistPath":    i.PersistPath,
	}
}

func (i *Input) FromMap(values map[string]interface{}) error {
	var err error
	i.Message, err = coerce.ToObject(values["message"])
	if err != nil {
		return err
	}
	i.ValueField, err = coerce.ToString(values["valueField"])
	if err != nil {
		return err
	}
	i.KeyField, err = coerce.ToString(values["keyField"])
	if err != nil {
		return err
	}
	i.EventTimeField, err = coerce.ToString(values["eventTimeField"])
	if err != nil {
		return err
	}
	i.EventTimestamp, err = coerce.ToInt64(values["eventTimestamp"])
	if err != nil {
		return err
	}
	i.MessageIDField, err = coerce.ToString(values["messageIDField"])
	if err != nil {
		return err
	}
	i.PersistPath, err = coerce.ToString(values["persistPath"])
	return err
}

// Output is the per-execution output written back to the Flogo flow.
type Output struct {
	WindowClosed bool    `md:"windowClosed"`
	Result       float64 `md:"result"`
	Count        int64   `md:"count"`
	WindowName   string  `md:"windowName"`
	Key          string  `md:"key"`

	// Enterprise outputs ------------------------------------------------

	// LateEvent is true when the current event was routed to the DLQ
	// because it was beyond AllowedLateness.
	LateEvent bool `md:"lateEvent"`

	// LateReason describes why the event was classified as late.
	LateReason string `md:"lateReason"`

	// DroppedCount is the number of events dropped (overflow) in the
	// last closed window.
	DroppedCount int64 `md:"droppedCount"`

	// LateEventCount is the number of late-but-accepted events (within
	// AllowedLateness) aggregated in the last closed window. Useful for
	// downstream SLA monitoring without enabling a full DLQ pipeline.
	LateEventCount int64 `md:"lateEventCount"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"windowClosed":   o.WindowClosed,
		"result":         o.Result,
		"count":          o.Count,
		"windowName":     o.WindowName,
		"key":            o.Key,
		"lateEvent":      o.LateEvent,
		"lateReason":     o.LateReason,
		"droppedCount":   o.DroppedCount,
		"lateEventCount": o.LateEventCount,
	}
}

func (o *Output) FromMap(values map[string]interface{}) error {
	var err error
	o.WindowClosed, err = coerce.ToBool(values["windowClosed"])
	if err != nil {
		return err
	}
	o.Result, err = coerce.ToFloat64(values["result"])
	if err != nil {
		return err
	}
	o.Count, err = coerce.ToInt64(values["count"])
	if err != nil {
		return err
	}
	o.WindowName, err = coerce.ToString(values["windowName"])
	if err != nil {
		return err
	}
	o.Key, err = coerce.ToString(values["key"])
	if err != nil {
		return err
	}
	o.LateEvent, err = coerce.ToBool(values["lateEvent"])
	if err != nil {
		return err
	}
	o.LateReason, err = coerce.ToString(values["lateReason"])
	if err != nil {
		return err
	}
	o.DroppedCount, err = coerce.ToInt64(values["droppedCount"])
	if err != nil {
		return err
	}
	o.LateEventCount, err = coerce.ToInt64(values["lateEventCount"])
	return err
}

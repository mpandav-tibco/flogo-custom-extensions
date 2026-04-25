package integration

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/project-flogo/core/support/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kafkastream "github.com/mpandav-tibco/flogo-extensions/kafkastream"
	aggregate "github.com/mpandav-tibco/flogo-extensions/kafkastream/activity/aggregate"
	filteract "github.com/mpandav-tibco/flogo-extensions/kafkastream/activity/filter"
)

// produceRaw sends raw bytes to the test topic without JSON marshalling.
func produceRaw(t *testing.T, p sarama.SyncProducer, payload []byte) {
	t.Helper()
	_, _, err := p.SendMessage(&sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.ByteEncoder(payload),
	})
	require.NoError(t, err)
}

// consumeNRaw returns raw Kafka message bytes (not parsed as JSON).
func consumeNRaw(t *testing.T, pc sarama.PartitionConsumer, n int, timeout time.Duration) [][]byte {
	t.Helper()
	var results [][]byte
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for len(results) < n {
		select {
		case msg := <-pc.Messages():
			cp := make([]byte, len(msg.Value))
			copy(cp, msg.Value)
			results = append(results, cp)
		case <-deadline.C:
			t.Fatalf("timed out waiting for raw messages (got %d, want %d)", len(results), n)
		}
	}
	return results
}

// Neg-1: unreachable broker — producer connection must fail.
func TestNeg_UnreachableBroker_ProducerFails(t *testing.T) {
	cfg := sarama.NewConfig()
	cfg.Producer.Return.Successes = true
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Net.DialTimeout = 2 * time.Second
	cfg.Net.ReadTimeout = 2 * time.Second
	cfg.Net.WriteTimeout = 2 * time.Second
	cfg.Metadata.Retry.Max = 1
	cfg.Metadata.Retry.Backoff = 100 * time.Millisecond
	_, err := sarama.NewSyncProducer([]string{"localhost:19099"}, cfg)
	require.Error(t, err, "producer to a dead broker must fail")
	t.Logf("PASS UnreachableBroker producer: %v", err)
}

// Neg-2: unreachable broker — consumer connection must fail.
func TestNeg_UnreachableBroker_ConsumerFails(t *testing.T) {
	cfg := sarama.NewConfig()
	cfg.Net.DialTimeout = 2 * time.Second
	cfg.Net.ReadTimeout = 2 * time.Second
	cfg.Net.WriteTimeout = 2 * time.Second
	cfg.Metadata.Retry.Max = 1
	cfg.Metadata.Retry.Backoff = 100 * time.Millisecond
	_, err := sarama.NewConsumer([]string{"localhost:19099"}, cfg)
	require.Error(t, err, "consumer to a dead broker must fail")
	t.Logf("PASS UnreachableBroker consumer: %v", err)
}

// Neg-3: malformed JSON payload consumed from Kafka must fail to unmarshal.
func TestNeg_MalformedJSON_FromKafka(t *testing.T) {
	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)
	produceRaw(t, producer, []byte("{not valid json"))
	raw := consumeNRaw(t, consumer, 1, 10*time.Second)
	require.Len(t, raw, 1)
	var payload map[string]interface{}
	err := json.Unmarshal(raw[0], &payload)
	require.Error(t, err, "malformed JSON must fail to unmarshal")
	t.Logf("PASS MalformedJSON: %v", err)
}

// Neg-4: aggregate — missing value field in live message must return error.
func TestNeg_Aggregate_MissingValueField_LiveMessage(t *testing.T) {
	wn := "neg-missing-field"
	clearWindow(wn)
	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)
	produce(t, producer, map[string]interface{}{"pressure": 1013.25, "unit": "hPa"})
	messages := consumeN(t, consumer, 1, 10*time.Second)
	require.Len(t, messages, 1)
	act := newAggAct(t, &aggregate.Settings{
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 3,
		Function: "sum", ValueField: "temperature",
	})
	tc := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc.SetInputObject(&aggregate.Input{Message: messages[0]}))
	done, err := act.Eval(tc)
	assert.False(t, done)
	require.Error(t, err)
	t.Logf("PASS MissingValueField: %v", err)
}

// Neg-5: aggregate — non-numeric string field in live message must error.
func TestNeg_Aggregate_NonNumericField_LiveMessage(t *testing.T) {
	wn := "neg-non-numeric"
	clearWindow(wn)
	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)
	produce(t, producer, map[string]interface{}{"temperature": "hot"})
	messages := consumeN(t, consumer, 1, 10*time.Second)
	act := newAggAct(t, &aggregate.Settings{
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 3,
		Function: "sum", ValueField: "temperature",
	})
	tc := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc.SetInputObject(&aggregate.Input{Message: messages[0]}))
	done, err := act.Eval(tc)
	assert.False(t, done)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be coerced to float64")
	t.Logf("PASS NonNumericField: %v", err)
}

// Neg-6: filter — rejected message must have nil Message and non-empty Reason.
func TestNeg_Filter_MessageRejected_LiveRoundtrip(t *testing.T) {
	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)
	produce(t, producer, map[string]interface{}{"temperature": 15.0, "sensor": "s-001"})
	messages := consumeN(t, consumer, 1, 10*time.Second)
	act := newFilterActivity(t, &filteract.Settings{
		Field: "temperature", Operator: "gt", Value: "30",
	})
	out := evalFilter(t, act, messages[0])
	assert.False(t, out.Passed)
	assert.Nil(t, out.Message)
	assert.NotEmpty(t, out.Reason)
	t.Logf("PASS FilterRejected: reason=%q", out.Reason)
}

// Neg-7: filter — all messages in a live batch must be rejected.
func TestNeg_Filter_AllRejected_LiveBatch(t *testing.T) {
	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)
	payloads := []map[string]interface{}{
		{"level": "info"},
		{"level": "warning"},
		{"level": "error"},
	}
	for _, p := range payloads {
		produce(t, producer, p)
	}
	messages := consumeN(t, consumer, len(payloads), 10*time.Second)
	act := newFilterActivity(t, &filteract.Settings{
		Field: "level", Operator: "eq", Value: "critical",
	})
	for i, msg := range messages {
		out := evalFilter(t, act, msg)
		assert.False(t, out.Passed, "msg %d must be rejected (level=%v)", i, msg["level"])
		assert.Nil(t, out.Message)
	}
	t.Logf("PASS AllRejected: all %d messages filtered out", len(payloads))
}

// Neg-8: filter — non-numeric field against numeric operator must set ErrorMessage.
func TestNeg_Filter_NonNumericFieldValue_LiveMessage(t *testing.T) {
	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)
	produce(t, producer, map[string]interface{}{"temp": "n/a"})
	messages := consumeN(t, consumer, 1, 10*time.Second)
	act := newFilterActivity(t, &filteract.Settings{
		Field: "temp", Operator: "gt", Value: "30",
	})
	out := evalFilter(t, act, messages[0])
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.ErrorMessage)
	assert.Empty(t, out.Reason)
	t.Logf("PASS NonNumericFieldValue: errorMessage=%q", out.ErrorMessage)
}

// Neg-9: filter — missing field with PassThroughOnMissing=false must reject.
func TestNeg_Filter_MissingField_DefaultFail_LiveMessage(t *testing.T) {
	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)
	produce(t, producer, map[string]interface{}{"humidity": 65.0})
	messages := consumeN(t, consumer, 1, 10*time.Second)
	act := newFilterActivity(t, &filteract.Settings{
		Field: "temperature", Operator: "gt", Value: "25",
		PassThroughOnMissing: false,
	})
	out := evalFilter(t, act, messages[0])
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.Reason)
	t.Logf("PASS MissingFieldDefaultFail: reason=%q", out.Reason)
}

// Neg-10: filter — missing field with PassThroughOnMissing=true must allow through.
func TestNeg_Filter_MissingField_PassThrough_LiveMessage(t *testing.T) {
	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)
	produce(t, producer, map[string]interface{}{"humidity": 65.0})
	messages := consumeN(t, consumer, 1, 10*time.Second)
	act := newFilterActivity(t, &filteract.Settings{
		Field: "temperature", Operator: "gt", Value: "25",
		PassThroughOnMissing: true,
	})
	out := evalFilter(t, act, messages[0])
	assert.True(t, out.Passed)
	t.Logf("PASS MissingFieldPassThrough: message allowed through")
}

// Neg-11: aggregate — invalid window type must be caught at construction.
func TestNeg_Aggregate_InvalidWindowType_ConstructionError(t *testing.T) {
	_, err := aggregate.New(test.NewActivityInitContext(&aggregate.Settings{
		WindowName: "neg-bad-wt", WindowType: "StreamingWindow", WindowSize: 5,
		Function: "sum", ValueField: "v",
	}, nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported windowType")
	t.Logf("PASS InvalidWindowType: %v", err)
}

// Neg-12: aggregate — invalid function must be caught at construction.
func TestNeg_Aggregate_InvalidFunction_ConstructionError(t *testing.T) {
	_, err := aggregate.New(test.NewActivityInitContext(&aggregate.Settings{
		WindowName: "neg-bad-fn", WindowType: "TumblingCount", WindowSize: 5,
		Function: "median", ValueField: "v",
	}, nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported function")
	t.Logf("PASS InvalidFunction: %v", err)
}

// Neg-13: aggregate — zero window size must be caught at construction.
func TestNeg_Aggregate_ZeroWindowSize_ConstructionError(t *testing.T) {
	_, err := aggregate.New(test.NewActivityInitContext(&aggregate.Settings{
		WindowName: "neg-zero-sz", WindowType: "TumblingCount", WindowSize: 0,
		Function: "sum", ValueField: "v",
	}, nil))
	require.Error(t, err)
	t.Logf("PASS ZeroWindowSize: %v", err)
}

// Neg-14: filter — invalid operator must be caught at construction.
func TestNeg_Filter_InvalidOperator_ConstructionError(t *testing.T) {
	_, err := filteract.New(test.NewActivityInitContext(&filteract.Settings{
		Field: "v", Operator: "fuzzyMatch", Value: "42",
	}, nil))
	require.Error(t, err)
	t.Logf("PASS InvalidOperator: %v", err)
}

// Neg-15: filter — invalid regex must be caught at construction.
func TestNeg_Filter_InvalidRegex_ConstructionError(t *testing.T) {
	_, err := filteract.New(test.NewActivityInitContext(&filteract.Settings{
		Field: "device", Operator: "regex", Value: "[unclosed",
	}, nil))
	require.Error(t, err)
	t.Logf("PASS InvalidRegex: %v", err)
}

// Neg-16: aggregate — overflow=error enforced with live messages.
func TestNeg_Aggregate_BufferOverflow_Error_LiveMessages(t *testing.T) {
	wn := "neg-overflow-err"
	clearWindow(wn)
	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)
	for _, v := range []float64{1, 2, 3} {
		produce(t, producer, map[string]interface{}{"metric": v})
	}
	messages := consumeN(t, consumer, 3, 10*time.Second)
	act := newAggAct(t, &aggregate.Settings{
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 10,
		Function: "sum", ValueField: "metric",
		MaxBufferSize: 2, OverflowPolicy: "error",
	})
	evalAggregate(t, act, messages[0])
	evalAggregate(t, act, messages[1])
	tc := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc.SetInputObject(&aggregate.Input{Message: messages[2]}))
	done, err := act.Eval(tc)
	assert.False(t, done)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "buffer full")
	t.Logf("PASS BufferOverflow: %v", err)
}

// Neg-17: aggregate — MaxKeys exceeded with live messages must return error.
func TestNeg_Aggregate_MaxKeys_Exceeded_LiveMessages(t *testing.T) {
	wn := "neg-maxkeys"
	priorCount := int64(len(kafkastream.ListSnapshots()))
	maxKeys := priorCount + 2
	defer func() {
		clearWindow(wn, wn+":device-A", wn+":device-B")
	}()
	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)
	produce(t, producer, map[string]interface{}{"metric": 10.0, "device": "device-A"})
	produce(t, producer, map[string]interface{}{"metric": 20.0, "device": "device-B"})
	messages := consumeN(t, consumer, 2, 10*time.Second)
	act := newAggAct(t, &aggregate.Settings{
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 10,
		Function: "sum", ValueField: "metric", KeyField: "device",
		MaxKeys: maxKeys,
	})
	out := evalAggregate(t, act, messages[0])
	assert.False(t, out.WindowClosed)
	tc := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc.SetInputObject(&aggregate.Input{Message: messages[1]}))
	done, err := act.Eval(tc)
	assert.False(t, done)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max keyed windows")
	t.Logf("PASS MaxKeysExceeded: %v", err)
}

// Neg-18: aggregate — duplicate MessageIDs deduplicated via live Kafka.
// Produces A, A (dup), B, C. Only 3 distinct events counted; sum must be 60.
func TestNeg_Aggregate_DeduplicateMessages_LiveKafka(t *testing.T) {
	wn := "neg-dedup-live"
	clearWindow(wn)
	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)
	payloads := []map[string]interface{}{
		{"val": 10.0, "msgid": "msg-A"},
		{"val": 10.0, "msgid": "msg-A"},
		{"val": 20.0, "msgid": "msg-B"},
		{"val": 30.0, "msgid": "msg-C"},
	}
	for _, p := range payloads {
		produce(t, producer, p)
	}
	messages := consumeN(t, consumer, 4, 10*time.Second)
	act := newAggAct(t, &aggregate.Settings{
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 3,
		Function: "sum", ValueField: "val", MessageIDField: "msgid",
	})
	var windowResult *aggregate.Output
	for _, msg := range messages {
		out := evalAggregate(t, act, msg)
		if out.WindowClosed {
			windowResult = out
		}
	}
	require.NotNil(t, windowResult, "window must close after 3 distinct messages")
	assert.Equal(t, 60.0, windowResult.Result)
	assert.Equal(t, int64(3), windowResult.Count)
	t.Logf("PASS DeduplicateMessages: sum=%.0f count=%d", windowResult.Result, windowResult.Count)
}

// Neg-19: filter — AND chain, one predicate fails via live Kafka.
func TestNeg_Filter_MultiPredicateAND_OneFails_LiveKafka(t *testing.T) {
	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)
	produce(t, producer, map[string]interface{}{"price": 50.0, "region": "US"})
	messages := consumeN(t, consumer, 1, 10*time.Second)
	act := newFilterActivity(t, &filteract.Settings{
		PredicatesJSON: `[{"field":"price","operator":"gt","value":"100"},{"field":"region","operator":"eq","value":"US"}]`,
		PredicateMode:  "and",
	})
	out := evalFilter(t, act, messages[0])
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.Reason)
	t.Logf("PASS MultiPredicateAND OneFails: reason=%q", out.Reason)
}

// Neg-20: filter — OR chain, all predicates fail via live Kafka.
func TestNeg_Filter_MultiPredicateOR_AllFail_LiveKafka(t *testing.T) {
	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)
	produce(t, producer, map[string]interface{}{"status": 500.0})
	messages := consumeN(t, consumer, 1, 10*time.Second)
	act := newFilterActivity(t, &filteract.Settings{
		PredicatesJSON: `[{"field":"status","operator":"eq","value":"200"},{"field":"status","operator":"eq","value":"201"}]`,
		PredicateMode:  "or",
	})
	out := evalFilter(t, act, messages[0])
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.Reason)
	t.Logf("PASS MultiPredicateOR AllFail: reason=%q", out.Reason)
}

// Neg-21: late event must be flagged with AllowedLateness=0 via live Kafka.
func TestNeg_Aggregate_LateEvent_FlaggedLate_LiveKafka(t *testing.T) {
	wn := "neg-late-live"
	clearWindow(wn)
	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)
	baseMs := int64(1_700_100_000_000)
	produce(t, producer, map[string]interface{}{
		"val":   100.0,
		"ts_ms": float64(baseMs + 10_000),
	})
	produce(t, producer, map[string]interface{}{
		"val":   5.0,
		"ts_ms": float64(baseMs + 2_000),
	})
	messages := consumeN(t, consumer, 2, 10*time.Second)
	act := newAggAct(t, &aggregate.Settings{
		WindowName: wn, WindowType: "TumblingTime", WindowSize: 10_000,
		Function: "sum", ValueField: "val",
		EventTimeField: "ts_ms", AllowedLateness: 0,
	})
	evalAggregate(t, act, messages[0])
	tc := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc.SetInputObject(&aggregate.Input{Message: messages[1]}))
	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)
	out := &aggregate.Output{}
	require.NoError(t, tc.GetOutputObject(out))
	assert.True(t, out.LateEvent)
	assert.NotEmpty(t, out.LateReason)
	t.Logf("PASS LateEvent: lateReason=%q", out.LateReason)
}

// Neg-22: window must NOT close on a partial batch produced to live Kafka.
func TestNeg_Aggregate_WindowNotClosed_PartialBatch_LiveKafka(t *testing.T) {
	wn := "neg-partial"
	clearWindow(wn)
	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)
	const windowSize = 10
	const produced = 4
	for i := 0; i < produced; i++ {
		produce(t, producer, map[string]interface{}{"val": float64(i + 1)})
	}
	messages := consumeN(t, consumer, produced, 10*time.Second)
	act := newAggAct(t, &aggregate.Settings{
		WindowName: wn, WindowType: "TumblingCount", WindowSize: windowSize,
		Function: "sum", ValueField: "val",
	})
	for i, msg := range messages {
		out := evalAggregate(t, act, msg)
		assert.False(t, out.WindowClosed,
			fmt.Sprintf("msg %d/%d: window must not close before %d events", i+1, produced, windowSize))
		assert.Equal(t, 0.0, out.Result)
	}
	t.Logf("PASS PartialBatch: window stayed open for all %d messages", produced)
}

// Neg-23: filter — invalid regex inside predicatesJSON must fail at construction.
func TestNeg_Filter_InvalidRegexInPredicatesJSON_ConstructionError(t *testing.T) {
	_, err := filteract.New(test.NewActivityInitContext(&filteract.Settings{
		PredicatesJSON: `[{"field":"device","operator":"regex","value":"[bad"}]`,
	}, nil))
	require.Error(t, err)
	t.Logf("PASS InvalidRegexInPredicatesJSON: %v", err)
}

// Neg-24: aggregate — negative window size must fail at construction.
func TestNeg_Aggregate_NegativeWindowSize_ConstructionError(t *testing.T) {
	_, err := aggregate.New(test.NewActivityInitContext(&aggregate.Settings{
		WindowName: "neg-neg-size", WindowType: "TumblingCount", WindowSize: -5,
		Function: "sum", ValueField: "v",
	}, nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "windowSize must be > 0")
	t.Logf("PASS NegativeWindowSize: %v", err)
}

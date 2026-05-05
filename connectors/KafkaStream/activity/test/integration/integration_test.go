// Package integration tests the kafka-stream activities against a live Kafka 3.8.0 broker.
//
// Prerequisites:
//   - Kafka running on localhost:9092 (PLAINTEXT, no auth)
//   - Topic "kafka-stream-test" exists
//
// Run:
//
//	go test -v ./...
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

const (
	broker = "localhost:9092"
	topic  = "kafka-stream-int-test"
)

// ─── Kafka helpers ───────────────────────────────────────────────────────────

func newProducer(t *testing.T) sarama.SyncProducer {
	t.Helper()
	cfg := sarama.NewConfig()
	cfg.Producer.Return.Successes = true
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Version = sarama.V2_8_0_0
	p, err := sarama.NewSyncProducer([]string{broker}, cfg)
	require.NoError(t, err, "failed to create Kafka producer — is Kafka running on %s?", broker)
	return p
}

func newConsumer(t *testing.T) sarama.PartitionConsumer {
	t.Helper()
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V2_8_0_0
	client, err := sarama.NewConsumer([]string{broker}, cfg)
	require.NoError(t, err)
	pc, err := client.ConsumePartition(topic, 0, sarama.OffsetNewest)
	require.NoError(t, err)
	t.Cleanup(func() { pc.Close(); client.Close() })
	return pc
}

func produce(t *testing.T, p sarama.SyncProducer, payload map[string]interface{}) int64 {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	msg := &sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.ByteEncoder(body),
	}
	_, offset, err := p.SendMessage(msg)
	require.NoError(t, err)
	return offset
}

func consumeN(t *testing.T, pc sarama.PartitionConsumer, n int, timeout time.Duration) []map[string]interface{} {
	t.Helper()
	var results []map[string]interface{}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for len(results) < n {
		select {
		case msg := <-pc.Messages():
			var payload map[string]interface{}
			require.NoError(t, json.Unmarshal(msg.Value, &payload))
			results = append(results, payload)
		case <-deadline.C:
			t.Fatalf("timed out waiting for messages (got %d, want %d)", len(results), n)
		}
	}
	return results
}

// ─── Activity helpers ────────────────────────────────────────────────────────

func evalAggregate(t *testing.T, act *aggregate.Activity, message map[string]interface{}) *aggregate.Output {
	t.Helper()
	tc := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc.SetInputObject(&aggregate.Input{Message: message}))
	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)
	out := &aggregate.Output{}
	require.NoError(t, tc.GetOutputObject(out))
	return out
}

func evalFilter(t *testing.T, act *filteract.Activity, message map[string]interface{}) *filteract.Output {
	t.Helper()
	tc := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc.SetInputObject(&filteract.Input{Message: message}))
	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)
	out := &filteract.Output{}
	require.NoError(t, tc.GetOutputObject(out))
	return out
}

func newAggAct(t *testing.T, s *aggregate.Settings) *aggregate.Activity {
	t.Helper()
	act, err := aggregate.New(test.NewActivityInitContext(s, nil))
	require.NoError(t, err)
	return act.(*aggregate.Activity)
}

func newFilterActivity(t *testing.T, s *filteract.Settings) *filteract.Activity {
	t.Helper()
	act, err := filteract.New(test.NewActivityInitContext(s, nil))
	require.NoError(t, err)
	return act.(*filteract.Activity)
}

func clearWindow(names ...string) {
	for _, n := range names {
		kafkastream.UnregisterWindowStore(n)
	}
}

// ─── Test 1: TumblingCount + sum ─────────────────────────────────────────────
//
// Produce 5 temperature readings, consume them, feed through aggregate.
// Window must close on 5th message with sum=150.

func TestIntegration_AggregateSum_TumblingCount(t *testing.T) {
	wn := "int-tc-sum"
	clearWindow(wn)

	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)

	readings := []float64{10, 20, 30, 40, 50}
	for _, v := range readings {
		produce(t, producer, map[string]interface{}{"temperature": v, "unit": "celsius"})
	}

	messages := consumeN(t, consumer, len(readings), 10*time.Second)
	require.Len(t, messages, len(readings))

	act := newAggAct(t, &aggregate.Settings{
		WindowName: wn,
		WindowType: "TumblingCount",
		WindowSize: int64(len(readings)),
		Function:   "sum",
		ValueField: "temperature",
	})

	var windowResult *aggregate.Output
	for i, msg := range messages {
		out := evalAggregate(t, act, msg)
		if i < len(readings)-1 {
			assert.False(t, out.WindowClosed, "message %d: window must not close before %d events", i+1, len(readings))
		} else {
			windowResult = out
		}
	}

	require.NotNil(t, windowResult)
	assert.True(t, windowResult.WindowClosed)
	assert.Equal(t, 150.0, windowResult.Result)
	assert.Equal(t, int64(5), windowResult.Count)
	t.Logf("PASS TumblingCount sum=%.1f count=%d", windowResult.Result, windowResult.Count)
}

// ─── Test 2: TumblingCount + avg ─────────────────────────────────────────────

func TestIntegration_AggregateAvg_TumblingCount(t *testing.T) {
	wn := "int-tc-avg"
	clearWindow(wn)

	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)

	readings := []float64{100, 200, 300}
	for _, v := range readings {
		produce(t, producer, map[string]interface{}{"price": v})
	}

	messages := consumeN(t, consumer, len(readings), 10*time.Second)
	act := newAggAct(t, &aggregate.Settings{
		WindowName: wn, WindowType: "TumblingCount",
		WindowSize: int64(len(readings)), Function: "avg", ValueField: "price",
	})

	var last *aggregate.Output
	for _, msg := range messages {
		last = evalAggregate(t, act, msg)
	}

	assert.True(t, last.WindowClosed)
	assert.Equal(t, 200.0, last.Result)
	t.Logf("PASS TumblingCount avg=%.1f", last.Result)
}

// ─── Test 3: Filter → aggregate pipeline ─────────────────────────────────────
//
// Produce 10 messages: 5 with temp>25 (pass), 5 with temp<=25 (block).
// After filtering, feed passing messages into a TumblingCount window of size 5.
// Expected sum = 30+35+28+40+27 = 160.

func TestIntegration_FilterThenAggregate(t *testing.T) {
	wn := "int-filter-agg"
	clearWindow(wn)

	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)

	high := []float64{30, 35, 28, 40, 27}
	low := []float64{25, 10, 5, 20, 15}
	hi, lo := 0, 0
	for i := 0; i < 10; i++ {
		var msg map[string]interface{}
		if i%2 == 0 && hi < len(high) {
			msg = map[string]interface{}{"temperature": high[hi], "idx": i}
			hi++
		} else if lo < len(low) {
			msg = map[string]interface{}{"temperature": low[lo], "idx": i}
			lo++
		}
		produce(t, producer, msg)
	}

	messages := consumeN(t, consumer, 10, 15*time.Second)

	filterAct := newFilterActivity(t, &filteract.Settings{
		Field: "temperature", Operator: "gt", Value: "25",
	})
	aggAct := newAggAct(t, &aggregate.Settings{
		WindowName: wn, WindowType: "TumblingCount",
		WindowSize: 5, Function: "sum", ValueField: "temperature",
	})

	var windowClosed bool
	var finalResult float64
	passCount := 0
	for _, msg := range messages {
		fOut := evalFilter(t, filterAct, msg)
		if !fOut.Passed {
			continue
		}
		passCount++
		aOut := evalAggregate(t, aggAct, fOut.Message)
		if aOut.WindowClosed {
			windowClosed = true
			finalResult = aOut.Result
			break
		}
	}

	assert.Equal(t, 5, passCount)
	assert.True(t, windowClosed)
	assert.Equal(t, 160.0, finalResult)
	t.Logf("PASS Filter->Aggregate passCount=%d sum=%.1f", passCount, finalResult)
}

// ─── Test 4: Keyed aggregation ───────────────────────────────────────────────
//
// 3 devices each emit 3 readings.  Window (size=3) closes independently per key.

func TestIntegration_KeyedAggregation(t *testing.T) {
	wn := "int-keyed"
	clearWindow(wn, wn+":dev-A", wn+":dev-B", wn+":dev-C")

	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)

	type reading struct {
		device string
		value  float64
	}
	readings := []reading{
		{"dev-A", 10}, {"dev-B", 20}, {"dev-C", 30},
		{"dev-A", 10}, {"dev-B", 20}, {"dev-C", 30},
		{"dev-A", 10}, {"dev-B", 20}, {"dev-C", 30},
	}

	for _, r := range readings {
		produce(t, producer, map[string]interface{}{
			"value":     r.value,
			"device_id": r.device,
		})
	}

	messages := consumeN(t, consumer, len(readings), 15*time.Second)

	act := newAggAct(t, &aggregate.Settings{
		WindowName: wn, WindowType: "TumblingCount",
		WindowSize: 3, Function: "sum",
		ValueField: "value", KeyField: "device_id",
	})

	deviceResults := make(map[string]float64)
	for _, msg := range messages {
		out := evalAggregate(t, act, msg)
		if out.WindowClosed {
			deviceResults[out.Key] = out.Result
		}
	}

	assert.Equal(t, 30.0, deviceResults["dev-A"])
	assert.Equal(t, 60.0, deviceResults["dev-B"])
	assert.Equal(t, 90.0, deviceResults["dev-C"])
	t.Logf("PASS Keyed: dev-A=%.0f dev-B=%.0f dev-C=%.0f",
		deviceResults["dev-A"], deviceResults["dev-B"], deviceResults["dev-C"])
}

// ─── Test 5: Two consecutive windows reset correctly ─────────────────────────

func TestIntegration_TwoConsecutiveWindows(t *testing.T) {
	wn := "int-consecutive"
	clearWindow(wn)

	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)

	allVals := []float64{10, 20, 30, 1, 3, 5}
	for _, v := range allVals {
		produce(t, producer, map[string]interface{}{"val": v})
	}

	messages := consumeN(t, consumer, len(allVals), 15*time.Second)
	act := newAggAct(t, &aggregate.Settings{
		WindowName: wn, WindowType: "TumblingCount",
		WindowSize: 3, Function: "sum", ValueField: "val",
	})

	var results []float64
	for _, msg := range messages {
		out := evalAggregate(t, act, msg)
		if out.WindowClosed {
			results = append(results, out.Result)
		}
	}

	require.Len(t, results, 2)
	assert.Equal(t, 60.0, results[0])
	assert.Equal(t, 9.0, results[1])
	t.Logf("PASS Consecutive: w1=%.0f w2=%.0f", results[0], results[1])
}

// ─── Test 6: Filter operators against live messages ──────────────────────────

func TestIntegration_FilterOperators_LiveMessages(t *testing.T) {
	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)

	produce(t, producer, map[string]interface{}{
		"temp":   35.0,
		"device": "sensor-42",
		"topic":  "sensor.temp",
		"env":    "production",
	})
	messages := consumeN(t, consumer, 1, 10*time.Second)
	msg := messages[0]

	tests := []struct {
		name     string
		settings *filteract.Settings
		want     bool
	}{
		{"gt-pass", &filteract.Settings{Field: "temp", Operator: "gt", Value: "30"}, true},
		{"gt-fail", &filteract.Settings{Field: "temp", Operator: "gt", Value: "40"}, false},
		{"lt-pass", &filteract.Settings{Field: "temp", Operator: "lt", Value: "40"}, true},
		{"eq-string-pass", &filteract.Settings{Field: "env", Operator: "eq", Value: "production"}, true},
		{"eq-string-fail", &filteract.Settings{Field: "env", Operator: "eq", Value: "staging"}, false},
		{"contains-pass", &filteract.Settings{Field: "device", Operator: "contains", Value: "-42"}, true},
		{"startsWith-pass", &filteract.Settings{Field: "topic", Operator: "startsWith", Value: "sensor."}, true},
		{"endsWith-pass", &filteract.Settings{Field: "topic", Operator: "endsWith", Value: ".temp"}, true},
		{"regex-pass", &filteract.Settings{Field: "device", Operator: "regex", Value: `^sensor-[0-9]+$`}, true},
		{"regex-fail", &filteract.Settings{Field: "device", Operator: "regex", Value: `^actuator-[0-9]+$`}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			act := newFilterActivity(t, tt.settings)
			out := evalFilter(t, act, msg)
			assert.Equal(t, tt.want, out.Passed,
				"operator=%s field=%s value=%s", tt.settings.Operator, tt.settings.Field, tt.settings.Value)
		})
	}
	t.Logf("PASS FilterOperators: all %d sub-tests", len(tests))
}

// ─── Test 7: SlidingCount rolling window ─────────────────────────────────────

func TestIntegration_SlidingCount_LiveMessages(t *testing.T) {
	wn := "int-sliding"
	clearWindow(wn)

	producer := newProducer(t)
	defer producer.Close()
	consumer := newConsumer(t)

	vals := []float64{5, 10, 15, 20, 25}
	for _, v := range vals {
		produce(t, producer, map[string]interface{}{"metric": v})
	}
	messages := consumeN(t, consumer, len(vals), 10*time.Second)

	act := newAggAct(t, &aggregate.Settings{
		WindowName: wn, WindowType: "SlidingCount",
		WindowSize: 3, Function: "sum", ValueField: "metric",
	})

	// rolling sum of last 3: 5, 15, 30, 45, 60
	expected := []float64{5, 15, 30, 45, 60}
	for i, msg := range messages {
		out := evalAggregate(t, act, msg)
		assert.True(t, out.WindowClosed, "sliding window must always close")
		assert.Equal(t, expected[i], out.Result,
			fmt.Sprintf("msg %d: expected rolling sum=%.0f", i+1, expected[i]))
	}
	t.Logf("PASS SlidingCount: rolling sums verified")
}

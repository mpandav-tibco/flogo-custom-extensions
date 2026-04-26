// Package integration — load / memory-leak tests against a live Kafka 3.8.0 broker.
//
// What is measured:
//   - Heap allocation growth (HeapInuse, HeapAlloc, HeapObjects) over time
//   - Goroutine count before and after each phase
//   - Window registry size (must not grow unboundedly after windows close)
//   - Dedup-map growth (must stay bounded; verified via Snapshot().BufferSize)
//   - Throughput (messages/sec produced and consumed against live Kafka)
//
// Topics: kafka-stream-load-test (3 partitions)
//
// Run:
//
//	go test -v -run TestLoad -timeout 300s ./...
package integration

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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

const loadTopic = "kafka-stream-load-test"

// msgSizeBytes is the target byte-size for each Kafka message in load tests.
// Set to 1024 (1 KB) to exercise realistic payload handling.
const msgSizeBytes = 1024

// loadMemStats is a heap snapshot captured for load-test assertions.
type loadMemStats struct {
	HeapInuse  uint64
	HeapAlloc  uint64
	HeapObj    uint64
	Goroutines int
}

// captureLoadMemStats forces a GC and returns current memory counters.
func captureLoadMemStats() loadMemStats {
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return loadMemStats{
		HeapInuse:  ms.HeapInuse,
		HeapAlloc:  ms.HeapAlloc,
		HeapObj:    ms.HeapObjects,
		Goroutines: runtime.NumGoroutine(),
	}
}

func (m loadMemStats) String() string {
	return fmt.Sprintf(
		"heapInuse=%.1fMB heapAlloc=%.1fMB heapObj=%d goroutines=%d",
		float64(m.HeapInuse)/1e6, float64(m.HeapAlloc)/1e6, m.HeapObj, m.Goroutines,
	)
}

// heapGrowthMB returns the heap growth in MB after a GC.
func heapGrowthMB(before, after loadMemStats) float64 {
	growth := int64(after.HeapInuse) - int64(before.HeapInuse)
	if growth < 0 {
		return 0
	}
	return float64(growth) / 1e6
}

// newLoadConsumerClient creates a bare Sarama Consumer client (not subscribed).
func newLoadConsumerClient(t *testing.T) sarama.Consumer {
	t.Helper()
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V2_8_0_0
	c, err := sarama.NewConsumer([]string{broker}, cfg)
	require.NoError(t, err)
	return c
}

// latestLoadOffsets returns the current high-water-mark offsets for all
// partitions of loadTopic so that a subsequent consumer can start from now.
func latestLoadOffsets(t *testing.T, c sarama.Consumer) map[int32]int64 {
	t.Helper()
	parts, err := c.Partitions(loadTopic)
	require.NoError(t, err)
	offsets := make(map[int32]int64, len(parts))
	for _, p := range parts {
		pc, err := c.ConsumePartition(loadTopic, p, sarama.OffsetNewest)
		require.NoError(t, err)
		offsets[p] = pc.HighWaterMarkOffset()
		pc.Close()
	}
	return offsets
}

// drainLoadTopic consumes messages from all loadTopic partitions starting at
// startOffsets, returning when totalWant messages are collected or idle elapses.
func drainLoadTopic(t *testing.T, c sarama.Consumer, startOffsets map[int32]int64, totalWant int, idleFor time.Duration) int {
	t.Helper()
	var wg sync.WaitGroup
	var got int64
	for part, off := range startOffsets {
		wg.Add(1)
		go func(partition int32, startOff int64) {
			defer wg.Done()
			pc, err := c.ConsumePartition(loadTopic, partition, startOff)
			if err != nil {
				t.Logf("partition %d consumer error: %v", partition, err)
				return
			}
			defer pc.Close()
			idle := time.NewTimer(idleFor)
			for {
				select {
				case <-pc.Messages():
					now := atomic.AddInt64(&got, 1)
					if !idle.Stop() {
						select {
						case <-idle.C:
						default:
						}
					}
					idle.Reset(idleFor)
					if now >= int64(totalWant) {
						return
					}
				case <-idle.C:
					return
				}
			}
		}(part, off)
	}
	wg.Wait()
	return int(atomic.LoadInt64(&got))
}

// makeLoadPayload builds a JSON byte slice of approximately sizeBytes bytes.
// The message always contains v (numeric) and sensor fields so it can be
// processed by both the filter and aggregate activities.  A "pad" field
// (filled with 'x' characters) makes up the remaining bytes to hit the
// target size.  index is embedded in the sensor name for uniqueness.
func makeLoadPayload(sizeBytes int, value float64, index int) []byte {
	base := fmt.Sprintf(`{"v":%g,"sensor":"s-%06d","region":"us-east-1","ts":"%s","pad":"`,
		value, index, "2026-03-28T10:00:00Z")
	suffix := `"}`
	padLen := sizeBytes - len(base) - len(suffix)
	if padLen < 0 {
		padLen = 0
	}
	return []byte(base + strings.Repeat("x", padLen) + suffix)
}

// bulkLoadProduce sends n copies of payload to loadTopic as fast as possible.
// Returns the number successfully acknowledged and elapsed time.
func bulkLoadProduce(t *testing.T, p sarama.SyncProducer, n int, payload []byte) (int, time.Duration) {
	t.Helper()
	start := time.Now()
	sent := 0
	for i := 0; i < n; i++ {
		_, _, err := p.SendMessage(&sarama.ProducerMessage{
			Topic: loadTopic,
			Value: sarama.ByteEncoder(payload),
		})
		if err == nil {
			sent++
		}
	}
	return sent, time.Since(start)
}

// ─── Load Test 1: High-throughput produce / consume (pure Kafka I/O) ─────────
// Produce 50 000 × 1 KB messages, consume them back, report throughput and
// heap growth.  Gives a baseline for pure Kafka I/O overhead at realistic
// payload sizes with no window processing.

func TestLoad_HighThroughput_ProduceConsume(t *testing.T) {
	const total = 50_000
	payload := makeLoadPayload(msgSizeBytes, 1.5, 0)

	producer := newProducer(t)
	defer producer.Close()
	client := newLoadConsumerClient(t)
	defer client.Close()

	startOffsets := latestLoadOffsets(t, client)
	before := captureLoadMemStats()

	sent, elapsed := bulkLoadProduce(t, producer, total, payload)
	throughput := float64(sent) / elapsed.Seconds()
	t.Logf("Produce: sent=%d elapsed=%s throughput=%.0f msg/s", sent, elapsed.Round(time.Millisecond), throughput)
	assert.Equal(t, total, sent, "all messages must be acknowledged by broker")

	received := drainLoadTopic(t, client, startOffsets, total, 15*time.Second)
	t.Logf("Consume: received=%d", received)
	assert.Equal(t, total, received, "all produced messages must be consumed")

	after := captureLoadMemStats()
	growth := heapGrowthMB(before, after)
	t.Logf("Memory: before=[%s] after=[%s] growth=%.1fMB", before, after, growth)
	assert.LessOrEqual(t, growth, 200.0, "heap growth must be <= 200 MB for 50k × 1 KB produce/consume")
}

// ─── Load Test 2: Window registry returns to baseline after bulk cleanup ──────
// Create 1000 TumblingCount(size=1) windows, feed one event each so all close,
// then unregister all.  Registry size and heap must return to baseline.

func TestLoad_WindowRegistry_NoLeak(t *testing.T) {
	const numWindows = 1000

	// Pre-clean any leftovers from previous runs.
	for i := 0; i < numWindows; i++ {
		clearWindow(fmt.Sprintf("load-reg-%04d", i))
	}

	registryBefore := len(kafkastream.ListSnapshots())
	before := captureLoadMemStats()

	closedCount := 0
	for i := 0; i < numWindows; i++ {
		wn := fmt.Sprintf("load-reg-%04d", i)
		act := newAggAct(t, &aggregate.Settings{
			WindowName: wn, WindowType: "TumblingCount", WindowSize: 1,
			Function: "sum", ValueField: "v",
		})
		out := evalAggregate(t, act, map[string]interface{}{"v": 1.0})
		if out.WindowClosed {
			closedCount++
		}
		clearWindow(wn)
	}

	after := captureLoadMemStats()
	registryAfter := len(kafkastream.ListSnapshots())
	growth := heapGrowthMB(before, after)

	t.Logf("Windows: created=%d closed=%d", numWindows, closedCount)
	t.Logf("Registry: before=%d after=%d (delta=%+d)", registryBefore, registryAfter, registryAfter-registryBefore)
	t.Logf("Memory: before=[%s] after=[%s] growth=%.1fMB", before, after, growth)

	assert.Equal(t, numWindows, closedCount, "all TumblingCount(1) windows must close on first message")
	assert.Equal(t, registryBefore, registryAfter, "registry must return to baseline after cleanup")
	assert.LessOrEqual(t, growth, 15.0, "heap growth for 1000 window create+close+unregister must be <= 15 MB")
}

// ─── Load Test 3: Keyed windows — no registry or heap leak ───────────────────
// 500 distinct keys × windowSize events each.  Every sub-window must close.
// Registry and heap must return to baseline after cleanup.

func TestLoad_KeyedWindows_NoRegistryLeak(t *testing.T) {
	const numKeys = 500
	const windowSize = 5
	wn := "load-keyed"

	// Pre-clean.
	clearWindow(wn)
	for i := 0; i < numKeys; i++ {
		clearWindow(fmt.Sprintf("%s:key-%04d", wn, i))
	}

	registryBefore := len(kafkastream.ListSnapshots())
	goroutinesBefore := runtime.NumGoroutine()
	before := captureLoadMemStats()

	act := newAggAct(t, &aggregate.Settings{
		WindowName: wn, WindowType: "TumblingCount", WindowSize: windowSize,
		Function: "sum", ValueField: "v", KeyField: "key",
	})

	closedKeys := make(map[string]float64)
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("key-%04d", i)
		for j := 0; j < windowSize; j++ {
			out := evalAggregate(t, act, map[string]interface{}{"v": float64(j + 1), "key": key})
			if out.WindowClosed {
				closedKeys[out.Key] = out.Result
			}
		}
	}

	// Clean up: unregister all sub-windows.
	clearWindow(wn)
	for i := 0; i < numKeys; i++ {
		clearWindow(fmt.Sprintf("%s:key-%04d", wn, i))
	}

	after := captureLoadMemStats()
	goroutinesAfter := runtime.NumGoroutine()
	registryAfter := len(kafkastream.ListSnapshots())
	growth := heapGrowthMB(before, after)
	goroutineDelta := goroutinesAfter - goroutinesBefore

	t.Logf("Keys: created=%d closed=%d", numKeys, len(closedKeys))
	t.Logf("Registry: before=%d after=%d", registryBefore, registryAfter)
	t.Logf("Goroutines: before=%d after=%d (delta=%+d)", goroutinesBefore, goroutinesAfter, goroutineDelta)
	t.Logf("Memory: before=[%s] after=[%s] growth=%.1fMB", before, after, growth)

	assert.Equal(t, numKeys, len(closedKeys), "every keyed sub-window must close")
	// Sum of 1+2+3+4+5 = 15 for every key.
	for k, v := range closedKeys {
		assert.Equal(t, 15.0, v, "window %s expected sum=15", k)
	}
	assert.Equal(t, registryBefore, registryAfter, "registry must return to baseline")
	assert.LessOrEqual(t, goroutineDelta, 5, "goroutine count must not leak")
	assert.LessOrEqual(t, growth, 20.0, "heap growth for 500 keyed windows must be <= 20 MB")
}

// ─── Load Test 4: Dedup map does not leak across window resets ───────────────
// 200 cycles of TumblingCount(50) with unique MessageIDs. The dedup map must
// be cleared (or its MessageIDs evicted) when a window closes and resets.
// Heap growth across all 10 000 events must stay within 5 MB.

func TestLoad_DedupMap_NoLeakAcrossResets(t *testing.T) {
	const windowSize = 50
	const cycles = 200
	wn := "load-dedup-reset"
	clearWindow(wn)
	defer clearWindow(wn)

	act := newAggAct(t, &aggregate.Settings{
		WindowName: wn, WindowType: "TumblingCount", WindowSize: windowSize,
		Function: "sum", ValueField: "v", MessageIDField: "id",
	})

	before := captureLoadMemStats()
	totalClosed := 0
	msgID := 0
	for cycle := 0; cycle < cycles; cycle++ {
		for i := 0; i < windowSize; i++ {
			msgID++
			out := evalAggregate(t, act, map[string]interface{}{
				"v":  1.0,
				"id": fmt.Sprintf("msg-%06d", msgID),
			})
			if out.WindowClosed {
				totalClosed++
			}
		}
	}

	after := captureLoadMemStats()
	growth := heapGrowthMB(before, after)

	t.Logf("Cycles=%d windowSize=%d totalClosed=%d", cycles, windowSize, totalClosed)
	t.Logf("Memory: before=[%s] after=[%s] growth=%.1fMB", before, after, growth)

	assert.Equal(t, cycles, totalClosed, "every cycle must close exactly one window")
	assert.LessOrEqual(t, growth, 5.0,
		"heap growth across %d dedup cycles must be <= 5 MB (check dedup map not clearing on reset)", cycles)
}

// ─── Load Test 5: SlidingCount buffer stays bounded ──────────────────────────
// SlidingCount(100) must keep at most 100 events in its buffer, regardless of
// how many total events are fed in.  Verified via Snapshot().BufferSize.
// Run 50 000 events so the eviction path is exercised at scale.

func TestLoad_SlidingCountWindow_BoundedBuffer(t *testing.T) {
	const total = 50_000
	const windowSize = 100
	wn := "load-sliding-bounded"
	clearWindow(wn)
	defer clearWindow(wn)

	act := newAggAct(t, &aggregate.Settings{
		WindowName: wn, WindowType: "SlidingCount", WindowSize: windowSize,
		Function: "sum", ValueField: "v",
	})

	before := captureLoadMemStats()
	start := time.Now()
	allEmitted := true
	maxBufSeen := int64(0)

	for i := 0; i < total; i++ {
		out := evalAggregate(t, act, map[string]interface{}{"v": 1.0})
		if !out.WindowClosed && i >= windowSize-1 {
			allEmitted = false
		}
		// Sample buffer size every 500 events.
		if i%500 == 0 {
			snaps := kafkastream.ListSnapshots()
			for _, s := range snaps {
				if s.Name == wn && s.BufferSize > maxBufSeen {
					maxBufSeen = s.BufferSize
				}
			}
		}
	}

	elapsed := time.Since(start)
	after := captureLoadMemStats()
	growth := heapGrowthMB(before, after)

	t.Logf("SlidingCount: events=%d elapsed=%s (%.0f evt/s)", total, elapsed.Round(time.Millisecond), float64(total)/elapsed.Seconds())
	t.Logf("MaxBufferSizeSeen=%d (must be <= windowSize=%d)", maxBufSeen, windowSize)
	t.Logf("Memory: before=[%s] after=[%s] growth=%.1fMB", before, after, growth)

	assert.True(t, allEmitted, "SlidingCount must emit on every event after the buffer is full")
	assert.LessOrEqual(t, maxBufSeen, int64(windowSize), "buffer must never exceed window size")
	assert.LessOrEqual(t, growth, 10.0, "heap growth for 5k sliding events must be <= 10 MB")
}

// ─── Load Test 6: Producer/consumer goroutine leak check ─────────────────────
// Open and close 50 SyncProducer + Consumer client pairs. After all are
// closed, goroutine count must return to within 5 of baseline.

func TestLoad_Goroutine_NoLeak_ProducerConsumer(t *testing.T) {
	const pairs = 50

	goroutinesBefore := runtime.NumGoroutine()
	before := captureLoadMemStats()

	for i := 0; i < pairs; i++ {
		cfg := sarama.NewConfig()
		cfg.Producer.Return.Successes = true
		cfg.Version = sarama.V2_8_0_0
		p, err := sarama.NewSyncProducer([]string{broker}, cfg)
		require.NoError(t, err)
		c, err := sarama.NewConsumer([]string{broker}, cfg)
		require.NoError(t, err)
		require.NoError(t, p.Close())
		require.NoError(t, c.Close())
	}
	// Give Sarama goroutines time to wind down.
	time.Sleep(500 * time.Millisecond)
	runtime.GC()

	goroutinesAfter := runtime.NumGoroutine()
	after := captureLoadMemStats()
	delta := goroutinesAfter - goroutinesBefore
	growth := heapGrowthMB(before, after)

	t.Logf("Goroutines: before=%d after=%d delta=%+d", goroutinesBefore, goroutinesAfter, delta)
	t.Logf("Memory: before=[%s] after=[%s] growth=%.1fMB", before, after, growth)

	assert.LessOrEqual(t, delta, 5,
		"goroutine delta after closing %d producer+consumer pairs must be <= 5", pairs)
}

// ─── Load Test 7: Filter+Aggregate pipeline with live Kafka messages ──────────
// Produce 50 000 × 1 KB messages to loadTopic with mixed values, consume them
// back, and run every message through a Filter → Aggregate pipeline.
// Half the messages pass the filter (v > 50, even-index messages carry v=90);
// each group of 100 passing messages closes a TumblingCount window.
// Expected closes: 50k / 2 (pass rate) / 100 (window) = 250.
// Measures filter hit/miss ratio, window close count, throughput, and heap.

func TestLoad_FilterAggregate_Pipeline_LiveKafka(t *testing.T) {
	const total = 50_000
	const windowSize = 100
	wn := "load-flt-agg-pipeline"
	clearWindow(wn)
	defer clearWindow(wn)

	producer := newProducer(t)
	defer producer.Close()
	client := newLoadConsumerClient(t)
	defer client.Close()

	startOffsets := latestLoadOffsets(t, client)
	before := captureLoadMemStats()

	// Produce total messages: even index v=90 (passes filter), odd v=10 (fails).
	// Each message is ~1 KB (msgSizeBytes) via makeLoadPayload.
	for i := 0; i < total; i++ {
		v := float64(10)
		if i%2 == 0 {
			v = 90
		}
		msg := makeLoadPayload(msgSizeBytes, v, i)
		_, _, err := producer.SendMessage(&sarama.ProducerMessage{
			Topic: loadTopic,
			Value: sarama.ByteEncoder(msg),
		})
		require.NoError(t, err)
	}
	t.Logf("Produced %d messages to %s", total, loadTopic)

	// Collect raw messages from all partitions.
	parts, err := client.Partitions(loadTopic)
	require.NoError(t, err)
	type rawMsg struct{ data []byte }
	msgCh := make(chan rawMsg, total)
	var pcWg sync.WaitGroup
	var pcs []sarama.PartitionConsumer
	for _, part := range parts {
		pc, err := client.ConsumePartition(loadTopic, part, startOffsets[part])
		require.NoError(t, err)
		pcs = append(pcs, pc)
		pcWg.Add(1)
		go func(pc sarama.PartitionConsumer) {
			defer pcWg.Done()
			for kafkaMsg := range pc.Messages() {
				cp := make([]byte, len(kafkaMsg.Value))
				copy(cp, kafkaMsg.Value)
				msgCh <- rawMsg{cp}
			}
		}(pc)
	}

	var rawMessages [][]byte
	collect := time.After(90 * time.Second)
collectLoop:
	for len(rawMessages) < total {
		select {
		case rm := <-msgCh:
			rawMessages = append(rawMessages, rm.data)
		case <-collect:
			break collectLoop
		}
	}
	for _, pc := range pcs {
		pc.Close()
	}
	pcWg.Wait()
	t.Logf("Collected %d/%d messages from Kafka", len(rawMessages), total)
	require.GreaterOrEqual(t, len(rawMessages), total*9/10, "must receive >= 90%% of produced messages")

	// Process through filter -> aggregate pipeline.
	fAct := newFilterActivity(t, &filteract.Settings{
		Field: "v", Operator: "gt", Value: "50",
	})
	aAct := newAggAct(t, &aggregate.Settings{
		WindowName: wn, WindowType: "TumblingCount", WindowSize: windowSize,
		Function: "sum", ValueField: "v",
	})

	var filterPass, filterFail, windowsClosed int
	var totalSum float64

	pipeStart := time.Now()
	for _, raw := range rawMessages {
		var msg map[string]interface{}
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		fOut := evalFilter(t, fAct, msg)
		if !fOut.Passed {
			filterFail++
			continue
		}
		filterPass++
		aOut := evalAggregate(t, aAct, fOut.Message)
		if aOut.WindowClosed {
			windowsClosed++
			totalSum += aOut.Result
		}
	}
	pipeElapsed := time.Since(pipeStart)

	after := captureLoadMemStats()
	growth := heapGrowthMB(before, after)

	t.Logf("Pipeline: pass=%d fail=%d windows_closed=%d total_sum=%.0f elapsed=%s (%.0f msg/s)",
		filterPass, filterFail, windowsClosed, totalSum,
		pipeElapsed.Round(time.Millisecond), float64(len(rawMessages))/pipeElapsed.Seconds())
	t.Logf("Memory AFTER pipeline: %s | growth=%.1fMB", after, growth)

	assert.Greater(t, filterPass, 0, "some messages must pass the filter")
	assert.Greater(t, filterFail, 0, "some messages must be rejected by the filter")
	assert.LessOrEqual(t, growth, 300.0, "heap growth must be <= 300 MB for 50k × 1 KB pipeline (got %.1f MB)", growth)
}

// ─── Load Test 8: Wall-clock heap profile across 50 k TumblingCount events ───
// Sample heap every 5000 events (10 samples total), log a growth table, assert
// there is no monotonic upward trend — proving GC reclaims window heap.

func TestLoad_HeapProfile_TumblingCount(t *testing.T) {
	const total = 50_000
	const windowSize = 1_000
	const sampleEvery = 5_000
	wn := "load-heap-profile"
	clearWindow(wn)
	defer clearWindow(wn)

	act := newAggAct(t, &aggregate.Settings{
		WindowName: wn, WindowType: "TumblingCount", WindowSize: windowSize,
		Function: "avg", ValueField: "v",
	})

	var samples []loadMemStats
	var closedCount int

	for i := 0; i < total; i++ {
		out := evalAggregate(t, act, map[string]interface{}{"v": float64(i % 100)})
		if out.WindowClosed {
			closedCount++
		}
		if (i+1)%sampleEvery == 0 {
			samples = append(samples, captureLoadMemStats())
			t.Logf("[%6d events] %s  windows_closed=%d", i+1, samples[len(samples)-1], closedCount)
		}
	}

	assert.Equal(t, total/windowSize, closedCount, "expected %d window closes", total/windowSize)

	// A healthy heap is stable: the spread between the highest and lowest
	// sample must be < 5 MB.  Monotonically rising values across 50k events
	// would indicate a memory leak (unreleased window buffers).
	if len(samples) >= 2 {
		var minInuse, maxInuse uint64
		minInuse = samples[0].HeapInuse
		maxInuse = samples[0].HeapInuse
		for _, s := range samples[1:] {
			if s.HeapInuse < minInuse {
				minInuse = s.HeapInuse
			}
			if s.HeapInuse > maxInuse {
				maxInuse = s.HeapInuse
			}
		}
		spreadMB := float64(maxInuse-minInuse) / 1e6
		t.Logf("HeapInuse spread across %d samples: min=%.1fMB max=%.1fMB spread=%.1fMB",
			len(samples), float64(minInuse)/1e6, float64(maxInuse)/1e6, spreadMB)
		assert.Less(t, spreadMB, 5.0,
			"heap spread across %d samples must be < 5 MB (spread=%.1f MB indicates a memory leak)", len(samples), spreadMB)
	}
}

// Ensure unused imports are used — the _ references prevent compile errors if
// a helper in another file happens to shadow the import.
var _ = test.NewActivityContext
var _ = atomic.AddInt64

//go:build integration

package datapipeline

// Against live data pipeline engines (their API in full mode, on loopback):
//
//	DP_ENGINE_URL=http://127.0.0.1:9080 DP_PIPELINE=filter-and-map \
//	DP_SLOW_ENGINE_URL=http://127.0.0.1:9081 DP_SLOW_PIPELINE=directory-watch \
//	go test -tags integration -run Live -v ./...
//
// DP_PIPELINE runs to its end; DP_SLOW_PIPELINE until cancelled (a watched
// directory), for the overlap cases. DP_API_TOKEN, when the engines want one.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func live(t *testing.T, urlVar, pipelineVar string) (string, string) {
	t.Helper()
	u, p := os.Getenv(urlVar), os.Getenv(pipelineVar)
	if u == "" || p == "" {
		t.Skipf("set %s and %s", urlVar, pipelineVar)
	}
	return u, p
}

func liveInputs(engine, pipeline string, more map[string]interface{}) map[string]interface{} {
	in := map[string]interface{}{"engineUrl": engine, "pipeline": pipeline, "apiToken": os.Getenv("DP_API_TOKEN"), "pollIntervalSeconds": 1}
	for k, v := range more {
		in[k] = v
	}
	return in
}

// cancel cancels the run id of pipeline on engine.
func cancel(t *testing.T, engine, pipeline, id string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodDelete, engine+"/v1/pipelines/"+pipeline+"/run/"+id, nil)
	if tok := os.Getenv("DP_API_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode, "cancel %s", id)
}

func TestLiveRunWaitsForTheResult(t *testing.T) {
	engine, pipeline := live(t, "DP_ENGINE_URL", "DP_PIPELINE")
	tc, done, err := eval(t, liveInputs(engine, pipeline, nil))
	require.NoError(t, err)
	assert.True(t, done)
	assert.Contains(t, []interface{}{"completed", "partial"}, tc.GetOutput("status"))
	assert.Greater(t, tc.GetOutput("rowsProcessed").(int64), int64(0))
	assert.NotEmpty(t, tc.GetOutput("finishedAt"))
	result := tc.GetOutput("result").(map[string]interface{})
	assert.Equal(t, tc.GetOutput("runId"), result["runId"])
}

// The result posted where the run asked: here a receiver on loopback (an
// engine in full mode posts anywhere).
func TestLiveCallback(t *testing.T) {
	engine, pipeline := live(t, "DP_ENGINE_URL", "DP_PIPELINE")
	got := make(chan map[string]interface{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ev map[string]interface{}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &ev)
		got <- ev
	}))
	defer srv.Close()
	tc, _, err := eval(t, liveInputs(engine, pipeline, map[string]interface{}{"mode": "start", "callbackUrl": srv.URL + "/hooks/done"}))
	require.NoError(t, err)
	select {
	case ev := <-got:
		run := ev["run"].(map[string]interface{})
		assert.Equal(t, "run.finished", ev["event"])
		assert.Equal(t, tc.GetOutput("runId"), run["runId"])
		assert.Contains(t, []interface{}{"completed", "partial"}, run["status"])
	case <-time.After(30 * time.Second):
		t.Fatal("no result posted")
	}
}

// While the slow pipeline runs: skip is skipped, queue waits its turn and
// runs once the run in flight is cancelled; a retried run id follows its run.
func TestLiveOverlap(t *testing.T) {
	engine, pipeline := live(t, "DP_SLOW_ENGINE_URL", "DP_SLOW_PIPELINE")
	first := "live-first-" + newRunID()[:6]
	tc, _, err := eval(t, liveInputs(engine, pipeline, map[string]interface{}{"mode": "start", "runId": first}))
	require.NoError(t, err)
	require.Equal(t, "running", tc.GetOutput("status"))

	tc, _, err = eval(t, liveInputs(engine, pipeline, map[string]interface{}{"overlap": "skip"}))
	require.NoError(t, err)
	assert.Equal(t, "skipped", tc.GetOutput("status"))
	assert.Contains(t, tc.GetOutput("error"), first)

	queued := "live-queued-" + newRunID()[:6]
	tc, _, err = eval(t, liveInputs(engine, pipeline, map[string]interface{}{"mode": "start", "overlap": "queue", "runId": queued}))
	require.NoError(t, err)
	assert.Equal(t, "queued", tc.GetOutput("status"))

	// The same run id again, in start mode: the engine has it in flight, so
	// the activity follows it instead of running it twice.
	tc, _, err = eval(t, liveInputs(engine, pipeline, map[string]interface{}{"mode": "start", "runId": first}))
	require.NoError(t, err)
	assert.Equal(t, "running", tc.GetOutput("status"))

	cancel(t, engine, pipeline, first)
	c, err := newClient(http.DefaultClient, engine, os.Getenv("DP_API_TOKEN"))
	require.NoError(t, err)
	deadline := time.Now().Add(20 * time.Second)
	for {
		run, err := c.get(t.Context(), pipeline, queued)
		require.NoError(t, err)
		if run.Status == "running" {
			break
		}
		require.True(t, time.Now().Before(deadline), "the queued run did not start: %s", run.Status)
		time.Sleep(100 * time.Millisecond)
	}
	cancel(t, engine, pipeline, queued)
	run, err := c.wait(t.Context(), pipeline, queued, 20*time.Second, time.Second, t.Logf)
	require.NoError(t, err)
	assert.Equal(t, "cancelled", run.Status)
}

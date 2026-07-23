package restfireforget

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/project-flogo/core/support/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newActivity(t *testing.T, settings map[string]interface{}) *Activity {
	t.Helper()
	ictx := test.NewActivityInitContext(settings, nil)
	a, err := New(ictx)
	require.NoError(t, err)
	return a.(*Activity)
}

// The request must actually be dispatched to the endpoint.
func TestFireAndForget_DispatchesRequest(t *testing.T) {
	var count int32
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		got <- r.Method
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	a := newActivity(t, map[string]interface{}{})
	tc := test.NewActivityContext(a.Metadata())
	tc.SetInput("method", "POST")
	tc.SetInput("url", srv.URL)
	tc.SetInput("body", map[string]interface{}{"hello": "world"})

	done, err := a.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, true, tc.GetOutput("accepted"))

	select {
	case m := <-got:
		assert.Equal(t, "POST", m)
	case <-time.After(3 * time.Second):
		t.Fatal("server never received the fire-and-forget request")
	}
}

// Eval must return immediately, even if the server holds the response open.
func TestFireAndForget_DoesNotWaitForResponse(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	defer close(release)

	a := newActivity(t, map[string]interface{}{})
	tc := test.NewActivityContext(a.Metadata())
	tc.SetInput("method", "GET")
	tc.SetInput("url", srv.URL)

	start := time.Now()
	done, err := a.Eval(tc)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.True(t, done)
	assert.Less(t, elapsed, 500*time.Millisecond, "Eval must not wait for the response")
}

func TestFireAndForget_MissingURL(t *testing.T) {
	a := newActivity(t, map[string]interface{}{})
	tc := test.NewActivityContext(a.Metadata())
	tc.SetInput("method", "POST")
	tc.SetInput("url", "")

	done, err := a.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, false, tc.GetOutput("accepted"))
}

func TestFireAndForget_InvalidURL(t *testing.T) {
	a := newActivity(t, map[string]interface{}{})
	tc := test.NewActivityContext(a.Metadata())
	tc.SetInput("method", "GET")
	tc.SetInput("url", "not-a-url")

	done, err := a.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, false, tc.GetOutput("accepted"))
}

func TestEval_InvalidMethod(t *testing.T) {
	a := newActivity(t, map[string]interface{}{})
	tc := test.NewActivityContext(a.Metadata())
	tc.SetInput("method", "FOO")
	tc.SetInput("url", "http://example.com")

	done, err := a.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done) // rejected gracefully; no dispatch
	assert.Equal(t, false, tc.GetOutput("accepted"))
}

func TestEval_DefaultsMethodToPost(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Method
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	a := newActivity(t, map[string]interface{}{})
	tc := test.NewActivityContext(a.Metadata())
	tc.SetInput("url", srv.URL) // no method set → defaults to POST

	done, err := a.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, true, tc.GetOutput("accepted"))

	select {
	case m := <-got:
		assert.Equal(t, "POST", m)
	case <-time.After(3 * time.Second):
		t.Fatal("server never received the defaulted POST request")
	}
}

// When the concurrency limit is saturated, further requests are not dispatched
// and report accepted=false (the flow is not blocked).
func TestFireAndForget_ConcurrencyLimit(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // hold the first request open so it keeps its concurrency slot
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	defer close(release)

	a := newActivity(t, map[string]interface{}{"maxConcurrentRequests": 1})

	// First request acquires the only slot and is dispatched.
	tc1 := test.NewActivityContext(a.Metadata())
	tc1.SetInput("method", "GET")
	tc1.SetInput("url", srv.URL)
	done, err := a.Eval(tc1)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, true, tc1.GetOutput("accepted"))

	// Second request finds the limit saturated and is rejected (not dispatched).
	tc2 := test.NewActivityContext(a.Metadata())
	tc2.SetInput("method", "GET")
	tc2.SetInput("url", srv.URL)
	done, err = a.Eval(tc2)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, false, tc2.GetOutput("accepted"))
}

func TestMethodHelpers(t *testing.T) {
	assert.True(t, isValidMethod("GET"))
	assert.True(t, isValidMethod("OPTIONS"))
	assert.False(t, isValidMethod("TRACE"))
	assert.True(t, methodAllowsBody("POST"))
	assert.False(t, methodAllowsBody("GET"))
}

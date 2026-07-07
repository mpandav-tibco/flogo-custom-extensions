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

	a := newActivity(t, map[string]interface{}{"method": "POST"})
	tc := test.NewActivityContext(a.Metadata())
	tc.SetInput("url", srv.URL)
	tc.SetInput("body", map[string]interface{}{"hello": "world"})

	done, err := a.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)

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

	a := newActivity(t, map[string]interface{}{"method": "GET"})
	tc := test.NewActivityContext(a.Metadata())
	tc.SetInput("url", srv.URL)

	start := time.Now()
	done, err := a.Eval(tc)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.True(t, done)
	assert.Less(t, elapsed, 500*time.Millisecond, "Eval must not wait for the response")
}

func TestFireAndForget_MissingURL(t *testing.T) {
	a := newActivity(t, map[string]interface{}{"method": "POST"})
	tc := test.NewActivityContext(a.Metadata())
	tc.SetInput("url", "")

	done, err := a.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)
}

func TestFireAndForget_InvalidURL(t *testing.T) {
	a := newActivity(t, map[string]interface{}{"method": "GET"})
	tc := test.NewActivityContext(a.Metadata())
	tc.SetInput("url", "not-a-url")

	done, err := a.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)
}

func TestNew_InvalidMethod(t *testing.T) {
	ictx := test.NewActivityInitContext(map[string]interface{}{"method": "FOO"}, nil)
	_, err := New(ictx)
	assert.Error(t, err)
}

func TestNew_DefaultsMethodToPost(t *testing.T) {
	a := newActivity(t, map[string]interface{}{})
	assert.Equal(t, http.MethodPost, a.method)
}

func TestMethodHelpers(t *testing.T) {
	assert.True(t, isValidMethod("GET"))
	assert.True(t, isValidMethod("OPTIONS"))
	assert.False(t, isValidMethod("TRACE"))
	assert.True(t, methodAllowsBody("POST"))
	assert.False(t, methodAllowsBody("GET"))
}

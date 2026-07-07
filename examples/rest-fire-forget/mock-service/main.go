// Command mock-service is a tiny HTTP server used to verify the REST Fire &
// Forget activity end-to-end. It records every request it receives and exposes
// them at GET /_received so a test can confirm the fire-and-forget request
// actually arrived. The /slow path deliberately delays its response to prove the
// caller (the Flogo flow) does NOT wait for the response.
package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

type received struct {
	Time    string            `json:"time"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   string            `json:"query"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

var (
	mu    sync.Mutex
	store []received
)

func main() {
	addr := os.Getenv("MOCK_ADDR")
	if addr == "" {
		addr = ":18899"
	}

	mux := http.NewServeMux()

	// Inspection endpoint: list everything received so far.
	mux.HandleFunc("/_received", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"count": len(store), "requests": store})
	})

	// Clear the recorded requests.
	mux.HandleFunc("/_reset", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		store = nil
		mu.Unlock()
		_, _ = w.Write([]byte("ok"))
	})

	// Catch-all: record the request. /slow delays the response to prove the
	// fire-and-forget caller does not block on it.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		hdrs := make(map[string]string, len(r.Header))
		for k := range r.Header {
			hdrs[k] = r.Header.Get(k)
		}
		rec := received{
			Time:    time.Now().Format(time.RFC3339Nano),
			Method:  r.Method,
			Path:    r.URL.Path,
			Query:   r.URL.RawQuery,
			Headers: hdrs,
			Body:    string(body),
		}
		mu.Lock()
		store = append(store, rec)
		n := len(store)
		mu.Unlock()
		log.Printf("RECEIVED #%d  %s %s  query=%q  body=%s", n, r.Method, r.URL.Path, r.URL.RawQuery, string(body))

		if r.URL.Path == "/slow" {
			time.Sleep(3 * time.Second) // prove the caller didn't wait
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"accepted"}`))
	})

	log.Printf("mock fire-and-forget service listening on %s (GET /_received to inspect, /_reset to clear)", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

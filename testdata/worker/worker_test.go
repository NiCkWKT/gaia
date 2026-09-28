package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BabySid/aether/executor"
	"github.com/BabySid/aether/model"
	"github.com/BabySid/aether/wire"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkerHandlesBothExecutors(t *testing.T) {
	var mu sync.Mutex
	var starts []string
	var results []wire.TaskResult
	fetched := make(map[string]bool)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/task/fetch":
			var request struct {
				WorkerID string `json:"workerID"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			mu.Lock()
			defer mu.Unlock()
			kind := ""
			switch request.WorkerID {
			case "v1::mock-worker::image":
				kind = "image"
			case "v1::mock-worker::prompt":
				kind = "prompt"
			default:
				t.Errorf("unexpected worker ID %q", request.WorkerID)
			}
			if fetched[kind] {
				if _, err := io.WriteString(w, `{"errNo":0,"errMsg":"succ","data":{}}`); err != nil {
					t.Errorf("write response: %v", err)
				}
				return
			}
			fetched[kind] = true
			assignment := wire.TaskAssignment{TaskRunID: "task-" + kind, WorkflowRunID: "workflow", ExecutorType: kind, Inputs: inputs("hello", []string{})}
			if err := json.NewEncoder(w).Encode(struct {
				ErrNo  int                 `json:"errNo"`
				ErrMsg string              `json:"errMsg"`
				Data   wire.TaskAssignment `json:"data"`
			}{0, "succ", assignment}); err != nil {
				t.Errorf("write response: %v", err)
			}
		case "/task/start":
			var request struct{ WorkerID, TaskRunID string }
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			assert.Equal(t, "v1::mock-worker::"+strings.TrimPrefix(request.TaskRunID, "task-"), request.WorkerID)
			mu.Lock()
			starts = append(starts, request.TaskRunID)
			mu.Unlock()
			if _, err := io.WriteString(w, `{"errNo":0,"errMsg":"succ","data":{}}`); err != nil {
				t.Errorf("write response: %v", err)
			}
		case "/task/complete":
			var result wire.TaskResult
			require.NoError(t, json.NewDecoder(r.Body).Decode(&result))
			mu.Lock()
			results = append(results, result)
			mu.Unlock()
			if _, err := io.WriteString(w, `{"errNo":0,"errMsg":"succ","data":{}}`); err != nil {
				t.Errorf("write response: %v", err)
			}
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	reg := executor.NewRegistry()
	require.NoError(t, reg.Register(imageExecutor{}))
	require.NoError(t, reg.Register(promptExecutor{}))
	w := worker{client: client{baseURL: server.URL, http: server.Client()}, registry: reg, id: "mock-worker", logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, kind := range []string{"image", "prompt"} {
		assignment, err := w.client.fetch(t.Context(), "v1::mock-worker::"+kind)
		require.NoError(t, err)
		require.NotNil(t, assignment)
		require.NoError(t, w.handle(t.Context(), "v1::mock-worker::"+kind, assignment))
	}
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"task-image", "task-prompt"}, starts)
	require.Len(t, results, 2)
	assert.Equal(t, model.ExecCodeSucceeded, results[0].Code)
	assert.Equal(t, model.ExecCodeSucceeded, results[1].Code)
	assert.Equal(t, "uri", results[0].Parameters[0].Name)
	assert.Equal(t, "text", results[1].Parameters[0].Name)
}

func TestPollingEachExecutorWithOwnConcurrency(t *testing.T) {
	var mu sync.Mutex
	fetches := make(map[string]int)
	seen := make(chan struct{}, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			WorkerID string `json:"workerID"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode fetch: %v", err)
			return
		}
		mu.Lock()
		fetches[request.WorkerID]++
		mu.Unlock()
		select {
		case seen <- struct{}{}:
		default:
		}
		if _, err := io.WriteString(w, `{"errNo":0,"errMsg":"succ","data":{}}`); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	w := worker{client: client{baseURL: server.URL, http: server.Client()}, registry: executor.NewRegistry(), id: "mock-worker", concurrency: 2, interval: time.Hour, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	done := make(chan struct{})
	go func() { w.run(ctx); close(done) }()
	for range 4 {
		select {
		case <-seen:
		case <-time.After(5 * time.Second):
			cancel()
			<-done
			t.Fatal("four polling loops did not fetch")
		}
	}
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 2, fetches["v1::mock-worker::image"])
	assert.Equal(t, 2, fetches["v1::mock-worker::prompt"])
}

func TestClientEmptyQueueAndErrorEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/task/fetch" {
			if _, err := io.WriteString(w, `{"errNo":0,"errMsg":"succ","data":{}}`); err != nil {
				t.Errorf("write response: %v", err)
			}
			return
		}
		if _, err := io.WriteString(w, `{"errNo":2001,"errMsg":"dependency unavailable","data":{}}`); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	c := client{baseURL: server.URL, http: server.Client()}
	assignment, err := c.fetch(t.Context(), "v1::mock-worker::image")
	require.NoError(t, err)
	assert.Nil(t, assignment)
	assert.ErrorContains(t, c.start(t.Context(), "v1::mock-worker::image", "task"), "Gaia error 2001")
}

func TestFailedExecutionStillCompletes(t *testing.T) {
	var result wire.TaskResult
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/task/complete" {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&result))
		}
		if _, err := io.WriteString(w, `{"errNo":0,"errMsg":"succ","data":{}}`); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	reg := executor.NewRegistry()
	require.NoError(t, reg.Register(promptExecutor{}))
	w := worker{client: client{baseURL: server.URL, http: server.Client()}, registry: reg, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	assignment := &wire.TaskAssignment{TaskRunID: "task", WorkflowRunID: "workflow", ExecutorType: "prompt"}
	require.NoError(t, w.handle(t.Context(), "v1::mock-worker::prompt", assignment))
	assert.Equal(t, model.ExecCodeError, result.Code)
	assert.NotEmpty(t, result.Message)
}

func TestPollingStopsOnCancel(t *testing.T) {
	var mu sync.Mutex
	var count int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		if _, err := io.WriteString(w, `{"errNo":0,"errMsg":"succ","data":{}}`); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(t.Context())
	w := worker{client: client{baseURL: server.URL, http: server.Client()}, registry: executor.NewRegistry(), id: "mock-worker", concurrency: 2, interval: time.Hour, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	done := make(chan struct{})
	go func() { w.run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
	mu.Lock()
	defer mu.Unlock()
	assert.LessOrEqual(t, count, 4)
}

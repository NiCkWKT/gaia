package notification

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/BabySid/aether/errsink"
	"github.com/BabySid/aether/hook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testAdapter(t *testing.T, handler http.HandlerFunc) *Telegram {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	a, err := NewTelegram("test-token", "123", slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	a.url = srv.URL
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, a.Close(ctx))
	})
	return a
}

func TestNotify(t *testing.T) {
	messages := make(chan string, 1)
	a := testAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var body struct {
			ChatID string `json:"chat_id"`
			Text   string `json:"text"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "123", body.ChatID)
		messages <- body.Text
		_, err := w.Write([]byte(`{"ok":true}`))
		assert.NoError(t, err)
	})
	event := &hook.Event{HookType: hook.OnSuccess, Scope: hook.ScopeTask, WorkflowName: "billing", WorkflowRunID: "wf-1", TaskName: "charge", TaskRunID: "task-1", Template: "receipt"}
	require.NoError(t, a.Notify(t.Context(), event))
	assert.Equal(t, "Workflow billing (wf-1): onSuccess\nTask charge (task-1)\nHook template: receipt", <-messages)
	require.Error(t, a.Notify(t.Context(), nil))
}

func TestNotifyFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
	}{
		{"http failure", http.StatusTooManyRequests, `{"ok":false}`},
		{"api failure", http.StatusOK, `{"ok":false}`},
		{"bad response", http.StatusOK, `bad`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
				_, err := w.Write([]byte(tc.body))
				assert.NoError(t, err)
			})
			require.Error(t, a.Notify(t.Context(), &hook.Event{}))
		})
	}
}

func TestErrorSink(t *testing.T) {
	messages := make(chan string, 4)
	a := testAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		messages <- body.Text
		_, err := w.Write([]byte(`{"ok":true}`))
		assert.NoError(t, err)
	})
	a.OnError(t.Context(), errors.New("benign"), errsink.ErrorContext{Severity: errsink.SeverityWarning})
	a.OnError(t.Context(), errors.New("stalled"), errsink.ErrorContext{Severity: errsink.SeverityError, Operation: "dispatch", WorkflowRunID: "wf"})
	a.OnError(t.Context(), errors.New("hung"), errsink.ErrorContext{Severity: errsink.SeverityCritical})
	require.NoError(t, a.Close(t.Context()))
	assert.Contains(t, <-messages, "Operation: dispatch\nWorkflow: wf")
	assert.Contains(t, <-messages, "hung")
	assert.Empty(t, messages)
	a.OnError(t.Context(), errors.New("late"), errsink.ErrorContext{Severity: errsink.SeverityCritical})
	require.Error(t, a.Notify(t.Context(), &hook.Event{}))
}

func TestAlertQueueFullDoesNotBlock(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	a := testAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		_, err := w.Write([]byte(`{"ok":true}`))
		assert.NoError(t, err)
	})
	defer close(release)
	a.OnError(t.Context(), errors.New("first"), errsink.ErrorContext{Severity: errsink.SeverityError})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("delivery did not start")
	}
	for range queueCapacity + 10 {
		a.OnError(t.Context(), errors.New("overflow"), errsink.ErrorContext{Severity: errsink.SeverityCritical})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.ErrorIs(t, a.Close(ctx), context.Canceled)
}

func TestValidationAndCancellation(t *testing.T) {
	_, err := NewTelegram("bad/token", "123", slog.Default())
	require.Error(t, err)
	_, err = NewTelegram("token", "", slog.Default())
	require.Error(t, err)
	a := testAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(`{"ok":true}`))
		assert.NoError(t, err)
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.ErrorIs(t, a.Notify(ctx, &hook.Event{}), context.Canceled)
	assert.Error(t, a.Notify(t.Context(), &hook.Event{Template: strings.Repeat("x", 4097)}))
}

// Package notification adapts Aether lifecycle events and internal errors to delivery channels.
package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/BabySid/aether/errsink"
	"github.com/BabySid/aether/hook"
)

const queueCapacity = 64

// Telegram sends hooks synchronously and buffers serious internal error alerts.
// Call Close to drain queued alerts during shutdown.
type Telegram struct {
	client    *http.Client
	url       string
	chatID    string
	logger    *slog.Logger
	alerts    chan string
	done      chan struct{}
	workerCtx context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	closed    bool
}

var (
	_ hook.Notifier     = (*Telegram)(nil)
	_ errsink.ErrorSink = (*Telegram)(nil)
)

// NewTelegram constructs a Telegram adapter for a deployment-owned bot and chat.
func NewTelegram(token, chatID string, logger *slog.Logger) (*Telegram, error) {
	if token == "" || chatID == "" || logger == nil || strings.ContainsAny(token, "/?# \t\r\n") {
		return nil, errors.New("telegram: token, chat ID, and logger are required")
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	a := &Telegram{
		client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		url:       "https://api.telegram.org/bot" + token + "/sendMessage",
		chatID:    chatID,
		logger:    logger,
		alerts:    make(chan string, queueCapacity),
		done:      make(chan struct{}),
		workerCtx: workerCtx,
		cancel:    cancel,
	}
	go a.deliverAlerts()
	return a, nil
}

func (a *Telegram) Notify(ctx context.Context, event *hook.Event) error {
	if event == nil {
		return errors.New("telegram: nil hook event")
	}
	a.mu.Lock()
	closed := a.closed
	a.mu.Unlock()
	if closed {
		return errors.New("telegram: adapter closed")
	}
	message := fmt.Sprintf("Workflow %s (%s): %s", event.WorkflowName, event.WorkflowRunID, event.HookType)
	if event.Scope == hook.ScopeTask {
		message += fmt.Sprintf("\nTask %s (%s)", event.TaskName, event.TaskRunID)
	}
	if event.Template != "" {
		message += "\nHook template: " + event.Template
	}
	return a.send(ctx, message)
}

// OnError logs every error; only error and critical severities are sent to Telegram.
// It never waits for Telegram or blocks the Engine on queue capacity.
func (a *Telegram) OnError(ctx context.Context, err error, ec errsink.ErrorContext) {
	a.logger.ErrorContext(ctx, "aether internal error", "err", err, "severity", ec.Severity,
		"operation", ec.Operation, "workflow_run_id", ec.WorkflowRunID, "task_run_id", ec.TaskRunID)
	if ec.Severity < errsink.SeverityError {
		return
	}
	message := fmt.Sprintf("Aether internal error (severity %d)\nOperation: %s\nWorkflow: %s\nTask: %s\nError: %v",
		ec.Severity, ec.Operation, ec.WorkflowRunID, ec.TaskRunID, err)
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		a.logger.Warn("telegram alert dropped: adapter closed")
		return
	}
	select {
	case a.alerts <- message:
		a.mu.Unlock()
	default:
		a.mu.Unlock()
		a.logger.Warn("telegram alert dropped: queue full")
	}
}

func (a *Telegram) deliverAlerts() {
	defer close(a.done)
	for message := range a.alerts {
		if err := a.send(a.workerCtx, message); err != nil {
			a.logger.Warn("telegram alert delivery failed", "err", err)
		}
	}
}

// Close stops accepting alerts and attempts to drain the queue until ctx expires.
func (a *Telegram) Close(ctx context.Context) error {
	a.mu.Lock()
	if !a.closed {
		a.closed = true
		close(a.alerts)
	}
	a.mu.Unlock()
	select {
	case <-a.done:
		return nil
	case <-ctx.Done():
		a.cancel()
		return ctx.Err()
	}
}

func (a *Telegram) send(ctx context.Context, message string) error {
	if len([]rune(message)) > 4096 {
		return errors.New("telegram: message exceeds 4096 characters")
	}
	body, err := json.Marshal(struct {
		ChatID string `json:"chat_id"`
		Text   string `json:"text"`
	}{a.chatID, message})
	if err != nil {
		return fmt.Errorf("telegram: encode message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url, bytes.NewReader(body))
	if err != nil {
		return errors.New("telegram: construct request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// HTTP transport errors may include the bot token in the request URL.
		return errors.New("telegram: request failed")
	}
	defer resp.Body.Close() //nolint:errcheck // A response-body close cannot change delivery status.
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram: HTTP status %d", resp.StatusCode)
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return errors.New("telegram: invalid response")
	}
	if !result.OK {
		return errors.New("telegram: API rejected message")
	}
	return nil
}

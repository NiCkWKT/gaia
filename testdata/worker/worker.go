package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/BabySid/aether/executor"
	"github.com/BabySid/aether/model"
	"github.com/BabySid/aether/wire"
)

type client struct {
	baseURL string
	http    *http.Client
}

type response struct {
	ErrNo  int             `json:"errNo"`
	ErrMsg string          `json:"errMsg"`
	Data   json.RawMessage `json:"data"`
}

func (c client) post(ctx context.Context, path string, input any, output any) (err error) {
	body, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode %s request: %w", path, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create %s request: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", path, err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("post %s: HTTP %d", path, resp.StatusCode)
	}
	var envelope response
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&envelope); err != nil {
		return fmt.Errorf("decode %s response: %w", path, err)
	}
	if len(envelope.Data) == 0 || envelope.Data[0] != '{' {
		return fmt.Errorf("invalid %s response data", path)
	}
	if envelope.ErrNo != 0 {
		return fmt.Errorf("post %s: Gaia error %d: %s", path, envelope.ErrNo, envelope.ErrMsg)
	}
	if output != nil {
		if err := json.Unmarshal(envelope.Data, output); err != nil {
			return fmt.Errorf("decode %s data: %w", path, err)
		}
	}
	return nil
}

func (c client) fetch(ctx context.Context, workerID string) (*wire.TaskAssignment, error) {
	var assignment wire.TaskAssignment
	if err := c.post(ctx, "/task/fetch", struct {
		WorkerID string `json:"workerID"`
	}{workerID}, &assignment); err != nil {
		return nil, err
	}
	if assignment.TaskRunID == "" {
		return nil, nil
	}
	if assignment.WorkflowRunID == "" || assignment.ExecutorType == "" {
		return nil, errors.New("invalid task assignment")
	}
	return &assignment, nil
}

func (c client) start(ctx context.Context, workerID, taskRunID string) error {
	return c.post(ctx, "/task/start", struct {
		WorkerID  string `json:"workerID"`
		TaskRunID string `json:"taskRunID"`
	}{workerID, taskRunID}, nil)
}

func (c client) complete(ctx context.Context, result *wire.TaskResult) error {
	return c.post(ctx, "/task/complete", result, nil)
}

type worker struct {
	client      client
	registry    *executor.Registry
	id          string
	concurrency int
	interval    time.Duration
	logger      *slog.Logger
}

func (w worker) run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, kind := range []string{"image", "prompt"} {
		for range w.concurrency {
			wg.Go(func() { w.poll(ctx, kind) })
		}
	}
	wg.Wait()
}

func (w worker) poll(ctx context.Context, kind string) {
	workerID := "v1::" + w.id + "::" + kind
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		assignment, err := w.client.fetch(ctx, workerID)
		if err != nil {
			if ctx.Err() == nil {
				w.logger.Error("fetch task", "executor", kind, "err", err)
			}
		} else if assignment != nil {
			if err := w.handle(ctx, workerID, assignment); err != nil {
				if ctx.Err() == nil {
					w.logger.Error("task callback failed; stopping polling loop", "executor", kind, "taskRunID", assignment.TaskRunID, "err", err)
				}
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w worker) handle(ctx context.Context, workerID string, assignment *wire.TaskAssignment) error {
	if err := w.client.start(ctx, workerID, assignment.TaskRunID); err != nil {
		return fmt.Errorf("start task: %w", err)
	}
	result := &wire.TaskResult{TaskRunID: assignment.TaskRunID, WorkflowRunID: assignment.WorkflowRunID}
	plugin, ok := w.registry.Get(assignment.ExecutorType)
	if !ok || !strings.HasSuffix(workerID, "::"+assignment.ExecutorType) {
		result.ExecOutputs = &model.ExecOutputs{Code: model.ExecCodeError, Message: "unknown executor type"}
	} else {
		taskCtx := ctx
		if assignment.Timeout != "" {
			duration, err := time.ParseDuration(assignment.Timeout)
			if err != nil || duration <= 0 {
				result.ExecOutputs = &model.ExecOutputs{Code: model.ExecCodeError, Message: "invalid task timeout"}
			} else {
				var cancel context.CancelFunc
				taskCtx, cancel = context.WithTimeout(ctx, duration)
				defer cancel()
			}
		}
		if result.ExecOutputs == nil {
			outputs, err := plugin.Execute(taskCtx, &executor.ExecuteRequest{
				TaskRunID: assignment.TaskRunID, WorkflowRunID: assignment.WorkflowRunID,
				TaskName: assignment.TaskName, TemplateName: assignment.TemplateName,
				Inputs: assignment.Inputs, Resources: assignment.Resources,
				Timeout: assignment.Timeout, RetryCount: assignment.RetryCount,
			})
			if err != nil {
				code := model.ExecCodeError
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(taskCtx.Err(), context.DeadlineExceeded) {
					code = model.ExecCodeTimeout
				}
				result.ExecOutputs = &model.ExecOutputs{Code: code, Message: err.Error()}
			} else {
				result.ExecOutputs = outputs
			}
		}
	}
	if err := w.client.complete(ctx, result); err != nil {
		return fmt.Errorf("complete task: %w", err)
	}
	w.logger.Info("task reported", "executor", assignment.ExecutorType, "taskRunID", assignment.TaskRunID)
	return nil
}

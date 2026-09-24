// Package taskbroker provides an at-most-once Redis task queue for Aether.
package taskbroker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/BabySid/aether/broker"
	"github.com/BabySid/aether/wire"
	"github.com/redis/go-redis/v9"
)

var (
	ErrNoTaskAvailable = errors.New("no task available")
	ErrInvalidArgument = errors.New("invalid argument")
	ErrNotImplemented  = errors.New("not implemented")
	ErrClosed          = errors.New("broker closed")
)

// Broker delivers assignments at most once; it does not own its Redis client.
type Broker struct {
	client     *redis.Client
	prefix     string
	onStart    broker.StartHandler
	onComplete broker.CompletionHandler
	mu         sync.RWMutex
	closed     bool
}

var _ broker.TaskBroker = (*Broker)(nil)

// NewBroker constructs a broker using a caller-owned Redis client.
func NewBroker(client *redis.Client, prefix string, onStart broker.StartHandler, onComplete broker.CompletionHandler) (*Broker, error) {
	if client == nil || prefix == "" || onStart == nil || onComplete == nil {
		return nil, fmt.Errorf("new broker: %w", ErrInvalidArgument)
	}
	return &Broker{client: client, prefix: prefix, onStart: onStart, onComplete: onComplete}, nil
}

func (b *Broker) queueKey(executor string) string {
	return strconv.Itoa(len(b.prefix)) + ":" + b.prefix + ":tasks:" + executor
}

func (b *Broker) checkLocked(ctx context.Context) error {
	if b.closed {
		return ErrClosed
	}
	return ctx.Err()
}

func (b *Broker) Dispatch(ctx context.Context, assignment *wire.TaskAssignment) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if err := b.checkLocked(ctx); err != nil {
		return err
	}
	if assignment == nil || assignment.TaskRunID == "" || assignment.WorkflowRunID == "" || assignment.ExecutorType == "" {
		return fmt.Errorf("dispatch: %w", ErrInvalidArgument)
	}
	payload, err := json.Marshal(assignment)
	if err != nil {
		return fmt.Errorf("encode assignment: %w", err)
	}
	if err := b.client.ZAdd(ctx, b.queueKey(assignment.ExecutorType), redis.Z{Score: float64(assignment.Priority), Member: string(payload)}).Err(); err != nil {
		return fmt.Errorf("dispatch assignment: %w", err)
	}
	return nil
}

// FetchTask returns immediately on an empty queue rather than blocking.
func (b *Broker) FetchTask(ctx context.Context, workerID string) (*wire.TaskAssignment, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if err := b.checkLocked(ctx); err != nil {
		return nil, err
	}
	parts := strings.Split(workerID, "::")
	if len(parts) != 3 {
		return nil, fmt.Errorf("fetch task: %w: worker ID", ErrInvalidArgument)
	}
	version, identifier, executorType := parts[0], parts[1], parts[2]
	if version != "v1" || identifier == "" || executorType == "" {
		return nil, fmt.Errorf("fetch task: %w: worker ID", ErrInvalidArgument)
	}
	queue := b.queueKey(executorType)
	count, err := b.client.ZCard(ctx, queue).Result()
	if err != nil {
		return nil, fmt.Errorf("check task queue: %w", err)
	}
	if count == 0 {
		return nil, ErrNoTaskAvailable
	}
	members, err := b.client.ZRangeArgs(ctx, redis.ZRangeArgs{Key: queue, Start: 0, Stop: 0, Rev: true}).Result()
	if err != nil {
		return nil, fmt.Errorf("read task queue: %w", err)
	}
	if len(members) == 0 {
		return nil, ErrNoTaskAvailable
	}
	// TODO: Add leases and redelivery so Worker crashes after removal do not lose tasks.
	removed, err := b.client.ZRem(ctx, queue, members[0]).Result()
	if err != nil {
		return nil, fmt.Errorf("claim task: %w", err)
	}
	if removed == 0 {
		return nil, ErrNoTaskAvailable
	}
	var assignment wire.TaskAssignment
	if err := json.Unmarshal([]byte(members[0]), &assignment); err != nil {
		return nil, fmt.Errorf("decode claimed task: %w", err)
	}
	return &assignment, nil
}

func (b *Broker) StartTask(ctx context.Context, taskRunID, _ string) error {
	b.mu.RLock()
	err := b.checkLocked(ctx)
	b.mu.RUnlock()
	if err != nil {
		return err
	}
	// Callbacks may close the broker, so do not hold the lock while invoking them.
	b.onStart(ctx, taskRunID)
	return nil
}

func (b *Broker) CompleteTask(ctx context.Context, result *wire.TaskResult) error {
	b.mu.RLock()
	err := b.checkLocked(ctx)
	b.mu.RUnlock()
	if err != nil {
		return err
	}
	b.onComplete(ctx, result)
	return nil
}

func (b *Broker) Cancel(ctx context.Context, _ string) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if err := b.checkLocked(ctx); err != nil {
		return err
	}
	return ErrNotImplemented
}

// Close stops new operations without removing queued assignments or draining fetched work.
func (b *Broker) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	// TODO: Drain remotely executing tasks when a Worker protocol supports it.
	b.closed = true
	return nil
}

package taskbroker_test

import (
	"context"
	"errors"
	"fmt"
	"gaia/taskbroker"
	"net"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BabySid/aether/broker"
	"github.com/BabySid/aether/model"
	"github.com/BabySid/aether/wire"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func redisClient(t *testing.T) *redis.Client {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker is required for broker integration tests")
	}
	out, err := exec.Command("docker", "run", "-d", "--rm", "-p", "127.0.0.1::6379", "redis:7.2.16-alpine").CombinedOutput()
	if err != nil {
		t.Skipf("cannot start isolated Redis: %v: %s", err, out)
	}
	id := strings.TrimSpace(string(out))
	t.Cleanup(func() {
		stop, stopErr := exec.Command("docker", "stop", id).CombinedOutput()
		assert.NoError(t, stopErr, string(stop))
	})
	out, err = exec.Command("docker", "port", id, "6379/tcp").CombinedOutput()
	require.NoError(t, err, string(out))
	_, port, err := net.SplitHostPort(strings.TrimSpace(string(out)))
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: net.JoinHostPort("127.0.0.1", port)})
	t.Cleanup(func() {
		err := client.Close()
		assert.True(t, err == nil || errors.Is(err, redis.ErrClosed), "%v", err)
	})
	for range 100 {
		if err := client.Ping(t.Context()).Err(); err == nil {
			return client
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.NoError(t, client.Ping(t.Context()).Err())
	return client
}

func newBroker(t *testing.T, client *redis.Client) broker.TaskBroker {
	t.Helper()
	b, err := taskbroker.NewBroker(client, "gaia-test", func(context.Context, string) {}, func(context.Context, *wire.TaskResult) {})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, b.Close()) })
	return b
}

func assignment(id, executor string, priority int) *wire.TaskAssignment {
	return &wire.TaskAssignment{TaskRunID: id, WorkflowRunID: "workflow", ExecutorType: executor, Priority: priority}
}

func TestDispatchFetch(t *testing.T) {
	client := redisClient(t)
	b := newBroker(t, client)
	ctx := t.Context()
	complete := assignment("high", "echo", 2)
	complete.TaskName = "named"
	complete.TemplateName = "template"
	complete.Timeout = "30m"
	complete.RetryCount = 3
	complete.Inputs = &model.Inputs{}
	complete.Resources = &model.Resources{Memory: "512Mi"}
	for _, a := range []*wire.TaskAssignment{assignment("negative", "echo", -5), complete, assignment("zero", "echo", 0), assignment("other", "shell", 10)} {
		require.NoError(t, b.Dispatch(ctx, a))
	}
	for _, want := range []*wire.TaskAssignment{complete, assignment("zero", "echo", 0), assignment("negative", "echo", -5)} {
		got, err := b.FetchTask(ctx, "v1::worker-1::echo")
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	_, err := b.FetchTask(ctx, "v1::worker-1::echo")
	assert.ErrorIs(t, err, taskbroker.ErrNoTaskAvailable)
	got, err := b.FetchTask(ctx, "v1::worker-2::shell")
	require.NoError(t, err)
	assert.Equal(t, "other", got.TaskRunID)
}

func TestValidationAndRetry(t *testing.T) {
	b := newBroker(t, redisClient(t))
	ctx := t.Context()
	for _, a := range []*wire.TaskAssignment{nil, {}, {TaskRunID: "id", ExecutorType: "echo"}, {TaskRunID: "id", WorkflowRunID: "wf"}} {
		assert.ErrorIs(t, b.Dispatch(ctx, a), taskbroker.ErrInvalidArgument)
	}
	for _, id := range []string{"", "v2::worker::echo", "v1::worker::", "v1::::echo", "v1::worker::echo::extra", "v1:worker:echo"} {
		_, err := b.FetchTask(ctx, id)
		assert.ErrorIs(t, err, taskbroker.ErrInvalidArgument, id)
	}
	a := assignment("retry", "echo", 1)
	for range 2 {
		require.NoError(t, b.Dispatch(ctx, a))
		got, err := b.FetchTask(ctx, "v1::worker::echo")
		require.NoError(t, err)
		assert.Equal(t, a, got)
	}
}

func TestConcurrentFetchAndIsolation(t *testing.T) {
	client := redisClient(t)
	first := newBroker(t, client)
	second := newBroker(t, client)
	a := assignment("one", "echo", 1)
	require.NoError(t, first.Dispatch(t.Context(), a))
	var wg sync.WaitGroup
	results := make(chan *wire.TaskAssignment, 20)
	errs := make(chan error, 20)
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b := first
			if i%2 == 0 {
				b = second
			}
			got, err := b.FetchTask(context.Background(), fmt.Sprintf("v1::worker-%d::echo", i))
			results <- got
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	count := 0
	for got := range results {
		if got != nil {
			count++
			assert.Equal(t, a, got)
		}
	}
	assert.Equal(t, 1, count)
	for err := range errs {
		if err != nil {
			assert.ErrorIs(t, err, taskbroker.ErrNoTaskAvailable)
		}
	}
	other, err := taskbroker.NewBroker(client, "other-deployment", func(context.Context, string) {}, func(context.Context, *wire.TaskResult) {})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, other.Close()) })
	require.NoError(t, first.Dispatch(t.Context(), a))
	_, err = other.FetchTask(t.Context(), "v1::worker::echo")
	assert.ErrorIs(t, err, taskbroker.ErrNoTaskAvailable)
	got, err := second.FetchTask(t.Context(), "v1::worker::echo")
	require.NoError(t, err)
	assert.Equal(t, a, got)
}

func TestConstructorAndDurability(t *testing.T) {
	client := redisClient(t)
	start := func(context.Context, string) {}
	complete := func(context.Context, *wire.TaskResult) {}
	for _, tc := range []struct {
		client *redis.Client
		prefix string
		start  func(context.Context, string)
		done   func(context.Context, *wire.TaskResult)
	}{
		{nil, "prefix", start, complete},
		{client, "", start, complete},
		{client, "prefix", nil, complete},
		{client, "prefix", start, nil},
	} {
		_, err := taskbroker.NewBroker(tc.client, tc.prefix, tc.start, tc.done)
		assert.ErrorIs(t, err, taskbroker.ErrInvalidArgument)
	}
	first := newBroker(t, client)
	a := assignment("durable", "echo", 1)
	require.NoError(t, first.Dispatch(t.Context(), a))
	require.NoError(t, first.Close())
	second := newBroker(t, client)
	got, err := second.FetchTask(t.Context(), "v1::worker::echo")
	require.NoError(t, err)
	assert.Equal(t, a, got)
}

func TestCallbacksContextAndClose(t *testing.T) {
	client := redisClient(t)
	var starts []string
	var completions []*wire.TaskResult
	b, err := taskbroker.NewBroker(client, "callbacks", func(_ context.Context, id string) { starts = append(starts, id) }, func(_ context.Context, result *wire.TaskResult) { completions = append(completions, result) })
	require.NoError(t, err)
	var port broker.TaskBroker = b
	result := &wire.TaskResult{TaskRunID: "run", WorkflowRunID: "wf", ExecOutputs: &model.ExecOutputs{Message: "done"}}
	require.NoError(t, port.StartTask(t.Context(), "run", "v1::worker::echo"))
	require.NoError(t, port.CompleteTask(t.Context(), result))
	assert.Equal(t, []string{"run"}, starts)
	assert.Equal(t, []*wire.TaskResult{result}, completions)
	assert.ErrorIs(t, port.Cancel(t.Context(), "run"), taskbroker.ErrNotImplemented)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.ErrorIs(t, port.Dispatch(ctx, assignment("canceled", "echo", 1)), context.Canceled)
	_, err = port.FetchTask(ctx, "v1::worker::echo")
	assert.ErrorIs(t, err, context.Canceled)
	assert.ErrorIs(t, port.StartTask(ctx, "run", "v1::worker::echo"), context.Canceled)
	assert.ErrorIs(t, port.CompleteTask(ctx, result), context.Canceled)
	assert.Len(t, starts, 1)
	assert.Len(t, completions, 1)
	require.NoError(t, port.Close())
	assert.NoError(t, port.Close())
	assert.ErrorIs(t, port.Dispatch(t.Context(), assignment("x", "echo", 1)), taskbroker.ErrClosed)
	_, err = port.FetchTask(t.Context(), "v1::worker::echo")
	assert.ErrorIs(t, err, taskbroker.ErrClosed)
	assert.ErrorIs(t, port.StartTask(t.Context(), "run", "worker"), taskbroker.ErrClosed)
	assert.ErrorIs(t, port.CompleteTask(t.Context(), result), taskbroker.ErrClosed)
	assert.ErrorIs(t, port.Cancel(t.Context(), "run"), taskbroker.ErrClosed)
	assert.NoError(t, client.Ping(t.Context()).Err()) // caller retains ownership of the Redis client
}

func TestRedisFailureIsNotEmpty(t *testing.T) {
	client := redisClient(t)
	b := newBroker(t, client)
	require.NoError(t, client.Close())
	assert.Error(t, b.Dispatch(t.Context(), assignment("id", "echo", 1)))
	_, err := b.FetchTask(t.Context(), "v1::worker::echo")
	assert.Error(t, err)
	assert.False(t, errors.Is(err, taskbroker.ErrNoTaskAvailable))
}

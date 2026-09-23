package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"gaia/store"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BabySid/aether/model"
	aether "github.com/BabySid/aether/store"
	_ "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testStore(t *testing.T) aether.Store {
	t.Helper()
	dsn := os.Getenv("GAIA_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set GAIA_TEST_MYSQL_DSN to an isolated MySQL 8.4 database (parseTime=true&loc=UTC&time_zone=%27%2B00%3A00%27)")
	}
	db, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	var zone string
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT @@session.time_zone").Scan(&zone))
	require.Equal(t, "+00:00", zone)
	migration, err := os.ReadFile("migrations/000001_create_store.up.sql")
	require.NoError(t, err)
	for _, table := range []string{"task_runs", "workflow_runs", "executor_schemas"} {
		_, err = db.ExecContext(t.Context(), "DROP TABLE IF EXISTS "+table)
		require.NoError(t, err)
	}
	for _, statement := range strings.Split(string(migration), ";") {
		if strings.TrimSpace(statement) != "" {
			_, err = db.ExecContext(t.Context(), statement)
			require.NoError(t, err)
		}
	}
	return store.New(db)
}

func TestStoreReopenDurability(t *testing.T) {
	dsn := os.Getenv("GAIA_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set GAIA_TEST_MYSQL_DSN to an isolated MySQL 8.4 database")
	}
	db, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	migration, err := os.ReadFile("migrations/000001_create_store.up.sql")
	require.NoError(t, err)
	for _, table := range []string{"task_runs", "workflow_runs", "executor_schemas"} {
		_, err = db.ExecContext(t.Context(), "DROP TABLE IF EXISTS "+table)
		require.NoError(t, err)
	}
	for _, statement := range strings.Split(string(migration), ";") {
		if strings.TrimSpace(statement) != "" {
			_, err = db.ExecContext(t.Context(), statement)
			require.NoError(t, err)
		}
	}
	first := store.New(db)
	require.NoError(t, first.CreateWorkflowRun(t.Context(), &aether.WorkflowRun{RunID: "durable", Workflow: json.RawMessage(`{}`)}))
	require.NoError(t, first.UpsertSchema(t.Context(), "worker", model.ExecutorSchema{Type: "echo", Version: "1"}))
	require.NoError(t, first.Close())

	reopenedDB, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	reopened := store.New(reopenedDB)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	_, err = reopened.GetWorkflowRun(t.Context(), "durable")
	require.NoError(t, err)
	schemas, err := reopened.ListSchemas(t.Context())
	require.NoError(t, err)
	require.Len(t, schemas, 1)
}

func TestStoreHonorsCanceledContext(t *testing.T) {
	s := testStore(t)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := s.GetWorkflowRun(ctx, "missing")
	assert.ErrorIs(t, err, context.Canceled)
}

func TestWorkflowPersistence(t *testing.T) {
	s := testStore(t)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	ctx := t.Context()
	empty, err := s.ListActiveWorkflowRuns(ctx)
	require.NoError(t, err)
	assert.NotNil(t, empty)
	deadline := time.Now().Add(-time.Hour)
	run := &aether.WorkflowRun{RunID: "W", Workflow: json.RawMessage(`{"spec":{"nested":true}}`), CronWorkflowID: "cron", CreatedAt: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), Deadline: &deadline}
	require.NoError(t, s.CreateWorkflowRun(ctx, run))
	got, err := s.GetWorkflowRun(ctx, "W")
	require.NoError(t, err)
	assert.Equal(t, uint64(0), got.Token)
	assert.Equal(t, "cron", got.CronWorkflowID)
	assert.NotEqual(t, run.CreatedAt, got.CreatedAt)
	assert.WithinDuration(t, time.Now(), got.CreatedAt, 5*time.Second)
	active, err := s.ListActiveWorkflowRuns(ctx)
	require.NoError(t, err)
	require.Len(t, active, 1)
	update, err := s.UpdateWorkflowRun(ctx, &aether.WorkflowRun{RunID: "W", Token: 0})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), update.Token)
	assert.NotNil(t, update.Deadline)
	msg := "done"
	phase := model.PhaseSucceeded
	update, err = s.UpdateWorkflowRun(ctx, &aether.WorkflowRun{RunID: "W", Token: 1, Message: &msg, Status: &phase, Outputs: &model.Outputs{ExecOutputs: model.ExecOutputs{Message: "output"}}})
	require.NoError(t, err)
	assert.Equal(t, uint64(2), update.Token)
	assert.Equal(t, "output", update.Outputs.Message)
	_, err = s.UpdateWorkflowRun(ctx, &aether.WorkflowRun{RunID: "W", Token: 0})
	assert.ErrorIs(t, err, aether.ErrTokenMismatch)
	active, err = s.ListActiveWorkflowRuns(ctx)
	require.NoError(t, err)
	assert.Empty(t, active)
	byCron, err := s.ListWorkflowRunsByCronID(ctx, "cron")
	require.NoError(t, err)
	require.Len(t, byCron, 1)
	assert.Equal(t, "done", *byCron[0].Message)
	_, err = s.GetWorkflowRun(ctx, "missing")
	assert.ErrorIs(t, err, aether.ErrNotFound)
	assert.ErrorIs(t, s.CreateWorkflowRun(ctx, run), store.ErrAlreadyExists)
	assert.ErrorIs(t, s.CreateWorkflowRun(ctx, &aether.WorkflowRun{RunID: "bad", Workflow: json.RawMessage(`[]`)}), store.ErrInvalidArgument)
	assert.ErrorIs(t, s.DeleteWorkflowRun(ctx, "W"), store.ErrNotImplemented)
}

func TestTaskIdentityAndUpdates(t *testing.T) {
	s := testStore(t)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	ctx := t.Context()
	require.NoError(t, s.CreateWorkflowRun(ctx, &aether.WorkflowRun{RunID: "wf", Workflow: json.RawMessage(`{}`)}))
	task := &aether.TaskRun{RunID: "a", WorkflowRunID: "wf", TaskName: "do", TemplateName: "do", TemplateType: "task", Inputs: &model.Inputs{}}
	require.NoError(t, s.CreateTaskRun(ctx, task))
	other := *task
	other.RunID = "b"
	require.NoError(t, s.CreateTaskRun(ctx, &other))
	_, err := s.GetTaskRun(ctx, "b")
	assert.ErrorIs(t, err, aether.ErrNotFound)
	other.TaskName = "other"
	other.RunID = "a"
	assert.ErrorIs(t, s.CreateTaskRun(ctx, &other), store.ErrAlreadyExists)
	other.RunID = "b"
	require.NoError(t, s.CreateTaskRun(ctx, &other))
	got, err := s.GetTaskRun(ctx, "a")
	require.NoError(t, err)
	assert.Nil(t, got.RetryCount)
	assert.Equal(t, uint64(0), got.Token)
	count := 0
	deadline := time.Now().Add(time.Hour)
	phase := model.PhaseSuspended
	changed, err := s.UpdateTaskRun(ctx, &aether.TaskRun{RunID: "a", RetryCount: &count, Deadline: &deadline, Status: &phase})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), changed.Token)
	require.NotNil(t, changed.RetryCount)
	assert.Zero(t, *changed.RetryCount)
	active, err := s.ListActiveTaskRuns(ctx)
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, "a", active[0].RunID)
	_, err = s.UpdateTaskRun(ctx, &aether.TaskRun{RunID: "a", Token: 0})
	assert.ErrorIs(t, err, aether.ErrTokenMismatch)
	rows, err := s.ListTaskRunsByParent(ctx, "wf", "")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "a", rows[0].RunID)
	assert.Error(t, s.CreateTaskRun(ctx, &aether.TaskRun{RunID: "orphan", WorkflowRunID: "missing", TaskName: "do", TemplateName: "do", TemplateType: "task"})) // foreign key error must NOT be classified as duplicate
}

func TestSchemasAndConcurrentUpdates(t *testing.T) {
	s := testStore(t)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	ctx := t.Context()
	require.NoError(t, s.UpsertSchema(ctx, "", model.ExecutorSchema{Type: "echo", Version: "1"}))
	require.NoError(t, s.UpsertSchema(ctx, "", model.ExecutorSchema{Type: "echo", Version: "2"}))
	schemas, err := s.ListSchemas(ctx)
	require.NoError(t, err)
	require.Len(t, schemas, 1)
	assert.Equal(t, "2", schemas[0].Schema.Version)
	assert.ErrorIs(t, s.UpsertSchema(ctx, "", model.ExecutorSchema{}), store.ErrInvalidArgument)
	require.NoError(t, s.CreateWorkflowRun(ctx, &aether.WorkflowRun{RunID: "race", Workflow: json.RawMessage(`{}`)}))
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.UpdateWorkflowRun(context.Background(), &aether.WorkflowRun{RunID: "race", Token: 0})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success, stale := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, aether.ErrTokenMismatch) {
			stale++
		} else {
			require.NoError(t, err)
		}
	}
	assert.Equal(t, 1, success)
	assert.Equal(t, 1, stale)
}

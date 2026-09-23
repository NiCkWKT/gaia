package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"gaia/store/db"
	"math"
	"strconv"

	"github.com/BabySid/aether/model"
	aether "github.com/BabySid/aether/store"
	"github.com/go-sql-driver/mysql"
)

func (s *Store) CreateWorkflowRun(ctx context.Context, r *aether.WorkflowRun) error {
	if err := validWorkflow(r, true); err != nil {
		return err
	}
	jsonValues, err := columns(r.Outputs, r.Metrics)
	if err != nil {
		return err
	}
	err = s.queries.CreateWorkflow(ctx, db.CreateWorkflowParams{RunID: r.RunID, Workflow: r.Workflow, CronWorkflowID: r.CronWorkflowID, Status: phase(r.Status), Message: str(r.Message), Outputs: jsonValues[0], Metrics: jsonValues[1], Deadline: stamp(r.Deadline)})
	var me *mysql.MySQLError
	if errors.As(err, &me) && me.Number == 1062 {
		return fmt.Errorf("create workflow %q: %w", r.RunID, ErrAlreadyExists)
	}
	if err != nil {
		return fmt.Errorf("create workflow %q: %w", r.RunID, err)
	}
	return nil
}

func (s *Store) GetWorkflowRun(ctx context.Context, id string) (*aether.WorkflowRun, error) {
	if !validID(id, true, 191) {
		return nil, invalid("workflow ID")
	}
	v, err := s.queries.GetWorkflow(ctx, id)
	if err != nil {
		return nil, failure("get workflow", id, err)
	}
	return readWorkflow(v)
}

func (s *Store) UpdateWorkflowRun(ctx context.Context, r *aether.WorkflowRun) (*aether.WorkflowRun, error) {
	if err := validWorkflow(r, false); err != nil {
		return nil, err
	}
	jsonValues, err := columns(r.Outputs, r.Metrics)
	if err != nil {
		return nil, err
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // Rollback after commit returns sql.ErrTxDone.
	q := s.queries.WithTx(tx)
	n, err := q.UpdateWorkflow(ctx, db.UpdateWorkflowParams{Status: phase(r.Status), Message: str(r.Message), Outputs: jsonValues[0], Metrics: jsonValues[1], Deadline: stamp(r.Deadline), RunID: r.RunID, Column7: strconv.FormatUint(r.Token, 10)})
	if err != nil {
		return nil, fmt.Errorf("update workflow %q: %w", r.RunID, err)
	}
	if n == 0 {
		// A locking read serializes classification with concurrent writers.
		v, err := q.GetWorkflowForUpdate(ctx, r.RunID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, missing("update workflow", r.RunID)
		}
		if err != nil {
			return nil, err
		}
		if v.Token == math.MaxUint64 && r.Token == v.Token {
			return nil, fmt.Errorf("update workflow %q: %w", r.RunID, ErrTokenOverflow)
		}
		return nil, fmt.Errorf("update workflow %q: %w", r.RunID, aether.ErrTokenMismatch)
	}
	v, err := q.GetWorkflow(ctx, r.RunID)
	if err != nil {
		return nil, err
	}
	result, err := readWorkflow(v)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) ListActiveWorkflowRuns(ctx context.Context) ([]*aether.WorkflowRun, error) {
	v, err := s.queries.ListActiveWorkflows(ctx)
	if err != nil {
		return nil, err
	}
	return workflows(v)
}

func (s *Store) ListWorkflowRunsByCronID(ctx context.Context, id string) ([]*aether.WorkflowRun, error) {
	if !validID(id, true, 191) {
		return nil, invalid("cron ID")
	}
	v, err := s.queries.ListCronWorkflowsRuns(ctx, id)
	if err != nil {
		return nil, err
	}
	return workflows(v)
}

func workflows(v []db.WorkflowRun) ([]*aether.WorkflowRun, error) {
	result := make([]*aether.WorkflowRun, 0, len(v))
	for _, row := range v {
		r, err := readWorkflow(row)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, nil
}

func (s *Store) CreateTaskRun(ctx context.Context, r *aether.TaskRun) error {
	if err := validTask(r, true); err != nil {
		return err
	}
	jsonValues, err := columns(r.Inputs, r.Outputs, r.Metrics)
	if err != nil {
		return err
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // Rollback after commit returns sql.ErrTxDone.
	q := s.queries.WithTx(tx)
	err = q.CreateTask(ctx, db.CreateTaskParams{RunID: r.RunID, WorkflowRunID: r.WorkflowRunID, ParentRunID: r.ParentRunID, Depth: uint32(r.Depth), Scope: r.Scope, TaskName: r.TaskName, TemplateName: r.TemplateName, TemplateType: r.TemplateType, Inputs: jsonValues[0], Status: phase(r.Status), Message: str(r.Message), Outputs: jsonValues[1], Metrics: jsonValues[2], RetryCount: retry(r.RetryCount), Deadline: stamp(r.Deadline)})
	if err == nil {
		return tx.Commit()
	}
	var me *mysql.MySQLError
	if !errors.As(err, &me) || me.Number != 1062 {
		return fmt.Errorf("create task %q: %w", r.RunID, err)
	}
	byID, idErr := q.GetTask(ctx, r.RunID)
	byKey, keyErr := q.GetTaskIdentity(ctx, db.GetTaskIdentityParams{WorkflowRunID: r.WorkflowRunID, ParentRunID: r.ParentRunID, Scope: r.Scope, TaskName: r.TaskName})
	if idErr != nil && !errors.Is(idErr, sql.ErrNoRows) {
		return idErr
	}
	if keyErr != nil && !errors.Is(keyErr, sql.ErrNoRows) {
		return keyErr
	}
	if idErr == nil && (byID.WorkflowRunID != r.WorkflowRunID || byID.ParentRunID != r.ParentRunID || byID.Scope != r.Scope || byID.TaskName != r.TaskName) {
		return fmt.Errorf("create task %q: %w", r.RunID, ErrAlreadyExists)
	}
	if keyErr == nil && (idErr != nil || byKey.RunID == r.RunID) {
		return tx.Commit()
	}
	return fmt.Errorf("create task %q: %w", r.RunID, ErrAlreadyExists)
}

func (s *Store) GetTaskRun(ctx context.Context, id string) (*aether.TaskRun, error) {
	if !validID(id, true, 191) {
		return nil, invalid("task ID")
	}
	v, err := s.queries.GetTask(ctx, id)
	if err != nil {
		return nil, failure("get task", id, err)
	}
	return readTask(v)
}

func (s *Store) UpdateTaskRun(ctx context.Context, r *aether.TaskRun) (*aether.TaskRun, error) {
	if err := validTask(r, false); err != nil {
		return nil, err
	}
	jsonValues, err := columns(r.Inputs, r.Outputs, r.Metrics)
	if err != nil {
		return nil, err
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // Rollback after commit returns sql.ErrTxDone.
	q := s.queries.WithTx(tx)
	n, err := q.UpdateTask(ctx, db.UpdateTaskParams{Inputs: jsonValues[0], Status: phase(r.Status), Message: str(r.Message), Outputs: jsonValues[1], Metrics: jsonValues[2], RetryCount: retry(r.RetryCount), Deadline: stamp(r.Deadline), RunID: r.RunID, Column9: strconv.FormatUint(r.Token, 10)})
	if err != nil {
		return nil, fmt.Errorf("update task %q: %w", r.RunID, err)
	}
	if n == 0 {
		v, err := q.GetTaskForUpdate(ctx, r.RunID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, missing("update task", r.RunID)
		}
		if err != nil {
			return nil, err
		}
		if v.Token == math.MaxUint64 && r.Token == v.Token {
			return nil, fmt.Errorf("update task %q: %w", r.RunID, ErrTokenOverflow)
		}
		return nil, fmt.Errorf("update task %q: %w", r.RunID, aether.ErrTokenMismatch)
	}
	v, err := q.GetTask(ctx, r.RunID)
	if err != nil {
		return nil, err
	}
	result, err := readTask(v)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) ListTaskRuns(ctx context.Context, id string) ([]*aether.TaskRun, error) {
	if !validID(id, true, 191) {
		return nil, invalid("workflow ID")
	}
	v, err := s.queries.ListTasks(ctx, id)
	if err != nil {
		return nil, err
	}
	return tasks(v)
}

func (s *Store) ListTaskRunsByParent(ctx context.Context, id, parent string) ([]*aether.TaskRun, error) {
	if !validID(id, true, 191) || !validID(parent, false, 191) {
		return nil, invalid("parent identity")
	}
	v, err := s.queries.ListParentTasks(ctx, db.ListParentTasksParams{WorkflowRunID: id, ParentRunID: parent})
	if err != nil {
		return nil, err
	}
	return tasks(v)
}

func (s *Store) ListActiveTaskRuns(ctx context.Context) ([]*aether.TaskRun, error) {
	v, err := s.queries.ListActiveTasks(ctx)
	if err != nil {
		return nil, err
	}
	return tasks(v)
}

func tasks(v []db.TaskRun) ([]*aether.TaskRun, error) {
	result := make([]*aether.TaskRun, 0, len(v))
	for _, row := range v {
		r, err := readTask(row)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, nil
}

func (s *Store) UpsertSchema(ctx context.Context, worker string, schema model.ExecutorSchema) error {
	if !validID(worker, false, 191) || !validID(schema.Type, true, 191) {
		return invalid("schema identity")
	}
	data, err := encode(schema)
	if err != nil {
		return err
	}
	return s.queries.UpsertSchema(ctx, db.UpsertSchemaParams{ExecutorType: schema.Type, WorkerID: worker, SchemaJson: data})
}

func (s *Store) ListSchemas(ctx context.Context) ([]aether.SchemaRecord, error) {
	rows, err := s.queries.ListSchemas(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]aether.SchemaRecord, 0, len(rows))
	for _, row := range rows {
		schema, err := decode[model.ExecutorSchema](sql.NullString{String: string(row.SchemaJson), Valid: true})
		if err != nil {
			return nil, err
		}
		if schema == nil {
			return nil, invalid("persisted schema")
		}
		result = append(result, aether.SchemaRecord{WorkerID: row.WorkerID, Schema: *schema})
	}
	return result, nil
}
func (s *Store) DeleteSchema(context.Context, string, string) error { return ErrNotImplemented }
func (s *Store) DeleteWorkflowRun(context.Context, string) error    { return ErrNotImplemented }
func (s *Store) CreateCronWorkflow(context.Context, *aether.CronWorkflowRecord) error {
	return ErrNotImplemented
}

func (s *Store) GetCronWorkflow(context.Context, string) (*aether.CronWorkflowRecord, error) {
	return nil, ErrNotImplemented
}

func (s *Store) UpdateCronWorkflow(context.Context, *aether.CronWorkflowRecord) error {
	return ErrNotImplemented
}
func (s *Store) DeleteCronWorkflow(context.Context, string) error { return ErrNotImplemented }
func (s *Store) ListCronWorkflows(context.Context) ([]*aether.CronWorkflowRecord, error) {
	return nil, ErrNotImplemented
}

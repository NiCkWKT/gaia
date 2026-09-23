package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"gaia/store/db"
	"math"
	"reflect"
	"time"

	"github.com/BabySid/aether/model"
	aether "github.com/BabySid/aether/store"
)

var (
	ErrAlreadyExists   = errors.New("already exists")
	ErrInvalidArgument = errors.New("invalid argument")
	ErrNotImplemented  = errors.New("not implemented")
	ErrTokenOverflow   = errors.New("token overflow")
)

type Store struct {
	database *sql.DB
	queries  *db.Queries
}

var _ aether.Store = (*Store)(nil)

// New takes ownership of database. The caller must configure UTC sessions and parseTime before opening it.
func New(database *sql.DB) *Store { return &Store{database: database, queries: db.New(database)} }
func (s *Store) Close() error     { return s.database.Close() }

func validID(v string, required bool, limit int) bool {
	return (!required || v != "") && len([]rune(v)) <= limit && len(v) <= limit*4
}
func invalid(name string) error { return fmt.Errorf("%s: %w", name, ErrInvalidArgument) }
func validPhase(p *model.Phase) bool {
	if p == nil {
		return true
	}
	switch *p {
	case model.PhaseCreated, model.PhaseReady, model.PhaseRunning, model.PhaseSuspended, model.PhaseSucceeded, model.PhaseFailed, model.PhaseError, model.PhaseTimeout, model.PhaseSkipped, model.PhaseCancelled:
		return true
	}
	return false
}

func phase(p *model.Phase) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: string(*p), Valid: true}
}

func str(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}

func stamp(p *time.Time) sql.NullTime {
	if p == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: p.UTC(), Valid: true}
}

func jsonColumn(v any) (sql.NullString, error) {
	b, err := encode(v)
	if err != nil {
		return sql.NullString{}, err
	}
	if b == nil {
		return sql.NullString{}, nil
	}
	return sql.NullString{String: string(b), Valid: true}, nil
}

func columns(values ...any) ([]sql.NullString, error) {
	result := make([]sql.NullString, 0, len(values))
	for _, v := range values {
		c, err := jsonColumn(v)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}

func isJSONObject(b []byte) bool {
	b = bytes.TrimSpace(b)
	return len(b) > 1 && b[0] == '{' && b[len(b)-1] == '}' && json.Valid(b)
}

func encode(v any) (json.RawMessage, error) {
	if v == nil || reflect.ValueOf(v).Kind() == reflect.Pointer && reflect.ValueOf(v).IsNil() {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil || !isJSONObject(b) {
		return nil, invalid("JSON object")
	}
	return b, nil
}

func decode[T any](b sql.NullString) (*T, error) {
	if !b.Valid {
		return nil, nil
	}
	if !isJSONObject([]byte(b.String)) {
		return nil, invalid("JSON object")
	}
	var v T
	if err := json.Unmarshal([]byte(b.String), &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func readPhase(v sql.NullString) (*model.Phase, error) {
	if !v.Valid {
		return nil, nil
	}
	p := model.Phase(v.String)
	if !validPhase(&p) {
		return nil, fmt.Errorf("unknown persisted phase %q", v.String)
	}
	return &p, nil
}

func readString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func readTime(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time.UTC()
	return &t
}

func readWorkflow(v db.WorkflowRun) (*aether.WorkflowRun, error) {
	p, err := readPhase(v.Status)
	if err != nil {
		return nil, err
	}
	o, err := decode[model.Outputs](v.Outputs)
	if err != nil {
		return nil, err
	}
	m, err := decode[model.Metrics](v.Metrics)
	if err != nil {
		return nil, err
	}
	return &aether.WorkflowRun{RunID: v.RunID, Workflow: v.Workflow, CreatedAt: v.CreatedAt.UTC(), CronWorkflowID: v.CronWorkflowID, Status: p, Message: readString(v.Message), Outputs: o, Metrics: m, Deadline: readTime(v.Deadline), Token: v.Token, UpdatedAt: v.UpdatedAt.UTC()}, nil
}

func readTask(v db.TaskRun) (*aether.TaskRun, error) {
	p, err := readPhase(v.Status)
	if err != nil {
		return nil, err
	}
	i, err := decode[model.Inputs](v.Inputs)
	if err != nil {
		return nil, err
	}
	o, err := decode[model.Outputs](v.Outputs)
	if err != nil {
		return nil, err
	}
	m, err := decode[model.Metrics](v.Metrics)
	if err != nil {
		return nil, err
	}
	var retry *int
	if v.RetryCount.Valid {
		n := int(v.RetryCount.Int32)
		retry = &n
	}
	return &aether.TaskRun{RunID: v.RunID, WorkflowRunID: v.WorkflowRunID, ParentRunID: v.ParentRunID, Depth: int(v.Depth), Scope: v.Scope, TaskName: v.TaskName, TemplateName: v.TemplateName, TemplateType: v.TemplateType, CreatedAt: v.CreatedAt.UTC(), Inputs: i, Status: p, Message: readString(v.Message), Outputs: o, Metrics: m, RetryCount: retry, Deadline: readTime(v.Deadline), Token: v.Token, UpdatedAt: v.UpdatedAt.UTC()}, nil
}

func validWorkflow(r *aether.WorkflowRun, create bool) error {
	if r == nil || !validID(r.RunID, true, 191) || !validPhase(r.Status) {
		return invalid("workflow run")
	}
	if create {
		if !validID(r.CronWorkflowID, false, 191) || !isJSONObject(r.Workflow) {
			return invalid("workflow JSON or cron ID")
		}
	}
	return nil
}

func validTask(r *aether.TaskRun, create bool) error {
	if r == nil || !validID(r.RunID, true, 191) || !validPhase(r.Status) {
		return invalid("task run")
	}
	if create && (!validID(r.WorkflowRunID, true, 191) || !validID(r.ParentRunID, false, 191) || !validID(r.Scope, false, 191) || !validID(r.TaskName, true, 191) || !validID(r.TemplateName, true, 191) || !validID(r.TemplateType, true, 32) || r.Depth < 0 || uint64(r.Depth) > math.MaxUint32) {
		return invalid("task identity")
	}
	if r.RetryCount != nil && (*r.RetryCount < 0 || uint64(*r.RetryCount) > math.MaxInt32) {
		return invalid("retry count")
	}
	return nil
}

func retry(v *int) sql.NullInt32 {
	if v == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: int32(*v), Valid: true}
}
func missing(op, id string) error { return fmt.Errorf("%s %q: %w", op, id, aether.ErrNotFound) }
func failure(op, id string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return missing(op, id)
	}
	return fmt.Errorf("%s %q: %w", op, id, err)
}

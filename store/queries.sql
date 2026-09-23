-- name: CreateWorkflow :exec
INSERT INTO workflow_runs (run_id, workflow, cron_workflow_id, status, message, outputs, metrics, deadline)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetWorkflow :one
SELECT * FROM workflow_runs WHERE run_id = ?;

-- name: UpdateWorkflow :execrows
UPDATE workflow_runs SET status = COALESCE(?, status), message = COALESCE(?, message),
 outputs = COALESCE(?, outputs), metrics = COALESCE(?, metrics), deadline = COALESCE(?, deadline),
 token = token + 1, updated_at = CURRENT_TIMESTAMP(6)
WHERE run_id = ? AND token = CAST(? AS CHAR) AND token < 18446744073709551615;

-- name: ListActiveWorkflows :many
SELECT * FROM workflow_runs WHERE deadline IS NOT NULL AND (status IS NULL OR status NOT IN ('Succeeded','Failed','Error','Timeout','Skipped','Cancelled')) ORDER BY created_at, run_id;

-- name: ListCronWorkflowsRuns :many
SELECT * FROM workflow_runs WHERE cron_workflow_id = ? ORDER BY created_at, run_id;

-- name: CreateTask :exec
INSERT INTO task_runs (run_id, workflow_run_id, parent_run_id, depth, scope, task_name, template_name, template_type, inputs, status, message, outputs, metrics, retry_count, deadline)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetTask :one
SELECT * FROM task_runs WHERE run_id = ?;

-- name: GetWorkflowForUpdate :one
SELECT * FROM workflow_runs WHERE run_id = ? FOR UPDATE;

-- name: GetTaskForUpdate :one
SELECT * FROM task_runs WHERE run_id = ? FOR UPDATE;

-- name: GetTaskIdentity :one
SELECT * FROM task_runs WHERE workflow_run_id = ? AND parent_run_id = ? AND scope = ? AND task_name = ?;

-- name: UpdateTask :execrows
UPDATE task_runs SET inputs = COALESCE(?, inputs), status = COALESCE(?, status), message = COALESCE(?, message), outputs = COALESCE(?, outputs), metrics = COALESCE(?, metrics), retry_count = COALESCE(?, retry_count), deadline = COALESCE(?, deadline), token = token + 1, updated_at = CURRENT_TIMESTAMP(6)
WHERE run_id = ? AND token = CAST(? AS CHAR) AND token < 18446744073709551615;

-- name: ListTasks :many
SELECT * FROM task_runs WHERE workflow_run_id = ? ORDER BY created_at, run_id;

-- name: ListParentTasks :many
SELECT * FROM task_runs WHERE workflow_run_id = ? AND parent_run_id = ? ORDER BY created_at, run_id;

-- name: ListActiveTasks :many
SELECT * FROM task_runs WHERE deadline IS NOT NULL AND (status IS NULL OR status NOT IN ('Succeeded','Failed','Error','Timeout','Skipped','Cancelled')) ORDER BY created_at, run_id;

-- name: UpsertSchema :exec
INSERT INTO executor_schemas (executor_type, worker_id, schema_json) VALUES (?, ?, ?)
ON DUPLICATE KEY UPDATE schema_json = VALUES(schema_json);

-- name: ListSchemas :many
SELECT * FROM executor_schemas ORDER BY executor_type, worker_id;

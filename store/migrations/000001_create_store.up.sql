CREATE TABLE workflow_runs (
  run_id VARCHAR(191) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL PRIMARY KEY,
  workflow JSON NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  cron_workflow_id VARCHAR(191) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  status VARCHAR(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NULL,
  message TEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NULL,
  outputs JSON NULL,
  metrics JSON NULL,
  deadline DATETIME(6) NULL,
  token BIGINT UNSIGNED NOT NULL DEFAULT 0,
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  CONSTRAINT ck_workflow_json CHECK (JSON_TYPE(workflow) = 'OBJECT'),
  CONSTRAINT ck_workflow_outputs CHECK (outputs IS NULL OR JSON_TYPE(outputs) = 'OBJECT'),
  CONSTRAINT ck_workflow_metrics CHECK (metrics IS NULL OR JSON_TYPE(metrics) = 'OBJECT'),
  INDEX idx_workflow_runs_active (deadline, status, created_at, run_id),
  INDEX idx_workflow_runs_cron (cron_workflow_id, created_at, run_id)
) ENGINE=InnoDB;

CREATE TABLE task_runs (
  run_id VARCHAR(191) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL PRIMARY KEY,
  workflow_run_id VARCHAR(191) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  parent_run_id VARCHAR(191) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  depth INT UNSIGNED NOT NULL,
  scope VARCHAR(191) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  task_name VARCHAR(191) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  template_name VARCHAR(191) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  template_type VARCHAR(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  inputs JSON NULL,
  status VARCHAR(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NULL,
  message TEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NULL,
  outputs JSON NULL,
  metrics JSON NULL,
  retry_count INT UNSIGNED NULL,
  deadline DATETIME(6) NULL,
  token BIGINT UNSIGNED NOT NULL DEFAULT 0,
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  UNIQUE KEY uq_task_runs_identity (workflow_run_id, parent_run_id, scope, task_name),
  INDEX idx_task_runs_workflow (workflow_run_id, created_at, run_id),
  INDEX idx_task_runs_parent (workflow_run_id, parent_run_id, created_at, run_id),
  INDEX idx_task_runs_active (deadline, status, created_at, run_id),
  CONSTRAINT fk_task_runs_workflow FOREIGN KEY (workflow_run_id) REFERENCES workflow_runs(run_id) ON DELETE CASCADE,
  CONSTRAINT ck_task_inputs CHECK (inputs IS NULL OR JSON_TYPE(inputs) = 'OBJECT'),
  CONSTRAINT ck_task_outputs CHECK (outputs IS NULL OR JSON_TYPE(outputs) = 'OBJECT'),
  CONSTRAINT ck_task_metrics CHECK (metrics IS NULL OR JSON_TYPE(metrics) = 'OBJECT')
) ENGINE=InnoDB;

CREATE TABLE executor_schemas (
  executor_type VARCHAR(191) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  worker_id VARCHAR(191) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL DEFAULT '',
  schema_json JSON NOT NULL,
  PRIMARY KEY (executor_type, worker_id),
  CONSTRAINT ck_executor_type CHECK (CHAR_LENGTH(executor_type) > 0),
  CONSTRAINT ck_schema_json CHECK (JSON_TYPE(schema_json) = 'OBJECT')
) ENGINE=InnoDB;

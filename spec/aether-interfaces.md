# Aether Interface Implementation Plan

This document tracks the Aether interfaces Gaia may implement. Implement them incrementally; do not implement any of them as part of this planning document.

## Dependency

Gaia depends on the NiCkWKT fork at commit `efc94c1fa82ab03284a1b1542946251934910e1d` (the `dev` branch commit). The fork declares its module path as `github.com/BabySid/aether`, so imports use that path and `go.mod` replaces it with `github.com/NiCkWKT/aether`.

## Interfaces to consider

### Core requirements

1. **`store.Store`** — `github.com/BabySid/aether/store`
   - Composite interface embedding `WorkflowRunStore`, `TaskRunStore`, `SchemaStore`, and `CronWorkflowStore`, plus `Close() error`.
   - Persists workflow runs, task runs, and executor schemas. The composite interface also embeds `CronWorkflowStore`, but without a cron scheduler Gaia will initially implement its methods as explicit unsupported-operation errors rather than persist cron records.
   - Use MySQL 8.4 with `sqlc` and apply schema migrations as an explicit deployment step. Preserve the documented idempotency and partial-update semantics, use write tokens to protect concurrent updates, and support the queries needed for scheduling and state inspection. Active task queries include every non-terminal task with a deadline, including suspended tasks.

2. **`idgen.Generator`** — `github.com/BabySid/aether/idgen`
   - Generates unique IDs.
   - Small, independent implementation; suitable to implement early.

3. **`broker.TaskBroker`** — `github.com/BabySid/aether/broker`
   - Bridges the Engine and Workers: dispatch/cancel tasks, fetch assignments, and report task start/completion.
   - Required by the Engine. Choose a local or distributed design before implementing.

### Worker and execution integration

4. **`worker.Registry`** — `github.com/BabySid/aether/worker`
   - Registers workers, tracks heartbeats, and lists workers by supported executor type.
   - Its methods use `*wire.WorkerInfo` from `github.com/BabySid/aether/wire`.
   - Needed when the deployment requires worker discovery; optional for Engine configuration.

5. **`executor.Plugin`** — `github.com/BabySid/aether/executor`
   - Defines an executor type, its schema, and task execution behavior.
   - The Engine in the selected commit no longer registers executor plugins through an Engine option. Determine how Gaia's worker/runtime will host and register plugins before implementing this interface.

### Optional integrations

6. **`expr.Evaluator`** — `github.com/BabySid/aether/expr`: evaluates expressions against an environment.
7. **`secret.Provider`** — `github.com/BabySid/aether/secret`: retrieves secret values by name and key.
8. **`hook.Notifier`** — `github.com/BabySid/aether/hook`: sends hook events.
9. **`errsink.ErrorSink`** — `github.com/BabySid/aether/errsink`: observes internal errors for monitoring or alerting.
10. **`vars.Source`** — `github.com/BabySid/aether/vars`: contributes variables to evaluation contexts.
11. **`artifact.Repository`** — `github.com/BabySid/aether/artifact`: uploads and downloads artifacts. The selected commit marks the Engine integration as not yet wired into execution, so confirm the intended use before implementing.
12. **`cron.Scheduler`** — `github.com/BabySid/aether/cron`: out of scope; Gaia will not implement a cron scheduler. `store.Store` still embeds `CronWorkflowStore`; Gaia's initial implementation will return an explicit unsupported-operation error from its cron methods.
13. **`timeout.Watcher`** — `github.com/BabySid/aether/timeout`: emits timeout events for overdue work.

## Recommended implementation order

Gaia hosts a singleton Engine instance in production; Workers are separately deployed. The Store must nevertheless protect against concurrent writes (for example, competing callbacks). MySQL 8.4 is the backing database, accessed via `sqlc` rather than an ORM.

1. Implement `idgen.Generator` — small and straightforward to test; implementation is already underway separately.
2. Implement `store.Store` as one concrete type constructed from a `*sql.DB`; the Store owns and closes that database. Keep connection setup, configuration, and migrations outside the Store. Use MySQL 8.4 with `sqlc`, JSON columns for structured Aether values, and relational columns for identifiers, phases, timestamps, tokens, and query fields. MySQL assigns `CreatedAt` and `UpdatedAt` with microsecond precision, and write queries return the stored row where practical. Use case-sensitive comparison for opaque identifiers and filters. Cron methods, `DeleteWorkflowRun`, and `DeleteSchema` return a Gaia-defined `ErrNotImplemented`; no delete SQL or cron-record table is needed yet. Define public `ErrAlreadyExists` and `ErrInvalidArgument` sentinels; nil inputs, empty required IDs/fields, and empty executor schema types return errors wrapping `ErrInvalidArgument`, while empty worker IDs remain valid orphan-schema identifiers. Test durable persistence, concurrent token-guarded updates, partial updates, task-create idempotency, deterministic ordering, context cancellation, and documented active queries against real MySQL 8.4 with explicit migrations. A unique key on `(workflowRunID, parentRunID, scope, taskName)` must make a duplicate `CreateTaskRun` a no-op without replacing the first record, even when the duplicate has a different RunID. Reusing a RunID for a different composite key returns an error wrapping `ErrAlreadyExists`. New workflow and task runs start at Token 0. Each successful update, even with no mutable fields supplied, atomically advances Token and refreshes UpdatedAt from the database clock; equal adjacent timestamps are allowed. It returns the post-update record; a stale token wraps `store.ErrTokenMismatch` without changing the row, and a missing ID wraps `store.ErrNotFound`. Use a token-guarded single-statement update with a safe existence check on zero affected rows. Perform update and post-update read atomically. Nil mutable pointers leave existing values unchanged; there is no explicit clear operation. Reject JSON `null` for non-nil structured pointers while allowing zero-value structs as JSON objects. Pass caller contexts through to all database operations. List methods return non-nil empty slices and use deterministic ordering: creation time ascending, then ID ascending; schema records use executor type, then worker ID. `ListActiveWorkflowRuns` returns only non-terminal workflow runs with a deadline; a nil status is treated as non-terminal. `ListActiveTaskRuns` returns every non-terminal task with a deadline, including suspended tasks, regardless of whether its deadline has passed. Preserve `CronWorkflowID` on workflow runs and implement `ListWorkflowRunsByCronID`; only the `CronWorkflowStore` methods are unsupported. Executor schemas are keyed by `(executor_type, worker_id)`, with an empty worker ID representing an orphan record; upserts are last-committer-wins because SchemaStore has no token contract. Use case-sensitive string columns/collations for all opaque keys. Use a foreign key from task runs to workflow runs with `ON DELETE CASCADE` for database integrity, although the Store does not expose deletion behavior initially. Each Store method is atomic on its own; do not add cross-record transaction APIs outside Aether's interface. Apply MySQL migrations explicitly before starting Gaia.
3. Implement `broker.TaskBroker` for the separately deployed Workers.
4. Implement one `executor.Plugin` and wire it into a Worker — run a real task end to end. In the pinned Aether version, plugins are not registered through an Engine option.
5. Add an end-to-end test — submit a workflow, execute a task, and verify the stored result before expanding the integrations.
6. Implement `worker.Registry` when worker discovery is needed.
7. Add `expr.Evaluator`, then `timeout.Watcher`, when workflows need expressions and deadline handling.
8. Add `secret.Provider`, `hook.Notifier`, `errsink.ErrorSink`, `vars.Source`, and `artifact.Repository` as product needs dictate. Confirm Aether's artifact integration before relying on it.

Persisting in-flight records through a restart does not by itself resume execution. The pinned Engine's `Start()` does not replay in-flight workflow or task runs; automatic re-dispatch requires separate Engine/broker integration work. With no cron scheduler, Gaia will not persist or schedule cron workflow records initially. The cron methods fulfill the Go interface shape but not the functional cron-storage contract.

## Before implementing each interface

- Read the interface and related types at the pinned Aether commit; treat those definitions and comments as the contract.
- Add a compile-time interface assertion for each implementation.
- Add tests for behavior, error handling, concurrency, and lifecycle requirements documented by the interface.
- Keep implementations small and avoid adding abstractions until a concrete use case requires them.
- For relational persistence, use `sqlc`; do not use an ORM.

## Open decisions

- How should automatic recovery of in-flight workflows and tasks be implemented beyond durable Store persistence?
- Which optional integrations are required for the first usable deployment, excluding cron scheduling?
- How should executor plugins be instantiated and made available to Workers?

### Initial Store limitations

The first Store implementation will expose the complete Go `store.Store` shape but return Gaia's `ErrNotImplemented` from cron methods, `DeleteWorkflowRun`, and `DeleteSchema`. It will not create a cron-record table or implement delete SQL. This is an intentional scope limitation, not a claim that the pinned Aether deletion and cron persistence contracts are fully implemented. Gaia's Store package also defines `ErrAlreadyExists` and `ErrInvalidArgument` for stable caller classification.

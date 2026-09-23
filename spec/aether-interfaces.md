# Aether Interface Implementation Plan

This document tracks the Aether interfaces Gaia may implement. Implement them incrementally; do not implement any of them as part of this planning document.

## Dependency

Gaia depends on the NiCkWKT fork at commit `efc94c1fa82ab03284a1b1542946251934910e1d` (the `dev` branch commit). The fork declares its module path as `github.com/BabySid/aether`, so imports use that path and `go.mod` replaces it with `github.com/NiCkWKT/aether`.

## Interfaces to consider

### Core requirements

1. **`store.Store`** — `github.com/BabySid/aether/store`
   - Composite interface embedding `WorkflowRunStore`, `TaskRunStore`, `SchemaStore`, and `CronWorkflowStore`, plus `Close() error`.
   - Persists workflow runs, task runs, executor schemas, and cron workflow records.
   - This is the largest implementation. Preserve the documented idempotency and partial-update semantics, and support the queries needed for scheduling and recovery.

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
12. **`cron.Scheduler`** — `github.com/BabySid/aether/cron`: manages scheduled callbacks and lifecycle.
13. **`timeout.Watcher`** — `github.com/BabySid/aether/timeout`: emits timeout events for overdue work.

## Recommended implementation order

Choose the database and deployment model (single-process or distributed) first; these decisions shape `store.Store` and `broker.TaskBroker`.

1. Implement `idgen.Generator` — small and straightforward to test.
2. Implement `store.Store` — the largest piece; test persistence, concurrency, recovery, and the documented interface contracts. Use `sqlc` if the backing store is relational.
3. Implement `broker.TaskBroker` — start with a local broker unless Engine and Workers must run separately.
4. Implement one `executor.Plugin` and wire it into a Worker — run a real task end to end. In the pinned Aether version, plugins are not registered through an Engine option.
5. Add an end-to-end test — submit a workflow, execute a task, and verify the stored result before expanding the integrations.
6. Implement `worker.Registry` when worker discovery or distributed execution is needed.
7. Add `expr.Evaluator`, then `timeout.Watcher`, when workflows need expressions and deadline handling.
8. Add `cron.Scheduler`, then `secret.Provider`, `hook.Notifier`, `errsink.ErrorSink`, `vars.Source`, and `artifact.Repository` as product needs dictate. Confirm Aether's artifact integration before relying on it.

## Before implementing each interface

- Read the interface and related types at the pinned Aether commit; treat those definitions and comments as the contract.
- Add a compile-time interface assertion for each implementation.
- Add tests for behavior, error handling, concurrency, and lifecycle requirements documented by the interface.
- Keep implementations small and avoid adding abstractions until a concrete use case requires them.
- For relational persistence, use `sqlc`; do not use an ORM.

## Open decisions

- Which relational database should back `store.Store` (if any)?
- Is Gaia single-process, or will Engine and Workers run separately?
- Which optional integrations are required for the first usable deployment?
- How should executor plugins be instantiated and made available to Workers?

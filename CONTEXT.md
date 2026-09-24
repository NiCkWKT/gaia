# Gaia Context

Gaia is the workflow orchestration service. It exposes HTTP endpoints to submit and control workflows and hosts the Aether Engine; task execution is performed by separate Worker services.

## Runtime roles

**Engine**:
The Gaia-hosted workflow orchestration runtime that coordinates workflow runs and task assignments.
_Avoid_: Worker, Executor

**Worker**:
A separately deployed service that receives task assignments from the Engine and executes them.
_Avoid_: Engine, Executor

**Executor**:
A Worker-side implementation that performs a particular kind of task.
_Avoid_: Worker, Engine

**Task assignment**:
The instruction for a Worker to execute a task, including the information it needs to perform that execution.
_Avoid_: Task run

**Lifecycle notification**:
A message triggered by an explicitly configured workflow or task lifecycle hook.
_Avoid_: Internal error alert

**Internal error alert**:
A best-effort message about a serious Engine-internal error, independent of configured lifecycle hooks.
_Avoid_: Lifecycle notification

# Gaia HTTP JSON contract (initial implementation)

Status: **approved initial contract**. Covers the first submit → poll → start → complete → inspect happy path, based on Aether fork commit `a23faa85122a3a197e1e64d3769dcebdd22c3ff9`. Gaia imports `github.com/BabySid/aether` via the NiCkWKT fork replacement. The Aether data shapes below are from that commit; the envelope, error numbers, and HTTP validation rules are Gaia's initial contract. Gaia constructs one Engine and serves these endpoints.

## Universal response envelope

Every request handled by Gaia returns one UTF-8 JSON object with `Content-Type: application/json`:

```json
{"errNo":0,"errMsg":"succ","data":{}}
```

- `errNo` is an integer and is **authoritative**: `0` means the operation succeeded; **any nonzero value means failure**. Clients must not infer application success or failure from the HTTP status code.
- `errMsg` is exactly `"succ"` when `errNo` is `0`. On failure it is a short, safe, human-readable message, not a machine identifier. Clients branch on `errNo`, not message text.
- `data` is **always present and is a JSON object**, even for errors, empty fetches, start and complete. For those cases it is `{}`. Endpoint-specific fields live inside `data`; no direct Aether object at the response root.
- Gaia returns HTTP `200 OK` for all **handled** application outcomes, including validation failure, unknown workflow, empty queue, and dependency errors. It never intentionally returns `201` or `204` from these endpoints. A proxy, network failure, timeout, or failure before a Gaia handler can respond may produce no envelope or another HTTP code. Clients must first verify they received a valid envelope, then inspect `errNo`. A missing/invalid envelope means *unknown outcome*, not success; retrying a submission may create another workflow.

Initial application error numbers (stable for this first contract; numbers are not Aether error codes):

| `errNo` | Meaning | Example `errMsg` | `data` |
| --- | --- | --- | --- |
| `0` | Success, including no task available | `succ` | Endpoint object or `{}` |
| `1001` | Invalid request, including malformed JSON, missing fields, wrong JSON media type, or size limit | `invalid request` | `{}` |
| `1002` | Workflow validation rejected by Aether | `invalid workflow` | `{}` |
| `1003` | Workflow run not found | `workflow not found` | `{}` |
| `2001` | Store/Redis unavailable, if distinguishable | `dependency unavailable` | `{}` |
| `2002` | Unexpected server error or unclassified dependency failure | `internal error` | `{}` |

For example: `{"errNo":1003,"errMsg":"workflow not found","data":{}}`. Do not expose credentials, raw database errors, stack traces, or untrusted input in `errMsg`; log details server-side.

## Request conventions

- JSON request bodies are UTF-8 objects with camelCase keys. POST requests require `Content-Type: application/json` (a charset parameter may be present). GET has no request body. Responses always use the envelope above.
- One JSON object per POST; reject missing, `null`, arrays, malformed JSON, trailing values, unknown fields, and bodies over 1 MiB as `errNo: 1001`. Missing or empty required fields also yield `1001`.
- The five URLs are unversioned for this initial contract. Public workflow data uses Aether's `aether/v1` document; Worker assignments/results use Aether's `aether/worker/v1` wire types (that version does not appear as a JSON field).
- Worker ID is a queue selector, **not** a credential. SRE must restrict listener reachability to trusted services; no Gaia-side auth or TLS is provided in this milestone. Never expose the Worker routes to untrusted callers.
- There is no idempotency key, fetch receipt, or delivery acknowledgment. Network failures can leave the outcome unknown; `errNo: 0` does not imply exactly-once processing.

## Endpoint summary

| Caller | Request | Success `data` | Called method |
| --- | --- | --- | --- |
| Client | `POST /workflow` | `{"workflowRunID":"..."}` | `Engine.Submit(ctx, *model.Workflow)` |
| Client | `GET /workflow?workflow_run_id=...` | Serialized `aether.WorkflowExecution` object | `Engine.Get(ctx, id)` |
| Worker | `POST /task/fetch` | Serialized `wire.TaskAssignment`, or `{}` if queue empty | `Broker.FetchTask(ctx, workerID)` |
| Worker | `POST /task/start` | `{}` | `Broker.StartTask(ctx, taskRunID, workerID)` |
| Worker | `POST /task/complete` | `{}` | `Broker.CompleteTask(ctx, *wire.TaskResult)` |

Use `/task/complete`, not the earlier `/start/complete` typo. For fetch, `errNo: 0` with `data: {}` means **no task available**; `errNo: 0` with `data.taskRunID` means an assignment was returned. There is no separate `no task` error number.

## `POST /workflow`

**Request:** one `model.Workflow` JSON object directly, not an envelope. Required for the HTTP shape: `apiVersion: "aether/v1"`, `kind: "Workflow"`, `metadata`, and `spec`; Aether validates their contents, including name, entrypoint, and templates. `CronWorkflow` is outside this endpoint. `spec.templates` contains Aether template unions (exactly one of `task`, `dag`, or `loop` per template); `spec.arguments`, `spec.hooks`, timeouts, and other fields follow Aether's `model/` types and `specs/graph-schema.json` at the pinned commit.

Illustrative submission (requires an `echo` executor on a future Worker):

```json
{
  "apiVersion": "aether/v1",
  "kind": "Workflow",
  "metadata": {"name": "greeting"},
  "spec": {
    "entrypoint": "greet",
    "templates": [{"task": {"name": "greet", "executor": {"type": "echo"}}}]
  }
}
```

**Success:** `{"errNo":0,"errMsg":"succ","data":{"workflowRunID":"wf-1"}}`. `Engine.Submit` generates the ID. Success means the call returned successfully, **not** that a task executed. Invalid request JSON → `1001`; Aether validation → `1002`; infrastructure failure → `2001` or `2002`.

Aether validates before persistence, but failures after persistence begins can leave partial workflow/task records: an error envelope does not promise rollback. An ambiguous response can be followed by a duplicate submission if the client retries; no deduplication is promised.

## `GET /workflow?workflow_run_id=...`

**Request:** exactly one non-empty `workflow_run_id` query value; no body. Missing, empty, or repeated value → `1001`. Unknown workflow run → `1003`. Returns current state, not a wait for completion.

**Success:** `data` is `Engine.Get`'s `WorkflowExecution` serialized with the JSON tags from the pinned Aether `types.go`. Example snapshot (IDs and timestamps illustrative):

```json
{
  "errNo": 0,
  "errMsg": "succ",
  "data": {
    "runID": "wf-1",
    "status": "Running",
    "message": "",
    "outputs": null,
    "metrics": null,
    "createdAt": "2026-09-28T03:05:00Z",
    "progress": "0/1",
    "tasks": [{
      "runID": "task-1",
      "workflowRunID": "wf-1",
      "parentRunID": "",
      "depth": 0,
      "scope": "",
      "taskName": "greet",
      "templateName": "greet",
      "templateType": "task",
      "createdAt": "2026-09-28T03:05:00Z",
      "status": "Running",
      "message": "",
      "inputs": null,
      "outputs": null,
      "metrics": null,
      "retryCount": 0
    }]
  }
}
```

Aether's `WorkflowExecution`/`TaskExecution` fields do not have `omitempty` tags: zero strings/numbers and nil pointers appear as `""`, `0`, and `null`; `tasks` may be `null` if the slice is nil or `[]` if non-nil and empty. `createdAt` is an encoded Go `time.Time` (RFC3339 with fractional seconds when present). `status` is an Aether `model.Phase` such as `Created`, `Ready`, `Running`, `Suspended`, `Succeeded`, `Failed`, `Error`, `Timeout`, `Skipped`, or `Cancelled`; it may be `""` if unset. No Store tokens, deadlines, or raw workflow definition are returned. The ID inside this response is `data.runID`, **not** `data.workflowRunID`.

## `POST /task/fetch`

**Request:** `{"workerID":"v1::worker-1::echo"}`. The broker requires `v1::<identifier>::<executor_type>` with non-empty identifier and executor type and no embedded `::`. Executor type selects the Redis queue. Invalid request → `1001`.

**Assignment available:** `data` is the Aether `wire.TaskAssignment` object (not nested in another assignment field):

```json
{
  "errNo": 0,
  "errMsg": "succ",
  "data": {
    "taskRunID": "task-1",
    "workflowRunID": "wf-1",
    "taskName": "greet",
    "templateName": "greet",
    "executorType": "echo",
    "inputs": {"parameters": [{"name": "text", "type": "string", "value": "hello"}]},
    "timeout": "30m",
    "resources": {"memory": "512Mi"}
  }
}
```

An assignment includes `taskRunID`, `workflowRunID`, `taskName`, `templateName`, and `executorType`; optional `inputs` (parameters/artifacts), `timeout`, `resources`, `priority`, and `retryCount` follow Aether's `omitempty` JSON tags. `value` may be any JSON value, not necessarily a string. Workers do not submit assignment objects.

**Empty queue:** `{"errNo":0,"errMsg":"succ","data":{}}` immediately. Workers distinguish this from an assignment by the absence of `data.taskRunID` and choose their own polling backoff. Redis errors are `2001`/`2002`, **not** empty-queue success. Fetch removes an assignment before the HTTP response reaches the Worker; crashes or lost responses can lose it. No lease, acknowledgment, or redelivery guarantee exists.

## `POST /task/start`

**Request:** `{"workerID":"v1::worker-1::echo","taskRunID":"task-1"}`; both fields required and non-empty. The HTTP layer validates shape; the broker currently ignores `workerID` on start and does not verify ownership. Workers send start before executing an assignment.

**Success:** `{"errNo":0,"errMsg":"succ","data":{}}`. This means `Broker.StartTask` invoked `Engine.OnTaskStarted`. The Engine callback returns no error, so this does **not** verify persistence or a state transition. Unknown or duplicate starts may still receive success. Invalid JSON/fields → `1001`; a broker error → `2002` (or `2001` if a dependency error is identifiable).

## `POST /task/complete`

**Request:** direct `wire.TaskResult` JSON object, with non-empty `taskRunID` and `workflowRunID`. Aether's result has **no workerID**. Embedded `model.ExecOutputs` fields are flat: optional `code` (integer), `message` (string), `parameters` (array of `model.Parameter`), and `artifacts` (array of `model.Artifact`), not an `execOutputs` wrapper. Absent `code` means success (`0`); code mapping: `0` succeeded, `1` suspended, `2` business failure, `3` system error, `4` timeout. Workers do not set `phase` or `metrics`.

Example successful completion with an output:

```json
{"taskRunID":"task-1","workflowRunID":"wf-1","parameters":[{"name":"text","type":"string","value":"hello"}]}
```

Example business failure: `{"taskRunID":"task-1","workflowRunID":"wf-1","code":2,"message":"rejected"}`. Only the two IDs are needed for a successful completion with no outputs. A suspended result requires a separate `Engine.Resume` operation, **not** provided in this five-endpoint milestone.

**Success:** `{"errNo":0,"errMsg":"succ","data":{}}`. Workers must call `/task/start` before completion: Aether only processes completion for a Running or Suspended task. `Broker.CompleteTask` invokes the void `Engine.OnTaskCompleted` callback, so success does **not** guarantee persistence, verify ownership, or prove the result was accepted. Duplicate, stale, or unknown task callbacks may be ignored. Internal callback errors, when reported, go to the ErrorSink rather than the HTTP response.

## Lifecycle and limits

If the server cannot start, it cannot produce the standard envelope; callers treat unavailable/unparseable responses as unknown outcomes. Stop accepting HTTP requests before closing Engine, broker, Store, and notification adapter. Apply database migrations explicitly as a deployment step, not through HTTP.

Outside this first contract: Worker registration/heartbeat, leases/acknowledgments, redelivery, workflow cancel/resume, cron, artifact upload, bulk listing, and timeout watcher endpoints. Gaia's broker `Cancel` remains unsupported, fetch is at-most-once, and Aether's `Start` does not replay in-flight tasks. These endpoints describe the chosen happy path, not recovery guarantees. Authentication and TLS are handled by SRE restricting listener reachability to trusted services. Body limit, unknown-field policy, and error mapping follow the initial contract above.

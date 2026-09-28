# Live-server test report — Gaia HTTP endpoints

**Date:** 2026-09-28 (UTC); live HTTP transcript recaptured at 05:46 UTC
**Revision:** `838de87` (`main`, two commits ahead of `origin/main` at test time)
**Result:** PASS for the checks below; no live-server failures observed.

## Environment and method

Built the current source with `go build -o <temporary binary> .` and launched that binary as a separate process bound to `127.0.0.1` on an ephemeral port. Provisioned disposable MySQL 8.4 and Redis 7.2.16 Docker containers on loopback-only ephemeral ports, created a dedicated `gaia_smoke` database, and applied the existing Store migration **before** starting Gaia. Configured Gaia with a dedicated Redis prefix and dummy Telegram settings; no real Telegram credentials or messages were used. All test containers and the server process were stopped after the run. No persistent test database or service was modified.

Requests were sent over real TCP HTTP (not `httptest`) using Python's standard HTTP client. Assertions covered HTTP status, JSON content type, envelope shape, application error number, returned IDs, and observable workflow/task status. The smoke procedure was run as a temporary local script, not added to the repository. After the initial run, the smoke test was repeated with request and response body capture; the transcript below belongs to that second run. All 23 assertions passed again, across 10 captured HTTP exchanges.

## Observations

| Check | Result | Evidence |
| --- | --- | --- |
| Missing Telegram chat ID blocks startup | PASS | Process exited 1, diagnostic named the missing variable; listener remained closed. |
| Hello route on running server | PASS | `GET /` returned HTTP 200 and `Hello, World!`. |
| Invalid workflow request | PASS | `POST /workflow` returned HTTP 200, `errNo: 1001`, `data: {}`. |
| Unknown workflow run | PASS | `GET /workflow?workflow_run_id=missing` returned HTTP 200, `errNo: 1003`. |
| Empty queue | PASS | Initial `POST /task/fetch` returned HTTP 200, `errNo: 0`, `data: {}`. |
| Submit workflow | PASS | `POST /workflow` returned `errNo: 0` and a nonempty `data.workflowRunID`. |
| Fetch task assignment | PASS | `POST /task/fetch` returned a task-run ID and matching workflow-run ID. A second fetch returned an empty success envelope. |
| Start and complete | PASS | `POST /task/start` and `POST /task/complete` each returned HTTP 200, `errNo: 0`, `data: {}`. Completion carried an output parameter. |
| Inspect completed run | PASS | `GET /workflow?workflow_run_id=<returned ID>` reported the same run ID and `Succeeded` status for both workflow and task. |
| Graceful termination | PASS | Sending SIGTERM exited with code 0; the listener then refused connections. |
| Response contract on all exercised API calls | PASS | Each returned HTTP 200, `Content-Type: application/json`, and an object-valued `data` field. |

Separately, `go test ./...` passed and `golangci-lint run` reported **0 issues** at this revision. The Go suite includes router-level integration coverage for successful, business-failure, and suspended task results against isolated dependencies when Docker is available.

## Captured HTTP request/response bodies

The following is the **second live run’s captured traffic**, in request order. Request and response JSON are pretty-printed from the bodies captured in the second live run; key order and values are preserved, but whitespace and the response’s final newline differ from the wire bytes. IDs and timestamps are real values from this disposable run. `GET` requests had no body. The hello route is included because it was tested. Startup failure and SIGTERM have no HTTP request/response body.

### 1. `GET /`
Request body:
```text
(none)
```
Response: HTTP 200; `Content-Type: text/plain; charset=utf-8`
```text
Hello, World!
```
### 2. `POST /workflow`
Request body:
```json
{
  "kind": "Workflow"
}
```
Response: HTTP 200; `Content-Type: application/json`
```json
{
  "errNo": 1001,
  "errMsg": "invalid request",
  "data": {}
}
```
### 3. `GET /workflow?workflow_run_id=missing`
Request body:
```text
(none)
```
Response: HTTP 200; `Content-Type: application/json`
```json
{
  "errNo": 1003,
  "errMsg": "workflow not found",
  "data": {}
}
```
### 4. `POST /task/fetch`
Request body:
```json
{
  "workerID": "v1::live-smoke::echo"
}
```
Response: HTTP 200; `Content-Type: application/json`
```json
{
  "errNo": 0,
  "errMsg": "succ",
  "data": {}
}
```
### 5. `POST /workflow`
Request body:
```json
{
  "apiVersion": "aether/v1",
  "kind": "Workflow",
  "metadata": {
    "name": "live-smoke"
  },
  "spec": {
    "entrypoint": "greet",
    "templates": [
      {
        "task": {
          "name": "greet",
          "executor": {
            "type": "echo"
          }
        }
      }
    ]
  }
}
```
Response: HTTP 200; `Content-Type: application/json`
```json
{
  "errNo": 0,
  "errMsg": "succ",
  "data": {
    "workflowRunID": "3f49bebd-060f-46e4-8cdc-c697254d9df8"
  }
}
```
### 6. `POST /task/fetch`
Request body:
```json
{
  "workerID": "v1::live-smoke::echo"
}
```
Response: HTTP 200; `Content-Type: application/json`
```json
{
  "errNo": 0,
  "errMsg": "succ",
  "data": {
    "taskRunID": "07800840-885e-49ba-8490-8d3b9094062b",
    "workflowRunID": "3f49bebd-060f-46e4-8cdc-c697254d9df8",
    "taskName": "greet",
    "templateName": "greet",
    "executorType": "echo",
    "priority": 500
  }
}
```
### 7. `POST /task/fetch`
Request body:
```json
{
  "workerID": "v1::live-smoke::echo"
}
```
Response: HTTP 200; `Content-Type: application/json`
```json
{
  "errNo": 0,
  "errMsg": "succ",
  "data": {}
}
```
### 8. `POST /task/start`
Request body:
```json
{
  "workerID": "v1::live-smoke::echo",
  "taskRunID": "07800840-885e-49ba-8490-8d3b9094062b"
}
```
Response: HTTP 200; `Content-Type: application/json`
```json
{
  "errNo": 0,
  "errMsg": "succ",
  "data": {}
}
```
### 9. `POST /task/complete`
Request body:
```json
{
  "taskRunID": "07800840-885e-49ba-8490-8d3b9094062b",
  "workflowRunID": "3f49bebd-060f-46e4-8cdc-c697254d9df8",
  "parameters": [
    {
      "name": "text",
      "type": "string",
      "value": "hello"
    }
  ]
}
```
Response: HTTP 200; `Content-Type: application/json`
```json
{
  "errNo": 0,
  "errMsg": "succ",
  "data": {}
}
```
### 10. `GET /workflow?workflow_run_id=3f49bebd-060f-46e4-8cdc-c697254d9df8`
Request body:
```text
(none)
```
Response: HTTP 200; `Content-Type: application/json`
```json
{
  "errNo": 0,
  "errMsg": "succ",
  "data": {
    "runID": "3f49bebd-060f-46e4-8cdc-c697254d9df8",
    "status": "Succeeded",
    "message": "",
    "outputs": {
      "phase": "Succeeded",
      "metrics": {
        "startedAt": "2026-09-28T05:46:33Z",
        "finishedAt": "2026-09-28T05:46:33Z",
        "duration": "861ms"
      },
      "parameters": [
        {
          "name": "text",
          "type": "string",
          "value": "hello"
        }
      ]
    },
    "metrics": {
      "startedAt": "2026-09-28T05:46:33Z",
      "finishedAt": "2026-09-28T05:46:33Z",
      "duration": "867ms"
    },
    "createdAt": "2026-09-28T05:46:33.800049Z",
    "progress": "1/1",
    "tasks": [
      {
        "runID": "07800840-885e-49ba-8490-8d3b9094062b",
        "workflowRunID": "3f49bebd-060f-46e4-8cdc-c697254d9df8",
        "parentRunID": "",
        "depth": 0,
        "scope": "",
        "taskName": "greet",
        "templateName": "greet",
        "templateType": "task",
        "createdAt": "2026-09-28T05:46:33.814509Z",
        "status": "Succeeded",
        "message": "",
        "inputs": null,
        "outputs": {
          "phase": "Succeeded",
          "metrics": {
            "startedAt": "2026-09-28T05:46:33Z",
            "finishedAt": "2026-09-28T05:46:33Z",
            "duration": "861ms"
          },
          "parameters": [
            {
              "name": "text",
              "type": "string",
              "value": "hello"
            }
          ]
        },
        "metrics": {
          "startedAt": "2026-09-28T05:46:33Z",
          "finishedAt": "2026-09-28T05:46:33Z",
          "duration": "861ms"
        },
        "retryCount": 0
      }
    ]
  }
}
```

## Limits and follow-up

- This run simulated Worker HTTP calls; it did **not** run an independent Worker or Executor service. The success envelope for start/completion alone is not proof of durable acceptance; the final inspection independently confirmed the successful path.
- Dummy Telegram configuration exercised construction, not real notification delivery or alerting. Validate delivery with real credentials in an authorized environment.
- This smoke run did not test Redis/MySQL outages during traffic, incomplete migration, concurrency, restart recovery, transport security, an untrusted listener, or sustained load. The initial broker remains at-most-once with no redelivery guarantee.
- The server was tested on a loopback listener only; SRE must restrict production reachability to trusted callers as documented in the HTTP contract.

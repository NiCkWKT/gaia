# Live test report: separate Gaia and multi-executor Worker

**Date:** 2026-09-28. The recorded workflow ran at 17:06 local time (UTC+08:00); API timestamps are UTC.

**Result:** PASS — workflow `fc76c752-5a32-4f9f-b383-bc8ba6fd9fbc` and all three task records reached `Succeeded`, progress `3/3`, and the workflow-level `outputs` are now populated.

**Source revision:** `cd7132c`, with uncommitted changes to `testdata/worker/workflow.json` (DAG-level `outputs`) and this report.

## Setup and method

Gaia and the Worker ran as **separate processes**, started with `make start-gaia` and `make start-worker` in two terminals. Gaia listened on `127.0.0.1:8080` and used the local MySQL 8.4 and Redis 7.2.16 Compose services; Telegram credentials came from the environment and the workflow declared no hooks. The Worker connected to `http://localhost:8080` as `mock-worker`, with concurrency `1` per executor (`prompt` and `image`) and a one-second poll interval. There was no Worker registration or heartbeat.

The workflow was submitted with `curl` over loopback. All application responses were HTTP 200 with `Content-Type: application/json` and `errNo: 0`.

**Provenance of the values below:** the running Gaia writes its access log to the operator's terminal rather than a file, so this run did not capture the Worker-side `/task/fetch`, `/task/start`, and `/task/complete` request bodies. Each task's **input and output** below are taken from `GET /workflow` and cross-checked against the durable `task_runs` rows in MySQL, so they are the values the Engine actually stored after the Worker reported them. The submit exchange was captured directly.

**IDs observed:**

| Record | Run ID |
| --- | --- |
| Workflow | `fc76c752-5a32-4f9f-b383-bc8ba6fd9fbc` |
| `pipeline` DAG | `15a14764-ab28-4bd8-9353-ff90f5ea8993` |
| `describe` (`prompt`) | `9cd14732-b47b-4f29-be87-5bfc3185e2ed` |
| `render` (`image`) | `a3584b02-148d-4462-890c-d86bb657113e` |

## Step 1: submit the workflow

**Input:** `POST /workflow`, `Content-Type: application/json`. This is the complete submitted body, including the DAG `outputs` block added for this run:

```json
{
  "apiVersion": "aether/v1",
  "kind": "Workflow",
  "metadata": {"name": "mock-prompt-to-image"},
  "spec": {
    "entrypoint": "pipeline",
    "templates": [
      {
        "dag": {
          "name": "pipeline",
          "outputs": {
            "parameters": [
              {"name": "text", "type": "string", "valueFrom": {"parameter": "tasks.describe.outputs.parameters.text"}},
              {"name": "uri", "type": "string", "valueFrom": {"parameter": "tasks.render.outputs.parameters.uri"}}
            ]
          },
          "tasks": [
            {"name": "describe", "template": "describe-prompt"},
            {
              "name": "render", "template": "render-image", "dependencies": ["describe"],
              "arguments": {"parameters": [
                {"name": "prompt", "valueFrom": {"parameter": "tasks.describe.outputs.parameters.text"}}
              ]}
            }
          ]
        }
      },
      {
        "task": {
          "name": "describe-prompt",
          "executor": {"type": "prompt"},
          "inputs": {"parameters": [
            {"name": "prompt", "type": "string", "value": "a cat reading a book"},
            {"name": "images", "type": "array", "value": []}
          ]},
          "outputs": {"parameters": [{"name": "text", "type": "string"}]}
        }
      },
      {
        "task": {
          "name": "render-image",
          "executor": {"type": "image"},
          "inputs": {"parameters": [
            {"name": "prompt", "type": "string"},
            {"name": "images", "type": "array", "value": []}
          ]},
          "outputs": {"parameters": [{"name": "uri", "type": "string"}]}
        }
      }
    ]
  }
}
```

**Output:** HTTP 200:

```json
{"errNo":0,"errMsg":"succ","data":{"workflowRunID":"fc76c752-5a32-4f9f-b383-bc8ba6fd9fbc"}}
```

This response acknowledges submission, not completion.

## Step 2: observe execution

Polling `GET /workflow?workflow_run_id=fc76c752-5a32-4f9f-b383-bc8ba6fd9fbc` once per second captured the normal progression:

| Poll | Workflow status | Progress | Task statuses observed |
| --- | --- | --- | --- |
| 1 | `Ready` | `0/2` | `pipeline` Ready, `describe` Ready |
| 2 | `Running` | `1/3` | `pipeline` Running, `describe` Succeeded, `render` Ready |
| 3 | `Succeeded` | `3/3` | `pipeline`, `describe`, `render` all Succeeded |

The `render` task record appears only at poll 2: it is created once `describe` succeeds, because of the `dependencies: ["describe"]` edge.

## Step 3: per-step executor input and output

Both executors received `prompt` and `images` and produced exactly one output parameter. The stored records:

| Task | Executor | Input | Output |
| --- | --- | --- | --- |
| `describe` | `prompt` | `prompt="a cat reading a book"`, `images=[]` | `text="mock response: a cat reading a book"` |
| `render` | `image` | `prompt="mock response: a cat reading a book"`, `images=[]` | `uri="file:///var/folders/9c/sdtmtw0d2ws541g1wgbwm9pm0000gn/T/gaia-mock-worker-images/0bdf42e4f54e24be3d343805bf41a687.jpeg"` |

`render`'s input prompt equals `describe`'s output text, confirming the DAG argument binding (`tasks.describe.outputs.parameters.text` → `prompt`). Durations were 642 ms for `describe` and 620 ms for `render`.

## Step 4: inspect workflow and verify outputs

**Input:** `GET /workflow?workflow_run_id=fc76c752-5a32-4f9f-b383-bc8ba6fd9fbc` (no body). **Output:** HTTP 200. The observed `data` object, with whitespace changed for readability:

```json
{
  "runID": "fc76c752-5a32-4f9f-b383-bc8ba6fd9fbc",
  "status": "Succeeded",
  "message": "",
  "outputs": {
    "phase": "Succeeded",
    "parameters": [
      {"name": "text", "type": "string", "value": "mock response: a cat reading a book"},
      {"name": "uri", "type": "string", "value": "file:///var/folders/9c/sdtmtw0d2ws541g1wgbwm9pm0000gn/T/gaia-mock-worker-images/0bdf42e4f54e24be3d343805bf41a687.jpeg"}
    ]
  },
  "metrics": {"startedAt": "2026-09-28T09:06:39Z", "finishedAt": "2026-09-28T09:06:40Z", "duration": "1.636s"},
  "createdAt": "2026-09-28T09:06:38.705558Z",
  "progress": "3/3",
  "tasks": [
    {
      "runID": "15a14764-ab28-4bd8-9353-ff90f5ea8993",
      "workflowRunID": "fc76c752-5a32-4f9f-b383-bc8ba6fd9fbc",
      "parentRunID": "", "depth": 0, "scope": "", "taskName": "pipeline", "templateName": "pipeline", "templateType": "dag",
      "createdAt": "2026-09-28T09:06:38.731253Z", "status": "Succeeded", "message": "", "inputs": null,
      "outputs": {"phase": "Succeeded", "parameters": [
        {"name": "text", "type": "string", "value": "mock response: a cat reading a book"},
        {"name": "uri", "type": "string", "value": "file:///var/folders/9c/sdtmtw0d2ws541g1wgbwm9pm0000gn/T/gaia-mock-worker-images/0bdf42e4f54e24be3d343805bf41a687.jpeg"}
      ]},
      "metrics": {"startedAt": "2026-09-28T09:06:39Z", "finishedAt": "2026-09-28T09:06:40Z", "duration": "1.63s"}, "retryCount": 0
    },
    {
      "runID": "9cd14732-b47b-4f29-be87-5bfc3185e2ed",
      "workflowRunID": "fc76c752-5a32-4f9f-b383-bc8ba6fd9fbc",
      "parentRunID": "15a14764-ab28-4bd8-9353-ff90f5ea8993", "depth": 1, "scope": "pipeline/", "taskName": "describe", "templateName": "describe-prompt", "templateType": "task",
      "createdAt": "2026-09-28T09:06:38.740214Z", "status": "Succeeded", "message": "",
      "inputs": {"parameters": [
        {"name": "prompt", "type": "string", "value": "a cat reading a book"},
        {"name": "images", "type": "array", "value": []}
      ]},
      "outputs": {"phase": "Succeeded", "metrics": {"startedAt": "2026-09-28T09:06:39Z", "finishedAt": "2026-09-28T09:06:39Z", "duration": "642ms"}, "parameters": [
        {"name": "text", "value": "mock response: a cat reading a book"}
      ]},
      "metrics": {"startedAt": "2026-09-28T09:06:39Z", "finishedAt": "2026-09-28T09:06:39Z", "duration": "642ms"}, "retryCount": 0
    },
    {
      "runID": "a3584b02-148d-4462-890c-d86bb657113e",
      "workflowRunID": "fc76c752-5a32-4f9f-b383-bc8ba6fd9fbc",
      "parentRunID": "15a14764-ab28-4bd8-9353-ff90f5ea8993", "depth": 1, "scope": "pipeline/", "taskName": "render", "templateName": "render-image", "templateType": "task",
      "createdAt": "2026-09-28T09:06:39.651977Z", "status": "Succeeded", "message": "",
      "inputs": {"parameters": [
        {"name": "prompt", "type": "string", "value": "mock response: a cat reading a book"},
        {"name": "images", "type": "array", "value": []}
      ]},
      "outputs": {"phase": "Succeeded", "metrics": {"startedAt": "2026-09-28T09:06:40Z", "finishedAt": "2026-09-28T09:06:40Z", "duration": "620ms"}, "parameters": [
        {"name": "uri", "value": "file:///var/folders/9c/sdtmtw0d2ws541g1wgbwm9pm0000gn/T/gaia-mock-worker-images/0bdf42e4f54e24be3d343805bf41a687.jpeg"}
      ]},
      "metrics": {"startedAt": "2026-09-28T09:06:40Z", "finishedAt": "2026-09-28T09:06:40Z", "duration": "620ms"}, "retryCount": 0
    }
  ]
}
```

The workflow-level `outputs` and the `pipeline` DAG outputs both carry the resolved `text` and `uri` values from the two leaf tasks. Durable confirmation from MySQL:

```sql
SELECT outputs FROM workflow_runs WHERE run_id='fc76c752-5a32-4f9f-b383-bc8ba6fd9fbc';
-- {"phase": "Succeeded", "parameters": [{"name": "text", ...}, {"name": "uri", ...}]}
```

## Step 5: verify the image file

The returned URI resolved to an existing **749 × 749 JPEG** on the Worker host:

```
file:///var/folders/9c/sdtmtw0d2ws541g1wgbwm9pm0000gn/T/gaia-mock-worker-images/0bdf42e4f54e24be3d343805bf41a687.jpeg
```

A byte comparison with [`../testdata/worker/testdata/gaia_worker_img1.jpeg`](../testdata/worker/testdata/gaia_worker_img1.jpeg) passed; both had SHA-256 `b957f3d0abfbcbe2ff670371d600e33962af56ad52ec1d760d1d6df5486a3e83`.

## Checks and limitations

| Check | Result | Evidence |
| --- | --- | --- |
| Gaia and Worker run separately | PASS | Started with `make start-gaia` and `make start-worker` in separate terminals; Gaia served `127.0.0.1:8080`. |
| One Worker hosts both Executor types | PASS | The same Worker process executed both `prompt` and `image` tasks, using type-specific Worker IDs. |
| Dependency and parameter handoff | PASS | `render`'s stored input prompt equals `describe`'s stored output text. |
| Persistence and completion | PASS | Inspection and MySQL both returned `Succeeded` for both leaves, the DAG, and the workflow, progress `3/3`. |
| Workflow-level outputs | PASS | `data.outputs.parameters` carries `text` and `uri`, matching the DAG `outputs` block. |
| Real local image file | PASS | `file://` path existed and matched the fixture bytes and SHA-256. |

**Not tested:** restart recovery, registration/heartbeat, cancellation, lease/redelivery, or remote artifact access; the Worker-side wire bodies were not captured in this run. Gaia's fetch removes a task before the Worker acknowledges it, and this run covers only the successful path. The `file://` URI identifies a local file, not a portable URL: a client on another host cannot retrieve it through Gaia. The `outputs` values shown above appear identically at both the workflow and `pipeline` DAG level because the DAG is the workflow's only entrypoint. The demo processes and local database were left running after the run; this report does not claim teardown or isolation from other local test data.
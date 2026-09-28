# Live test report: separate Gaia and multi-executor Worker

**Date:** 2026-09-28 (local time UTC+08:00; workflow timestamps in the API are UTC)

**Result:** PASS — workflow and all three task records reached `Succeeded` (`3/3`).
**Source revision:** `8ad8f57` plus the then-uncommitted `testdata/worker/` mock Worker and workflow fixture. This report records one observed run, not a guarantee of repeatability under failure.

## Setup and method

Gaia and the Worker ran as **separate processes**. Gaia listened on `127.0.0.1:18080` and used the existing local MySQL 8.4 and Redis 7.2.16 Docker services. The Store tables already existed; this run did not create a disposable database. Gaia used a run-specific Redis prefix and dummy Telegram token/chat ID; the workflow had no hooks. The Worker connected to `http://127.0.0.1:18080` as `demo-worker`, with concurrency `1` per executor (`prompt` and `image`), each polling once per second. There was no Worker registration or heartbeat.

The request was sent over loopback HTTP using `curl`, and Gaia's access log captured each Worker HTTP request and response. The submitted source is [`../testdata/worker/workflow.json`](../testdata/worker/workflow.json). Run artifacts, including full access logs and unabridged JSON responses, were saved outside the repository in `/tmp/gaia-worker-demo.U7zSpa/` (`gaia.log`, `worker.log`, `submit.json`, `final.json`); those temporary files may be removed later. The bodies and observations needed to understand the run are reproduced below. All listed application responses were HTTP 200 with `errNo: 0`.

**IDs observed:**

| Record | Run ID |
| --- | --- |
| Workflow | `d6450db8-4c91-42da-b2c8-0c5bfe49b0b3` |
| `pipeline` DAG | `2bc25c36-37a1-4e49-9433-c16d87cfc68e` |
| `describe` (`prompt`) | `587efd8f-1893-4da4-bf3c-5ac09ca0de7a` |
| `render` (`image`) | `225bf35c-7841-4834-be00-03419068fec3` |

## Step 1: start services and submit workflow

Gaia's `GET /` health check returned `Hello, World!`. The Worker logged `worker started`, `id=demo-worker`, `concurrencyPerExecutor=1`. Both executor queues initially returned `{"errNo":0,"errMsg":"succ","data":{}}` to the Worker's fetches (no assignment yet).

**Input:** `POST /workflow`, `Content-Type: application/json`. This is the complete submitted request body:

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
          "tasks": [
            {"name": "describe", "template": "describe-prompt"},
            {
              "name": "render", "template": "render-image", "dependencies": ["describe"],
               "arguments": {"parameters": [
                {"name": "prompt", "valueFrom": {"parameter": "tasks.describe.outputs.parameters.text"}}
              ]}}
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

**Output:** HTTP 200, `16:16:41.527813+08:00`:

```json
{"errNo":0,"errMsg":"succ","data":{"workflowRunID":"d6450db8-4c91-42da-b2c8-0c5bfe49b0b3"}}
```

This response acknowledges submission, not completion.

## Step 2: `prompt` Executor (`describe`)

**Fetch input:** `POST /task/fetch`:

```json
{"workerID":"v1::demo-worker::prompt"}
```

**Fetch output:** HTTP 200, `16:16:42.044067+08:00`:

```json
{"errNo":0,"errMsg":"succ","data":{"taskRunID":"587efd8f-1893-4da4-bf3c-5ac09ca0de7a","workflowRunID":"d6450db8-4c91-42da-b2c8-0c5bfe49b0b3","taskName":"describe","templateName":"describe-prompt","executorType":"prompt","inputs":{"parameters":[{"name":"prompt","type":"string","value":"a cat reading a book"},{"name":"images","type":"array","value":[]}]},"priority":500}}
```

**Start input:** `POST /task/start`:

```json
{"workerID":"v1::demo-worker::prompt","taskRunID":"587efd8f-1893-4da4-bf3c-5ac09ca0de7a"}
```

**Start output:** HTTP 200, `16:16:42.071711+08:00`:

```json
{"errNo":0,"errMsg":"succ","data":{}}
```

The Executor received `prompt="a cat reading a book"` and `images=[]` and produced `text="mock response: a cat reading a book"`. **Complete input:** `POST /task/complete`:

```json
{"taskRunID":"587efd8f-1893-4da4-bf3c-5ac09ca0de7a","workflowRunID":"d6450db8-4c91-42da-b2c8-0c5bfe49b0b3","parameters":[{"name":"text","value":"mock response: a cat reading a book"}]}
```

**Complete output:** HTTP 200, `16:16:42.095231+08:00`:

```json
{"errNo":0,"errMsg":"succ","data":{}}
```

The final inspection below confirms that Gaia stored this output and marked the task `Succeeded`; a completion HTTP acknowledgement alone would not prove persistence.

## Step 3: `image` Executor (`render`)

The DAG depends on `describe` and binds `tasks.describe.outputs.parameters.text` to `render`'s `prompt`. The observed assignment confirms this binding: the prompt is the *previous Executor's output*, not the original prompt.

**Fetch input:** `POST /task/fetch`:

```json
{"workerID":"v1::demo-worker::image"}
```

**Fetch output:** HTTP 200, `16:16:43.042297+08:00`:

```json
{"errNo":0,"errMsg":"succ","data":{"taskRunID":"225bf35c-7841-4834-be00-03419068fec3","workflowRunID":"d6450db8-4c91-42da-b2c8-0c5bfe49b0b3","taskName":"render","templateName":"render-image","executorType":"image","inputs":{"parameters":[{"name":"prompt","type":"string","value":"mock response: a cat reading a book"},{"name":"images","type":"array","value":[]}]},"priority":500}}
```

**Start input:** `POST /task/start`:

```json
{"workerID":"v1::demo-worker::image","taskRunID":"225bf35c-7841-4834-be00-03419068fec3"}
```

**Start output:** HTTP 200, `16:16:43.050797+08:00`:

```json
{"errNo":0,"errMsg":"succ","data":{}}
```

The Executor received `prompt="mock response: a cat reading a book"` and `images=[]`. It copied the embedded fixture to a local path and returned a `file://` URI. **Complete input:** `POST /task/complete`:

```json
{"taskRunID":"225bf35c-7841-4834-be00-03419068fec3","workflowRunID":"d6450db8-4c91-42da-b2c8-0c5bfe49b0b3","parameters":[{"name":"uri","value":"file:///var/folders/9c/sdtmtw0d2ws541g1wgbwm9pm0000gn/T/gaia-mock-worker-images/0bdf42e4f54e24be3d343805bf41a687.jpeg"}]}
```

**Complete output:** HTTP 200, `16:16:43.080353+08:00`:

```json
{"errNo":0,"errMsg":"succ","data":{}}
```

## Step 4: inspect workflow and verify image

**Input:** `GET /workflow?workflow_run_id=d6450db8-4c91-42da-b2c8-0c5bfe49b0b3` (no body). **Output:** HTTP 200, `16:16:51.498612+08:00`. The following is the observed API response with whitespace changed for readability:

```json
{
  "errNo": 0,
  "errMsg": "succ",
  "data": {
    "runID": "d6450db8-4c91-42da-b2c8-0c5bfe49b0b3",
    "status": "Succeeded",
    "message": "",
    "outputs": null,
    "metrics": {"startedAt":"2026-09-28T08:16:42Z","finishedAt":"2026-09-28T08:16:43Z","duration":"1.075s"},
    "createdAt": "2026-09-28T08:16:41.453809Z",
    "progress": "3/3",
    "tasks": [
      {
        "runID": "2bc25c36-37a1-4e49-9433-c16d87cfc68e",
        "workflowRunID": "d6450db8-4c91-42da-b2c8-0c5bfe49b0b3",
        "parentRunID": "", "depth": 0, "scope": "", "taskName": "pipeline", "templateName": "pipeline", "templateType": "dag",
        "createdAt": "2026-09-28T08:16:41.478448Z", "status": "Succeeded", "message": "", "inputs": null, "outputs": null,
        "metrics": {"startedAt":"2026-09-28T08:16:42Z","finishedAt":"2026-09-28T08:16:43Z","duration":"1.068s"}, "retryCount": 0
      },
      {
        "runID": "587efd8f-1893-4da4-bf3c-5ac09ca0de7a",
        "workflowRunID": "d6450db8-4c91-42da-b2c8-0c5bfe49b0b3",
        "parentRunID": "2bc25c36-37a1-4e49-9433-c16d87cfc68e", "depth": 1, "scope": "pipeline/", "taskName": "describe", "templateName": "describe-prompt", "templateType": "task",
        "createdAt": "2026-09-28T08:16:41.486746Z", "status": "Succeeded", "message": "",
        "inputs": {"parameters":[{"name":"prompt","type":"string","value":"a cat reading a book"},{"name":"images","type":"array","value":[]}]},
        "outputs": {"phase":"Succeeded","metrics":{"startedAt":"2026-09-28T08:16:42Z","finishedAt":"2026-09-28T08:16:42Z","duration":"74ms"},"parameters":[{"name":"text","value":"mock response: a cat reading a book"}]},
        "metrics": {"startedAt":"2026-09-28T08:16:42Z","finishedAt":"2026-09-28T08:16:42Z","duration":"74ms"}, "retryCount": 0
      },
      {
        "runID": "225bf35c-7841-4834-be00-03419068fec3",
        "workflowRunID": "d6450db8-4c91-42da-b2c8-0c5bfe49b0b3",
        "parentRunID": "2bc25c36-37a1-4e49-9433-c16d87cfc68e", "depth": 1, "scope": "pipeline/", "taskName": "render", "templateName": "render-image", "templateType": "task",
        "createdAt": "2026-09-28T08:16:42.076108Z", "status": "Succeeded", "message": "",
        "inputs": {"parameters":[{"name":"prompt","type":"string","value":"mock response: a cat reading a book"},{"name":"images","type":"array","value":[]}]},
        "outputs": {"phase":"Succeeded","metrics":{"startedAt":"2026-09-28T08:16:43Z","finishedAt":"2026-09-28T08:16:43Z","duration":"56ms"},"parameters":[{"name":"uri","value":"file:///var/folders/9c/sdtmtw0d2ws541g1wgbwm9pm0000gn/T/gaia-mock-worker-images/0bdf42e4f54e24be3d343805bf41a687.jpeg"}]},
        "metrics": {"startedAt":"2026-09-28T08:16:43Z","finishedAt":"2026-09-28T08:16:43Z","duration":"56ms"}, "retryCount": 0
      }
    ]
  }
}
```

The image URI resolved to an existing **749 × 749 JPEG** on the Worker host. A byte comparison with [`../testdata/worker/testdata/gaia_worker_img1.jpeg`](../testdata/worker/testdata/gaia_worker_img1.jpeg) passed. Both files had SHA-256 `b957f3d0abfbcbe2ff670371d600e33962af56ad52ec1d760d1d6df5486a3e83`.

## Checks and limitations

| Check | Result | Evidence |
| --- | --- | --- |
| Gaia and Worker run separately | PASS | Gaia served port 18080; Worker made its own fetch/start/complete HTTP calls. |
| One Worker hosts both Executor types | PASS | Same process logged successful `prompt` and `image` task reports, using type-specific Worker IDs. |
| Dependency and parameter handoff | PASS | `render`'s observed input prompt equals `describe`'s observed output text. |
| Persistence and completion | PASS | Inspection returned `Succeeded` for both leaf tasks, DAG, and workflow, with progress `3/3`. |
| Real local image file | PASS | `file://` path existed and matched fixture bytes and SHA-256. |
| Root Go tests | PASS | `go test ./...` completed successfully during the run. |
| Separate Worker module tests | PASS | `(cd testdata/worker && go test ./...)` completed successfully during the run. |

**Not tested:** restart recovery, registration/heartbeat, cancellation, lease/redelivery, or remote artifact access. Gaia's fetch removes a task before the Worker acknowledges it; this test covers the successful path only. The `file://` URI identifies a local file, not a portable URL: a client on another host cannot retrieve it through Gaia. The Workflow's top-level `outputs` is `null` because the DAG does not declare workflow outputs; inspect `render`'s task output for the image URI. The demo processes and local database were left running after the run; this report does not claim teardown or isolation from other local test data.

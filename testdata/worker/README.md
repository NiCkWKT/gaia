# Mock Worker

A standalone example Worker for Gaia. It hosts two Aether Executors in one process: `image` and `prompt`. Each executor type has `WORKER_CONCURRENCY` independent polling goroutines; each goroutine fetches once per second. No Worker registration, heartbeat, fetch lease, or redelivery is provided.

```sh
cd testdata/worker
go run .
# In this directory, run the module's tests separately from root go test ./...:
go test ./...
```

| Environment variable | Default | Meaning |
| --- | --- | --- |
| `GAIA_URL` | `http://localhost:8080` | Base URL of the trusted Gaia server |
| `WORKER_ID` | `mock-worker` | Process identifier; fetch IDs are `v1::<identifier>::image` and `v1::<identifier>::prompt` |
| `WORKER_CONCURRENCY` | `1` | Number of polling goroutines **per** executor type |

Both executors expect `prompt` (JSON string) and `images` (JSON array of strings, including `[]`) as named input parameters. `prompt` returns a `text` output parameter containing `mock response: <prompt>`. `image` returns a `uri` output parameter containing a `file://` URI for a local copy of `testdata/gaia_worker_img1.jpeg`. Its filename is a stable hash of the prompt and ordered images; the fixture bytes never change. The file is placed in the Worker's local temporary directory, **not** shared storage: another host cannot read this URI without its own copy. Neither executor calls an image or language-model service.

The Worker sends `/task/start` before execution and `/task/complete` afterward, including when executor input is invalid. Start or completion transport errors stop the affected polling loop and are logged: the current Gaia protocol cannot confirm whether an ambiguous callback was accepted. Fetch errors are logged and retried on the next tick. A `file://` output is only suitable for local testing; Gaia does not upload or serve it.

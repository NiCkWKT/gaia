# Gaia

Gaia is a Go service that hosts the [Aether](https://github.com/NiCkWKT/aether) workflow Engine. Clients submit workflows and inspect runs over HTTP; separately deployed Workers fetch and execute tasks. Gaia stores run state in MySQL and queues assignments in Redis. The repository is an **initial integration**, not a production-ready workflow platform.

Gaia imports `github.com/BabySid/aether` through the pinned NiCkWKT fork in [`go.mod`](go.mod). The interface roadmap and intended limitations are in [`spec/aether-interfaces.md`](spec/aether-interfaces.md); the checkboxes below describe the **current repository**, not just that plan.

## Implementation status

- [x] `idgen.Generator`: UUID-based run IDs (`idgen/`).
- [x] `store.Store`: MySQL 8.4 persistence via sqlc for workflow runs, task runs and executor schemas, including token-guarded updates (`store/`). **Partial:** cron methods, `DeleteWorkflowRun` and `DeleteSchema` return `ErrNotImplemented`.
- [x] `broker.TaskBroker`: Redis priority queues, fetch, and start/completion callbacks (`taskbroker/`). **Partial:** `Cancel` is unsupported; fetch has no lease or redelivery and is at-most-once.
- [x] `expr.Evaluator`: expression evaluation wired into the Engine (`expr/`).
- [x] `hook.Notifier` and `errsink.ErrorSink`: Telegram adapter wired into the Engine for configured lifecycle hooks and best-effort serious internal-error alerts (`notification/`). Requires a bot token and chat ID to start Gaia.
- [x] HTTP submit, inspect, fetch, start and complete endpoints (`http.go`). See the [JSON contract](spec/http-json-contract.md); this is a trusted-network API with no built-in authentication or TLS.
- [x] Example Worker with mock `executor.Plugin` implementations in [`testdata/worker/`](testdata/worker/README.md). This is not a production Worker; generated `file://` outputs stay on the Worker's host.
- [ ] `worker.Registry`: registration, discovery and heartbeats.
- [ ] `secret.Provider`, `vars.Source`, `artifact.Repository`, `timeout.Watcher`.
- [ ] `cron.Scheduler` and functional cron storage (cron scheduling is out of scope).
- [ ] Workflow cancellation/resume HTTP endpoints, broker cancellation, Worker ownership checks, fetch acknowledgments and recovery of in-flight work after restart.

A successful `/task/start` or `/task/complete` response confirms callback invocation, **not** durable acceptance. Redis removes an assignment on fetch: a Worker crash or lost response can lose it. Persisted in-flight runs are not automatically replayed on Engine startup. See the [broker decision](docs/adr/0001-start-with-at-most-once-redis-task-broker.md) for the delivery trade-off.

## Run locally

Requires Go 1.26.5, Docker with Compose, and `make`. The local server also requires Telegram credentials; supply them through your environment, not source control.

```sh
export GAIA_TELEGRAM_BOT_TOKEN=... GAIA_TELEGRAM_CHAT_ID=...
make start-gaia
```

In another terminal:

```sh
make start-worker
```

`make start-gaia` starts local MySQL 8.4 and Redis, applies the Store migration if needed, then listens on `127.0.0.1:8080`. `make start-worker` runs the polling mock Worker. Set `GAIA_MYSQL_DSN`, `GAIA_REDIS_ADDR`, `GAIA_REDIS_PREFIX`, or `GAIA_PORT` to override the local defaults; see [`docs/server.md`](docs/server.md) for deployment settings and [`testdata/worker/README.md`](testdata/worker/README.md) for Worker options. For other deployments, apply [`store/migrations/`](store/migrations/) **before** starting Gaia; the server checks the schema but does not migrate it.

The default local database is `gaia_test`. **Do not keep important data there:** Store tests drop and recreate its tables. Never expose Gaia's HTTP listener to untrusted networks.

## Test

```sh
make test
```

This starts local dependencies and runs the root Go test suite, including integration tests that use Docker. The Worker is a separate Go module and needs its own test command:

```sh
cd testdata/worker && go test ./...
```

Use `make stop` to stop the local Compose services. More details: [infrastructure](docs/infrastructure.md), [server setup](docs/server.md), [HTTP contract](spec/http-json-contract.md), and [interface plan](spec/aether-interfaces.md).

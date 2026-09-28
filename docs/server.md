# Running Gaia

Gaia serves five JSON routes for trusted clients and separately deployed Workers: `POST /workflow`, `GET /workflow?workflow_run_id=...`, `POST /task/fetch`, `POST /task/start`, and `POST /task/complete`. The hello route remains at `/`. See `spec/http-json-contract.md` for payloads, error numbers, and at-most-once delivery limitations. Worker IDs select a queue; they are not credentials. SRE must restrict access to trusted callers; **do not expose this listener to untrusted networks**.

Before starting Gaia, create a MySQL 8.4 database and apply the SQL migrations in `store/migrations/` as a separate deployment step. Gaia verifies that the Store schema is present but does not run migrations. Set:

- `GAIA_MYSQL_DSN`: MySQL driver DSN for the migrated database (for example, `user:password@tcp(127.0.0.1:3306)/gaia`). Gaia forces `parseTime`, UTC location, and UTC SQL sessions. Do not point the running server at the isolated test database; Store tests drop tables there.
- `GAIA_REDIS_ADDR`: reachable Redis address (`host:port`).
- `GAIA_REDIS_PREFIX`: nonempty deployment-specific queue prefix; keep it stable across restarts.
- `GAIA_TELEGRAM_BOT_TOKEN` and `GAIA_TELEGRAM_CHAT_ID`: required credentials for lifecycle notifications and serious internal error alerts. Provide them through a secret manager rather than committing them.
- `ADDR`: optional listen address (default `:8080`).

Gaia pings MySQL and Redis and checks the Store schema before listening; a missing setting or unavailable dependency prevents startup. Migrations, listener reachability, and any transport protection are deployment responsibilities. A successful task-start or task-complete HTTP response confirms callback invocation, not durable acceptance; an empty fetch is a success with `data: {}`.

Local services for development: `make setup`; isolated Store and broker integration tests: `make test`. HTTP integration tests launch isolated MySQL and Redis containers when Docker is available.

## Local run

Run each command in its own terminal; both block until interrupted.

```sh
make start-gaia    # starts MySQL and Redis, applies migrations, then serves on 127.0.0.1:8080
make start-worker  # mock Worker polling the local Gaia
```

`start-gaia` applies `store/migrations/` only when the three Store tables are absent, so it is safe to re-run. It defaults to the local `gaia_test` database, which `make test` drops and recreates: do not keep data you care about there. Override `GAIA_MYSQL_DSN`, `GAIA_REDIS_ADDR`, `GAIA_REDIS_PREFIX`, or `GAIA_PORT` for another target. It reads `GAIA_TELEGRAM_BOT_TOKEN` and `GAIA_TELEGRAM_CHAT_ID` from the environment; Gaia refuses to start when either is empty. `start-worker` sets `GAIA_URL`, `WORKER_ID`, and `WORKER_CONCURRENCY` for `testdata/worker`; see `testdata/worker/README.md`.

# Local infrastructure

Start and wait for MySQL 8.4 and Redis 7.2.16:

```sh
make setup
```

Both services bind only to localhost (`3306` and `6379`) and use named volumes. The Compose MySQL database is `gaia_test`, with development-only credentials `gaia_test` / `gaia-test-password` (root password `gaia-root-password`). Override the passwords with `GAIA_MYSQL_PASSWORD` and `GAIA_MYSQL_ROOT_PASSWORD` before first startup if needed; MySQL initialization variables do not update an existing volume.

Store integration tests **drop and recreate tables** in `gaia_test`. Never point this DSN at a database containing data you need to keep. Run the full suite (including Store and Broker integration tests) with:

```sh
make test
```

`make test` runs `make setup` first, exports `GAIA_TEST_MYSQL_DSN` for the Store tests, and runs `go test -count=1 ./...`. The default DSN uses the Compose MySQL password; if you set `GAIA_MYSQL_PASSWORD` when initializing Compose, set it for `make test` too. You can override `GAIA_TEST_MYSQL_DSN` directly for a different **isolated** database.

Broker integration tests launch their own isolated Redis containers via Docker; they do not use the Compose Redis service. Stop the Compose services with `make stop`, or remove them and their data with `docker compose down -v`.

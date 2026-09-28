.PHONY: setup migrate test start-gaia start-worker stop

GAIA_MYSQL_PASSWORD ?= gaia-test-password
GAIA_MYSQL_ROOT_PASSWORD ?= gaia-root-password
GAIA_DB ?= gaia_test
GAIA_TEST_MYSQL_DSN ?= gaia_test:$(GAIA_MYSQL_PASSWORD)@tcp(127.0.0.1:3306)/gaia_test?parseTime=true&loc=UTC&time_zone=%27%2B00%3A00%27
export GAIA_MYSQL_PASSWORD GAIA_TEST_MYSQL_DSN

# Local run settings. `make test` drops and recreates tables in GAIA_DB, so do not
# keep data you care about there. Override any value on the command line or by env.
GAIA_MYSQL_DSN ?= $(GAIA_TEST_MYSQL_DSN)
GAIA_REDIS_ADDR ?= 127.0.0.1:6379
GAIA_REDIS_PREFIX ?= gaia-local
GAIA_PORT ?= 8080
export GAIA_MYSQL_DSN GAIA_REDIS_ADDR GAIA_REDIS_PREFIX

GAIA_URL ?= http://localhost:$(GAIA_PORT)
WORKER_ID ?= mock-worker
WORKER_CONCURRENCY ?= 1

setup:
	docker compose up -d --wait

migrate: setup
	@docker compose exec -T mysql mysql -u root -p'$(GAIA_MYSQL_ROOT_PASSWORD)' -N -B \
		-e "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='$(GAIA_DB)' AND table_name IN ('workflow_runs','task_runs','executor_schemas')" \
		| grep -qx '3' \
		|| docker compose exec -T mysql mysql -u root -p'$(GAIA_MYSQL_ROOT_PASSWORD)' '$(GAIA_DB)' < store/migrations/000001_create_store.up.sql

test: setup
	go test -count=1 ./...

start-gaia: migrate
	ADDR='127.0.0.1:$(GAIA_PORT)' go run .

start-worker:
	cd testdata/worker && GAIA_URL='$(GAIA_URL)' WORKER_ID='$(WORKER_ID)' WORKER_CONCURRENCY='$(WORKER_CONCURRENCY)' go run .

stop:
	docker compose stop

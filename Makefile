.PHONY: setup test stop

GAIA_MYSQL_PASSWORD ?= gaia-test-password
GAIA_TEST_MYSQL_DSN ?= gaia_test:$(GAIA_MYSQL_PASSWORD)@tcp(127.0.0.1:3306)/gaia_test?parseTime=true&loc=UTC&time_zone=%27%2B00%3A00%27
export GAIA_MYSQL_PASSWORD GAIA_TEST_MYSQL_DSN

setup:
	docker compose up -d --wait

test: setup
	go test -count=1 ./...

stop:
	docker compose stop

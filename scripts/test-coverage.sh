#!/bin/sh
set -eu

# Set TEST_DATABASE_URL to reuse an isolated test server. Otherwise start one
# for this invocation, so database integration tests cannot silently skip.
if [ -z "${TEST_DATABASE_URL:-}" ]; then
  test_pg_root=$(mktemp -d /tmp/ai-buddy-tests.XXXXXX)
  test_pg_port=${TEST_POSTGRES_PORT:-55441}
  run_pg() {
    if [ "$(id -u)" = 0 ]; then su-exec postgres "$@"; else "$@"; fi
  }
  if [ "$(id -u)" = 0 ]; then chown postgres "$test_pg_root"; fi
  cleanup() {
    run_pg pg_ctl -D "$test_pg_root/data" -m immediate stop >/dev/null 2>&1 || true
    rm -rf "$test_pg_root"
  }
  trap cleanup EXIT INT TERM
  run_pg initdb -D "$test_pg_root/data" -A trust -U coverage >/dev/null
  run_pg pg_ctl -D "$test_pg_root/data" -l "$test_pg_root/postgres.log" -o "-h 127.0.0.1 -p $test_pg_port -k $test_pg_root" -w start >/dev/null
  export TEST_DATABASE_URL="postgres://coverage@127.0.0.1:$test_pg_port/postgres?sslmode=disable"
fi

coverage_file=${COVERAGE_FILE:-/tmp/ai-buddy-coverage.out}
go test -count=1 -coverpkg=./... -coverprofile="$coverage_file" ./...
go tool cover -func="$coverage_file" | awk '/^total:/ {print; gsub("%", "", $3); if ($3 + 0 < 90) exit 1}'

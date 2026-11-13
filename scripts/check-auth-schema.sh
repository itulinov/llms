#!/usr/bin/env bash
# Изолированная проверка миграции 004; рабочую lms не изменяет.
set -euo pipefail
cd "$(dirname "$0")/.."
check_db="lms_auth_check_${RANDOM}_$$"
psql_db() { docker compose exec -T db psql -X -q -v ON_ERROR_STOP=1 -U lms "$@"; }
cleanup() { psql_db -d postgres -c "DROP DATABASE IF EXISTS $check_db"; }
psql_db -d postgres -c "CREATE DATABASE $check_db"
trap cleanup EXIT
for file in postgres/migrations/00[123]_*.sql; do
    psql_db -d "$check_db" --single-transaction -f - < "$file"
done
psql_db -d "$check_db" -c "INSERT INTO collaborators(lastname) VALUES ('Проверка')"
psql_db -d "$check_db" --single-transaction -f - < postgres/migrations/004_create_auth.sql
psql_db -d "$check_db" --single-transaction -f - < postgres/tests/auth_schema.sql
echo 'Проверки авторизации пройдены'

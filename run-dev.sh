#!/usr/bin/env bash
set -Eeuo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/load-env.sh
source "$ROOT_DIR/scripts/load-env.sh"
for env_file in "$ROOT_DIR/.env" "$ROOT_DIR/backend/.env"; do
  load_env_file "$env_file"
done
FRONTEND_PORT="${FRONTEND_PORT:-15680}"
BACKEND_PORT="${BACKEND_PORT:-15681}"
DB_PORT="${DB_PORT:-15682}"
export DATABASE_URL="${DATABASE_URL:-postgres://postgres:postgres@localhost:${DB_PORT}/getgpt?sslmode=disable}"
export BACKEND_ADDR="${BACKEND_ADDR:-:${BACKEND_PORT}}"
DB_CONTAINER_NAME="${DB_CONTAINER_NAME:-getgpt-postgres-15682}"
command -v go >/dev/null || { echo "缺少 go" >&2; exit 1; }
command -v npm >/dev/null || { echo "缺少 npm" >&2; exit 1; }
if command -v docker >/dev/null; then
  if docker container inspect "$DB_CONTAINER_NAME" >/dev/null 2>&1; then
    docker start "$DB_CONTAINER_NAME" >/dev/null 2>&1 || true
  else
    docker run -d --name "$DB_CONTAINER_NAME" \
      -e POSTGRES_USER=postgres \
      -e POSTGRES_PASSWORD=postgres \
      -e POSTGRES_DB=getgpt \
      -p "${DB_PORT}:5432" \
      -v getgpt_pgdata:/var/lib/postgresql/data \
      postgres:16-alpine >/dev/null
  fi
  for _ in $(seq 1 30); do
    docker exec "$DB_CONTAINER_NAME" pg_isready -U postgres -d getgpt >/dev/null 2>&1 && break
    sleep 1
  done
  docker exec -i "$DB_CONTAINER_NAME" psql -v ON_ERROR_STOP=1 -U postgres -d getgpt < "$ROOT_DIR/backend/migrations/schema.sql" >/dev/null
  for migration in "$ROOT_DIR/backend/migrations/"[0-9][0-9][0-9]_*.sql; do
    [ -f "$migration" ] || continue
    docker exec -i "$DB_CONTAINER_NAME" psql -v ON_ERROR_STOP=1 -U postgres -d getgpt < "$migration" >/dev/null
  done
else
  echo "未找到 docker，将直接使用 DATABASE_URL 中的 PostgreSQL" >&2
fi
(cd "$ROOT_DIR/backend" && go run ./cmd/server) & BACKEND_PID=$!
if [ ! -d "$ROOT_DIR/frontend/node_modules" ]; then (cd "$ROOT_DIR/frontend" && npm install); fi
(cd "$ROOT_DIR/frontend" && VITE_API_URL="http://localhost:${BACKEND_PORT}/api" npm run dev -- --port "$FRONTEND_PORT" --strictPort) & FRONTEND_PID=$!
cleanup(){ kill "$BACKEND_PID" "$FRONTEND_PID" 2>/dev/null || true; }
trap cleanup EXIT INT TERM
echo "getgpt 前端: http://localhost:${FRONTEND_PORT}"
echo "getgpt 后端: http://localhost:${BACKEND_PORT}"
wait "$BACKEND_PID" "$FRONTEND_PID"

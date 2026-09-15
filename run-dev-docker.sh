#!/usr/bin/env bash
set -Eeuo pipefail

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$PROJECT_DIR/.env.docker"
ENV_NAME=local
APP_SERVICES=(backend frontend)
DB_SERVICES=(postgres)
SCRIPT_ARGS=()

usage() {
  cat <<'EOF'
用法: ./run-dev-docker.sh [--env=<环境>] <命令> [服务]

参数:
  --env=<环境>       额外读取 backend/.env.<环境>，默认 local

命令:
  up                 构建并启动全部服务，初始化本地数据库
  down               停止并移除全部容器（保留数据库卷）
  app-start          仅启动前后端（需先启动数据库）
  app-restart        仅重建前后端容器，重新加载配置（不构建镜像）
  app-down           仅停止并移除前后端
  db-start           启动 PostgreSQL 并初始化本地表结构
  db-restart         重建 PostgreSQL 容器（保留数据库卷）
  db-down            仅停止并移除 PostgreSQL（保留数据库卷）
  stop               停止全部服务
  restart            重建全部容器，重新加载配置（不构建镜像）
  build              重新构建前后端镜像
  status             查看服务状态
  logs [service...]  持续查看日志，可选 backend / frontend / postgres
  db                 db-start 的别名
  shell-api          进入 Go 后端容器
  shell-admin        进入前端容器
  help               显示帮助

配置依次读取 .env、backend/.env、.env.docker、backend/.env.<环境>。
Docker 使用独立的 aitok-dev 项目和数据库卷；代码修改后执行 up 重新构建。
启动或重启时自动停止占用目标端口的其他容器或本机进程。
EOF
}

fail() {
  echo "$*" >&2
  exit 1
}

require_docker() {
  command -v docker >/dev/null 2>&1 || fail "缺少 Docker，请先安装 Docker Desktop 或 Docker Engine。"
  docker compose version >/dev/null 2>&1 || fail "缺少 Docker Compose 插件，请先安装。"
  docker info >/dev/null 2>&1 || fail "Docker daemon 未运行，请先启动 Docker。"
}

configure_environment() {
  local env_path key generated
  [[ "$ENV_NAME" =~ ^[A-Za-z0-9_-]+$ ]] || fail "环境名称只能包含字母、数字、下划线和短横线: $ENV_NAME"
  if [ "$ENV_NAME" != local ] && [ ! -f "$PROJECT_DIR/backend/.env.$ENV_NAME" ]; then
    fail "未找到环境文件: $PROJECT_DIR/backend/.env.$ENV_NAME"
  fi

  if [ ! -f "$ENV_FILE" ]; then
    (umask 077; cp "$PROJECT_DIR/.env.docker.example" "$ENV_FILE")
    echo "已生成本地 Docker 配置: $ENV_FILE"
  fi

  # 不使用 source 读取环境文件，保留连接串中的 &、$ 等字符。
  # shellcheck source=scripts/load-env.sh
  source "$PROJECT_DIR/scripts/load-env.sh"
  for env_path in "$PROJECT_DIR/.env" "$PROJECT_DIR/backend/.env" "$ENV_FILE"; do
    load_env_file "$env_path"
  done

  # 首次生成后持久保存，重启不会改变登录或 Session 加密密钥。
  for key in JWT_SECRET SESSION_ENCRYPTION_KEY; do
    if [ -z "${!key:-}" ]; then
      command -v openssl >/dev/null 2>&1 || fail "缺少 openssl，请安装或在 .env.docker 中配置 ${key}。"
      if [ "$key" = JWT_SECRET ]; then
        generated="$(openssl rand -hex 32)"
      else
        generated="$(openssl rand -base64 32)"
      fi
      chmod 600 "$ENV_FILE"
      printf '\n%s=%s\n' "$key" "$generated" >> "$ENV_FILE"
      export "$key=$generated"
    fi
  done

  if [ "$ENV_NAME" != local ]; then
    load_env_file "$PROJECT_DIR/backend/.env.$ENV_NAME"
  fi

  export FRONTEND_PORT="${FRONTEND_PORT:-15680}"
  export BACKEND_PORT="${BACKEND_PORT:-15681}"
  export DB_PORT="${DB_PORT:-15682}"
  export APP_BASE_URL="${APP_BASE_URL:-http://localhost:$FRONTEND_PORT}"
  PROJECT_NAME="${DEV_PROJECT_NAME:-aitok-dev}"
  export DB_CONTAINER_NAME="${DB_CONTAINER_NAME:-${PROJECT_NAME}-postgres}"
  WAIT_TIMEOUT="${DEV_WAIT_TIMEOUT:-120}"
  [[ "$WAIT_TIMEOUT" =~ ^[1-9][0-9]*$ ]] || fail "DEV_WAIT_TIMEOUT 必须是正整数。"

  # 环境文件已经按原值导出，禁用 Compose 再次读取 .env 和变量插值。
  # 数据库与应用共用开发网络配置，避免数据库先启动时仍申请默认地址池。
  COMPOSE=(docker compose --project-name "$PROJECT_NAME" --project-directory "$PROJECT_DIR" --env-file /dev/null -f "$PROJECT_DIR/docker-compose.yml" -f "$PROJECT_DIR/docker-compose.dev.yml")
}

print_app_addresses() {
  echo "运行环境: $ENV_NAME"
  echo "应用服务地址:"
  echo "  前端: http://localhost:$FRONTEND_PORT"
  echo "  API:  http://localhost:$BACKEND_PORT"
}

print_db_addresses() {
  echo "本地 PostgreSQL: 127.0.0.1:${DB_PORT}（数据库 getgpt）"
}

release_service_port() {
  local service="$1" port="$2" container_ids container_id details owner_project owner_service published_ports
  local own_container=false pids pid process_name attempt remaining
  command -v lsof >/dev/null 2>&1 || fail "自动释放端口需要 lsof，请先安装。"

  # 按宿主机发布端口精确匹配；不能直接结束 OrbStack / Docker 的端口代理进程。
  container_ids="$(docker ps --no-trunc --filter "publish=$port" --quiet)"
  for container_id in $container_ids; do
    details="$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.service"}}|{{range .NetworkSettings.Ports}}{{range .}}{{.HostPort}} {{end}}{{end}}' "$container_id")"
    IFS='|' read -r owner_project owner_service published_ports <<< "$details"
    [[ " $published_ports" == *" $port "* ]] || continue
    if [ "$owner_project" = "$PROJECT_NAME" ] && [ "$owner_service" = "$service" ]; then
      own_container=true
      continue
    fi
    echo "端口 $port 被容器 $container_id 占用，正在停止..."
    docker stop --timeout 10 "$container_id" >/dev/null
  done
  [ "$own_container" = false ] || return 0

  pids="$(lsof -nP -t -iTCP:"$port" -sTCP:LISTEN 2>/dev/null | sort -u || true)"
  pids="${pids//$'\n'/ }"
  [ -n "$pids" ] || return 0
  for pid in $pids; do
    process_name="$(ps -p "$pid" -o comm= 2>/dev/null || true)"
    case "$process_name" in
      *OrbStack*|*orb*stack*|*com.docker*|*docker-proxy*|*dockerd*|*vpnkit*|*rootlesskit*)
        fail "端口 $port 仍由 Docker 运行时代理占用，请检查对应容器；不能结束运行时进程。"
        ;;
    esac
  done
  for pid in $pids; do
    echo "端口 $port 被进程 $pid 占用，正在结束..."
    kill -TERM "$pid" 2>/dev/null || true
  done
  for ((attempt = 0; attempt < 15; attempt++)); do
    remaining="$(lsof -nP -t -iTCP:"$port" -sTCP:LISTEN 2>/dev/null | sort -u || true)"
    [ -n "$remaining" ] || return 0
    sleep 0.2
  done
  # 只强制结束最初的占用者，避免杀掉等待期间新启动的进程。
  for pid in $remaining; do
    case " $pids " in
      *" $pid "*) kill -KILL "$pid" 2>/dev/null || fail "无法结束占用端口 $port 的进程 $pid。" ;;
    esac
  done
  for ((attempt = 0; attempt < 10; attempt++)); do
    remaining="$(lsof -nP -t -iTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)"
    [ -n "$remaining" ] || return 0
    sleep 0.2
  done
  fail "端口 $port 仍被占用: $remaining"
}

release_app_ports() {
  release_service_port backend "$BACKEND_PORT"
  release_service_port frontend "$FRONTEND_PORT"
}

initialize_local_database() {
  echo "初始化本地 PostgreSQL 表结构..."
  # 只连接本脚本的 postgres 服务，不对 DATABASE_URL 指向的外部数据库执行 SQL。
  bash "$PROJECT_DIR/scripts/build-release-sql.sh" |
    "${COMPOSE[@]}" exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d getgpt >/dev/null
}

start_database() {
  release_service_port postgres "$DB_PORT"
  "${COMPOSE[@]}" up -d --wait --wait-timeout "$WAIT_TIMEOUT" "$@" "${DB_SERVICES[@]}"
  initialize_local_database
}

while (($# > 0)); do
  case "$1" in
    --env=*) ENV_NAME="${1#*=}" ;;
    --env)
      shift
      (($# > 0)) || fail "--env 缺少环境名称。"
      ENV_NAME="$1"
      ;;
    *) SCRIPT_ARGS+=("$1") ;;
  esac
  shift
done

command_name="${SCRIPT_ARGS[0]:-help}"
if ((${#SCRIPT_ARGS[@]} > 1)); then
  set -- "${SCRIPT_ARGS[@]:1}"
else
  set --
fi

# 帮助与参数错误不要求 Docker，也不生成本地配置。
case "$command_name" in
  help|-h|--help) usage; exit 0 ;;
  up|down|app-start|app-restart|app-down|db-start|db-restart|db-down|stop|restart|build|status|db|shell-api|shell-admin)
    (($# == 0)) || fail "$command_name 不接受额外参数。"
    ;;
  logs)
    for service in "$@"; do
      case "$service" in
        backend|frontend|postgres) ;;
        *) fail "未知服务: ${service}（可选 backend / frontend / postgres）" ;;
      esac
    done
    ;;
  *) usage >&2; fail "未知命令: $command_name" ;;
esac

require_docker
configure_environment

case "$command_name" in
  up)
    "${COMPOSE[@]}" build "${APP_SERVICES[@]}"
    start_database
    release_app_ports
    "${COMPOSE[@]}" up -d --no-deps --no-build --wait --wait-timeout "$WAIT_TIMEOUT" "${APP_SERVICES[@]}"
    "${COMPOSE[@]}" ps
    print_app_addresses
    print_db_addresses
    ;;
  down) "${COMPOSE[@]}" down --remove-orphans ;;
  app-start|app-restart)
    release_app_ports
    app_args=(--no-deps)
    if [ "$command_name" = app-restart ]; then
      app_args+=(--no-build --force-recreate)
    fi
    "${COMPOSE[@]}" up -d --wait --wait-timeout "$WAIT_TIMEOUT" "${app_args[@]}" "${APP_SERVICES[@]}"
    "${COMPOSE[@]}" ps "${APP_SERVICES[@]}"
    print_app_addresses
    ;;
  app-down) "${COMPOSE[@]}" rm --stop --force "${APP_SERVICES[@]}" ;;
  db-start|db|db-restart)
    if [ "$command_name" = db-restart ]; then
      start_database --force-recreate
    else
      start_database
    fi
    "${COMPOSE[@]}" ps "${DB_SERVICES[@]}"
    print_db_addresses
    ;;
  db-down) "${COMPOSE[@]}" rm --stop --force "${DB_SERVICES[@]}" ;;
  stop) "${COMPOSE[@]}" stop ;;
  restart)
    start_database --force-recreate
    release_app_ports
    "${COMPOSE[@]}" up -d --no-deps --no-build --force-recreate --wait --wait-timeout "$WAIT_TIMEOUT" "${APP_SERVICES[@]}"
    "${COMPOSE[@]}" ps
    print_app_addresses
    print_db_addresses
    ;;
  build) "${COMPOSE[@]}" build "${APP_SERVICES[@]}" ;;
  status) "${COMPOSE[@]}" ps ;;
  logs) "${COMPOSE[@]}" logs --follow --tail=200 "$@" ;;
  shell-api) "${COMPOSE[@]}" exec backend sh ;;
  shell-admin) "${COMPOSE[@]}" exec frontend sh ;;
esac

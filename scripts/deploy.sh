#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="$ROOT_DIR/docker-compose.deploy.yml"
BUILDKIT_CONFIG="$ROOT_DIR/scripts/buildkitd.toml"
SKIP_PULL=false
CHECK_ONLY=false

usage() {
  echo "用法: bash scripts/deploy.sh [--skip-pull] [--check]"
  echo "  --skip-pull  使用当前代码部署，不执行 git pull"
  echo "  --check      仅检查配置，不拉取代码、构建或启动服务"
}

for arg in "$@"; do
  case "$arg" in
    --skip-pull) SKIP_PULL=true ;;
    --check) CHECK_ONLY=true ;;
    -h|--help) usage; exit 0 ;;
    *) echo "未知参数: $arg" >&2; usage >&2; exit 1 ;;
  esac
done

trap 'echo "部署失败（第 ${LINENO} 行），请根据上方错误处理后重试。" >&2' ERR
cd "$ROOT_DIR"
command -v docker >/dev/null || { echo "缺少 Docker" >&2; exit 1; }
docker compose version >/dev/null

if ! "$CHECK_ONLY" && ! "$SKIP_PULL"; then
  command -v git >/dev/null || { echo "缺少 git" >&2; exit 1; }
  echo "拉取最新代码..."
  git pull --ff-only
fi

# 与本地启动保持相同顺序，backend/.env 覆盖根目录配置。
# shellcheck source=scripts/load-env.sh
source "$ROOT_DIR/scripts/load-env.sh"
for env_file in "$ROOT_DIR/.env" "$ROOT_DIR/backend/.env"; do
  load_env_file "$env_file"
done

BUILD_CPU_COUNT="${BUILD_CPU_COUNT:-1}"
COMPOSE_PARALLEL_LIMIT="${COMPOSE_PARALLEL_LIMIT:-1}"
DEPLOY_BUILDER_NAME="${DEPLOY_BUILDER_NAME:-aitok-limited-${BUILD_CPU_COUNT}cpu}"
DEPLOY_WAIT_TIMEOUT="${DEPLOY_WAIT_TIMEOUT:-120}"
export BUILD_CPU_COUNT COMPOSE_PARALLEL_LIMIT

require_positive_integer() {
  local name="$1" value="$2"
  case "$value" in
    ''|*[!0-9]*) echo "$name 必须是正整数" >&2; exit 1 ;;
  esac
  if [ "$value" -lt 1 ]; then
    echo "$name 必须大于 0" >&2
    exit 1
  fi
}

require_positive_integer "BUILD_CPU_COUNT" "$BUILD_CPU_COUNT"
require_positive_integer "COMPOSE_PARALLEL_LIMIT" "$COMPOSE_PARALLEL_LIMIT"
require_positive_integer "DEPLOY_WAIT_TIMEOUT" "$DEPLOY_WAIT_TIMEOUT"

# 只验证服务配置，不输出解析后的密钥。
compose=(docker compose --project-name aitok --env-file /dev/null -f "$COMPOSE_FILE")
"${compose[@]}" config --quiet
if "$CHECK_ONLY"; then
  echo "部署配置检查通过：仅包含 frontend、backend 服务。"
  exit 0
fi

docker info >/dev/null
docker buildx version >/dev/null
BUILDKIT_CPU_PERIOD=100000
BUILDKIT_CPU_QUOTA=$((BUILD_CPU_COUNT * BUILDKIT_CPU_PERIOD))
if ! docker buildx inspect "$DEPLOY_BUILDER_NAME" >/dev/null 2>&1; then
  echo "创建限额构建器（CPU: ${BUILD_CPU_COUNT}）..."
  docker buildx create \
    --name "$DEPLOY_BUILDER_NAME" \
    --driver docker-container \
    --driver-opt "cpu-period=$BUILDKIT_CPU_PERIOD" \
    --driver-opt "cpu-quota=$BUILDKIT_CPU_QUOTA" \
    --driver-opt "default-load=true" \
    --buildkitd-config "$BUILDKIT_CONFIG"
fi

echo "启动限额构建器..."
docker buildx inspect "$DEPLOY_BUILDER_NAME" --bootstrap >/dev/null
echo "构建前后端镜像（CPU: ${BUILD_CPU_COUNT}，并行任务: ${COMPOSE_PARALLEL_LIMIT}）..."
"${compose[@]}" build --builder "$DEPLOY_BUILDER_NAME" backend frontend

echo "更新前后端服务并等待健康检查..."
if ! "${compose[@]}" up -d --no-deps --no-build --wait --wait-timeout "$DEPLOY_WAIT_TIMEOUT" backend frontend; then
  "${compose[@]}" ps
  echo "服务未就绪，请检查上方容器状态；加载相同环境变量后可使用 docker compose -p aitok -f docker-compose.deploy.yml logs 查看日志。" >&2
  exit 1
fi

"${compose[@]}" ps
echo "部署完成，前端端口: ${FRONTEND_PORT:-15680}，后端端口: ${BACKEND_PORT:-15681}"

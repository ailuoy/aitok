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
  echo "  --check      检查配置和 Docker CPU 预算，不拉取代码、构建或启动服务"
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
  if ! git diff --quiet || ! git diff --cached --quiet; then
    echo "存在未提交的已跟踪文件，请先处理后部署；部署当前工作区可使用 --skip-pull。" >&2
    exit 1
  fi
  echo "拉取最新代码..."
  git pull --ff-only
  # pull 可能更新本脚本；重新执行，避免继续使用已加载的旧逻辑。
  exec bash "$ROOT_DIR/scripts/deploy.sh" --skip-pull
fi

# 与本地启动保持相同顺序，backend/.env 覆盖根目录配置。
# shellcheck source=scripts/load-env.sh
source "$ROOT_DIR/scripts/load-env.sh"
for env_file in "$ROOT_DIR/.env" "$ROOT_DIR/backend/.env"; do
  load_env_file "$env_file"
done

COMPOSE_PARALLEL_LIMIT="${COMPOSE_PARALLEL_LIMIT:-1}"
DEPLOY_WAIT_TIMEOUT="${DEPLOY_WAIT_TIMEOUT:-120}"

require_positive_integer() {
  local name="$1" value="$2"
  if [[ ! "$value" =~ ^[1-9][0-9]{0,5}$ ]]; then
    echo "$name 必须是 1 至 999999 的整数，不允许前导零" >&2
    exit 1
  fi
}

require_positive_integer "COMPOSE_PARALLEL_LIMIT" "$COMPOSE_PARALLEL_LIMIT"
require_positive_integer "DEPLOY_WAIT_TIMEOUT" "$DEPLOY_WAIT_TIMEOUT"

# 读取 Docker 服务端可用 CPU，避免远程 Docker/虚拟机与执行脚本的主机核数不同。
DOCKER_CPU_COUNT="$(docker info --format '{{.NCPU}}')"
require_positive_integer "Docker CPU 数" "$DOCKER_CPU_COUNT"
docker buildx version >/dev/null
BUILDKIT_CPU_PERIOD=100000
BUILDKIT_CPU_QUOTA=$((DOCKER_CPU_COUNT * BUILDKIT_CPU_PERIOD / 2))
if [ -n "${BUILD_CPU_COUNT:-}" ]; then
  require_positive_integer "BUILD_CPU_COUNT" "$BUILD_CPU_COUNT"
  requested_quota=$((BUILD_CPU_COUNT * BUILDKIT_CPU_PERIOD))
  if [ "$requested_quota" -lt "$BUILDKIT_CPU_QUOTA" ]; then
    BUILDKIT_CPU_QUOTA="$requested_quota"
  elif [ "$requested_quota" -gt "$BUILDKIT_CPU_QUOTA" ]; then
    echo "BUILD_CPU_COUNT 超过 Docker CPU 的一半，已限制到 50% 上限。"
  fi
fi
# Go 编译并行度取整，但内核配额保留半核：单核服务器最多使用 0.5 核。
BUILD_CPU_COUNT=$((BUILDKIT_CPU_QUOTA / BUILDKIT_CPU_PERIOD))
BUILD_CPU_LIMIT="$BUILD_CPU_COUNT"
if [ $((BUILDKIT_CPU_QUOTA % BUILDKIT_CPU_PERIOD)) -ne 0 ]; then
  BUILD_CPU_LIMIT="${BUILD_CPU_LIMIT}.5"
fi
if [ "$BUILD_CPU_COUNT" -lt 1 ]; then BUILD_CPU_COUNT=1; fi
DEPLOY_BUILDER_NAME="${DEPLOY_BUILDER_NAME:-aitok-half-${BUILDKIT_CPU_QUOTA}}"
if [[ ! "$DEPLOY_BUILDER_NAME" =~ ^[a-zA-Z][a-zA-Z0-9_.-]*$ ]]; then
  echo "DEPLOY_BUILDER_NAME 必须以字母开头，仅包含字母、数字、下划线、点或横线" >&2
  exit 1
fi
export BUILD_CPU_COUNT COMPOSE_PARALLEL_LIMIT

# 只验证服务配置，不输出解析后的密钥。
compose=(docker compose --project-name aitok --env-file /dev/null -f "$COMPOSE_FILE")
"${compose[@]}" config --quiet
if [ "${#JWT_SECRET}" -lt 32 ]; then
  echo "JWT_SECRET 必须至少 32 个字符" >&2
  exit 1
fi
if [[ ! "$SESSION_ENCRYPTION_KEY" =~ ^[A-Za-z0-9+/]{43}=$ ]]; then
  echo "SESSION_ENCRYPTION_KEY 必须是 32 字节密钥的 Base64 编码；已有数据请沿用原密钥" >&2
  exit 1
fi
echo "Docker 可用 CPU: ${DOCKER_CPU_COUNT} 核；构建上限: ${BUILD_CPU_LIMIT} 核；Go 编译并行度: ${BUILD_CPU_COUNT}。"
if "$CHECK_ONLY"; then
  echo "部署配置检查通过：仅包含 frontend、backend 服务。"
  exit 0
fi

if ! docker buildx inspect "$DEPLOY_BUILDER_NAME" >/dev/null 2>&1; then
  echo "创建限额构建器（CPU 上限: ${BUILD_CPU_LIMIT} 核）..."
  docker buildx create \
    --name "$DEPLOY_BUILDER_NAME" \
    --node "${DEPLOY_BUILDER_NAME}0" \
    --driver docker-container \
    --driver-opt "cpu-period=$BUILDKIT_CPU_PERIOD" \
    --driver-opt "cpu-quota=$BUILDKIT_CPU_QUOTA" \
    --driver-opt "default-load=true" \
    --buildkitd-config "$BUILDKIT_CONFIG"
fi

# 不复用 docker 驱动或多节点构建器，防止任务绕过同一个容器的总配额。
builder_info="$(docker buildx inspect "$DEPLOY_BUILDER_NAME")"
builder_driver="$(awk '$1 == "Driver:" { print $2 }' <<< "$builder_info")"
builder_node="$(awk '/^Nodes:/ { nodes=1; next } nodes && $1 == "Name:" { print $2 }' <<< "$builder_info")"
if [ "$builder_driver" != docker-container ] || [ -z "$builder_node" ] || [[ "$builder_node" == *$'\n'* ]]; then
  echo "构建器必须使用 docker-container 驱动且只有一个节点，请为 DEPLOY_BUILDER_NAME 设置独立名称。" >&2
  exit 1
fi
echo "启动限额构建器..."
docker buildx inspect "$DEPLOY_BUILDER_NAME" --bootstrap >/dev/null
builder_container="buildx_buildkit_${builder_node}"
cpu_format='{{.HostConfig.CpuPeriod}} {{.HostConfig.CpuQuota}} {{.HostConfig.NanoCpus}}'
cpu_limits="$(docker inspect --type container --format "$cpu_format" "$builder_container")"
expected_limits="$BUILDKIT_CPU_PERIOD $BUILDKIT_CPU_QUOTA 0"
if [ "$cpu_limits" != "$expected_limits" ]; then
  echo "校正已有构建器 CPU 限额，保留构建缓存..."
  docker update --cpu-period "$BUILDKIT_CPU_PERIOD" --cpu-quota "$BUILDKIT_CPU_QUOTA" "$builder_container" >/dev/null
fi
if [ "$(docker inspect --type container --format "$cpu_format" "$builder_container")" != "$expected_limits" ]; then
  echo "构建器 CPU 限额未生效，已停止构建。" >&2
  exit 1
fi
echo "构建前后端镜像（总 CPU 上限: ${BUILD_CPU_LIMIT} 核，并行任务: ${COMPOSE_PARALLEL_LIMIT}）..."
"${compose[@]}" build --builder "$DEPLOY_BUILDER_NAME" backend frontend

echo "更新前后端服务并等待健康检查..."
if ! "${compose[@]}" up -d --no-deps --no-build --wait --wait-timeout "$DEPLOY_WAIT_TIMEOUT" backend frontend; then
  "${compose[@]}" ps
  echo "服务未就绪，请检查上方容器状态；加载相同环境变量后可使用 docker compose -p aitok -f docker-compose.deploy.yml logs 查看日志。" >&2
  exit 1
fi

"${compose[@]}" ps
echo "部署完成，前端端口: ${FRONTEND_PORT:-15680}，后端端口: ${BACKEND_PORT:-15681}"

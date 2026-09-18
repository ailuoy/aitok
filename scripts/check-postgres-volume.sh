#!/usr/bin/env bash
set -Eeuo pipefail

# 只读检查，绝不使用新版本数据库直接打开旧数据目录。
volume="${1:?请指定 PostgreSQL 数据卷}"
if ! docker volume inspect "$volume" >/dev/null 2>&1; then
  exit 0
fi

layout="$(docker run --rm --network none \
  --mount "type=volume,src=$volume,dst=/volume,readonly" \
  --entrypoint sh postgres:18.3-alpine -c '
    for path in /volume/PG_VERSION /volume/data/PG_VERSION /volume/*/docker/PG_VERSION; do
      if [ -f "$path" ]; then
        printf "%s:%s\n" "${path#/volume/}" "$(cat "$path")"
      fi
    done
  ')"

if [ -z "$layout" ] || [ "$layout" = '18/docker/PG_VERSION:18' ]; then
  exit 0
fi

printf 'PostgreSQL 数据卷 %s 与 postgres:18.3-alpine 不兼容：\n%s\n' "$volume" "$layout" >&2
printf '%s\n' '已停止启动，原数据卷未修改。请先备份并迁移到独立的 PostgreSQL 18.3 数据卷，再设置 DEV_DB_VOLUME_NAME 指向新卷。禁止清空旧卷或直接替换镜像启动旧数据。' >&2
exit 1

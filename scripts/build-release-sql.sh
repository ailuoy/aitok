#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MIGRATION_DIR="$ROOT_DIR/backend/migrations"
sources=("$MIGRATION_DIR/schema.sql" "$MIGRATION_DIR/"[0-9][0-9][0-9]_*.sql)

# 输出到标准输出，由调用方选择保存位置；本脚本不执行 SQL。
for source_file in "${sources[@]}"; do
  if [ ! -f "$source_file" ]; then
    echo "迁移文件不存在: $source_file" >&2
    exit 1
  fi
done

cat <<'SQL'
-- AiTok 上线 SQL：适用于空库及本项目历史版本升级。
-- 自动生成，请修改 schema.sql / 编号迁移后重新生成，不要单独编辑本文件。
-- 生成命令：bash scripts/build-release-sql.sh > backend/migrations/release.sql
-- 连接已创建的目标数据库后执行；不包含创建数据库、用户、授权或测试数据。
-- 所有结构调整在一个事务内执行，报错时回滚本次调整。
-- IF NOT EXISTS 支持重复执行，但不会修复同名对象的结构差异。

BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';
SQL

for source_file in "${sources[@]}"; do
  printf '\n-- 来源：%s\n' "${source_file##*/}"
  # 合并现有迁移的事务边界，避免中途 COMMIT 导致部分结构已上线。
  sed '/^[[:space:]]*BEGIN;[[:space:]]*$/d; /^[[:space:]]*COMMIT;[[:space:]]*$/d' "$source_file"
done

printf '\nCOMMIT;\n'

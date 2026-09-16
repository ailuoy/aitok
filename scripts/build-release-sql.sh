#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MIGRATION_DIR="$ROOT_DIR/backend/migrations"
# 完整 schema 快照仅用于空库初始化；旧库必须从不可变基线逐步升级。
sources=("$MIGRATION_DIR/"[0-9][0-9][0-9]_*.sql)

# 输出到标准输出，由调用方选择保存位置；本脚本不执行 SQL。
for source_file in "${sources[@]}"; do
  if [ ! -f "$source_file" ]; then
    echo "迁移文件不存在: $source_file" >&2
    exit 1
  fi
done

cat <<'SQL'
-- AiTok 上线 SQL：适用于空库及本项目历史版本升级。
-- 自动生成自 000 历史基线及后续编号迁移，不要单独编辑本文件。
-- 变更结构时必须同步维护 schema.sql 完整快照，并新增迁移后重新生成。
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
  # 003/004 的旧全量唯一索引已由 012 的活跃记录索引替代。
  # 重放时不能先重建旧索引，否则软删除后允许的同名记录会导致升级失败。
  sed '/^[[:space:]]*BEGIN;[[:space:]]*$/d; /^[[:space:]]*COMMIT;[[:space:]]*$/d' "$source_file" | awk '
    /^CREATE UNIQUE INDEX IF NOT EXISTS (addresses_location_unique|account_groups_user_name_unique|bank_card_ledger_subscription_idx|recharge_orders_cycle_unique|bank_card_ledger_cycle_unique)([[:space:]]|$)/ { obsolete = 1 }
    obsolete { if ($0 ~ /;[[:space:]]*$/) obsolete = 0; next }
    { print }
  '
done

printf '\nCOMMIT;\n'

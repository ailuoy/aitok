-- AiTok 上线 SQL：适用于空库及本项目历史版本升级。
-- 自动生成，请修改 schema.sql / 编号迁移后重新生成，不要单独编辑本文件。
-- 生成命令：bash scripts/build-release-sql.sh > backend/migrations/release.sql
-- 连接已创建的目标数据库后执行；不包含创建数据库、用户、授权或测试数据。
-- 所有结构调整在一个事务内执行，报错时回滚本次调整。
-- IF NOT EXISTS 支持重复执行，但不会修复同名对象的结构差异。

BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';

-- 来源：schema.sql
-- 平台用户；超级管理员由后端在首次成功登录时创建。
CREATE TABLE IF NOT EXISTS users (
  id BIGSERIAL PRIMARY KEY,
  email TEXT UNIQUE NOT NULL,
  password_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ChatGPT 账号；api_key 为旧版本兼容字段，新版本使用 session_ciphertext。
CREATE TABLE IF NOT EXISTS chatgpt_accounts (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  label TEXT NOT NULL,
  email TEXT NOT NULL,
  api_key TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 邮箱验证码；code 保存验证码哈希。
CREATE TABLE IF NOT EXISTS email_codes (
  email TEXT NOT NULL,
  purpose TEXT NOT NULL,
  code TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY(email, purpose)
);

-- 来源：001_wallet_and_renewals.sql
ALTER TABLE chatgpt_accounts ADD COLUMN IF NOT EXISTS session_ciphertext TEXT;
ALTER TABLE chatgpt_accounts ADD COLUMN IF NOT EXISTS renewal_date DATE;

CREATE TABLE IF NOT EXISTS wallets (
  user_id BIGINT PRIMARY KEY REFERENCES users(id),
  balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0)
);
CREATE TABLE IF NOT EXISTS topup_orders (
  order_no TEXT PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id),
  request_key TEXT NOT NULL,
  amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
  tokens BIGINT NOT NULL CHECK (tokens > 0),
  currency TEXT NOT NULL DEFAULT 'usd',
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','paid','expired','failed')),
  session_id TEXT UNIQUE,
  checkout_url TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(user_id, request_key)
);
CREATE TABLE IF NOT EXISTS wallet_ledger (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id),
  amount BIGINT NOT NULL,
  balance_after BIGINT NOT NULL CHECK (balance_after >= 0),
  kind TEXT NOT NULL,
  reference TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS account_renewals (
  user_id BIGINT NOT NULL REFERENCES users(id),
  request_key TEXT NOT NULL,
  account_id BIGINT NOT NULL,
  tokens BIGINT NOT NULL CHECK (tokens > 0),
  renewal_date DATE NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY(user_id, request_key)
);
CREATE TABLE IF NOT EXISTS renewal_date_audit (
  id BIGSERIAL PRIMARY KEY,
  account_id BIGINT NOT NULL,
  admin_id BIGINT NOT NULL REFERENCES users(id),
  previous_date DATE,
  renewal_date DATE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 来源：002_payment_quantity_and_history.sql
ALTER TABLE topup_orders ADD COLUMN IF NOT EXISTS quantity INTEGER NOT NULL DEFAULT 1 CHECK (quantity BETWEEN 1 AND 100);
ALTER TABLE topup_orders ADD COLUMN IF NOT EXISTS unit_amount_minor BIGINT;
ALTER TABLE topup_orders ADD COLUMN IF NOT EXISTS price_id TEXT;
ALTER TABLE account_renewals ADD COLUMN IF NOT EXISTS account_label TEXT;
ALTER TABLE account_renewals ADD COLUMN IF NOT EXISTS months INTEGER;

COMMIT;

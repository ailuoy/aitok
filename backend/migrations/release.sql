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

-- 来源：003_addresses.sql

-- 地址库独立于账号与账单；采集来源仅作为地址溯源信息。
CREATE TABLE IF NOT EXISTS addresses (
  id BIGSERIAL PRIMARY KEY,
  address_line1 TEXT NOT NULL CHECK (length(address_line1) BETWEEN 1 AND 200),
  address_line2 TEXT NOT NULL DEFAULT '' CHECK (length(address_line2) <= 200),
  city TEXT NOT NULL CHECK (length(city) BETWEEN 1 AND 100),
  state TEXT NOT NULL CHECK (length(state) BETWEEN 1 AND 100),
  postal_code TEXT NOT NULL CHECK (length(postal_code) BETWEEN 1 AND 20),
  country TEXT NOT NULL DEFAULT 'US' CHECK (country ~ '^[A-Z]{2}$'),
  source_url TEXT NOT NULL DEFAULT '',
  source_key TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS addresses_location_unique
  ON addresses (lower(address_line1), lower(address_line2), lower(city), lower(state), lower(postal_code), country);


-- 来源：004_account_groups_and_login.sql

CREATE TABLE IF NOT EXISTS account_groups (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS account_groups_user_name_unique ON account_groups(user_id, lower(name));
ALTER TABLE chatgpt_accounts ADD COLUMN IF NOT EXISTS group_id BIGINT REFERENCES account_groups(id) ON DELETE SET NULL;
ALTER TABLE chatgpt_accounts ADD COLUMN IF NOT EXISTS last_login_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS chatgpt_accounts_group_id_idx ON chatgpt_accounts(group_id);


-- 来源：005_bank_cards_and_address_owners.sql

ALTER TABLE addresses ADD COLUMN IF NOT EXISTS user_id BIGINT REFERENCES users(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS addresses_user_id_idx ON addresses(user_id);
CREATE TABLE IF NOT EXISTS bank_cards (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  label TEXT NOT NULL CHECK (length(label) BETWEEN 1 AND 80),
  cardholder TEXT NOT NULL CHECK (length(cardholder) BETWEEN 1 AND 120),
  number_ciphertext TEXT NOT NULL,
  number_fingerprint TEXT NOT NULL,
  last4 TEXT NOT NULL CHECK (last4 ~ '^[0-9]{4}$'),
  brand TEXT NOT NULL,
  exp_month INTEGER NOT NULL CHECK (exp_month BETWEEN 1 AND 12),
  exp_year INTEGER NOT NULL CHECK (exp_year BETWEEN 2000 AND 9999),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(user_id, number_fingerprint)
);


-- 来源：006_address_full_name.sql

-- 旧地址未采集姓名，保留空值供用户补充。
ALTER TABLE addresses ADD COLUMN IF NOT EXISTS full_name TEXT NOT NULL DEFAULT '' CHECK (length(full_name) <= 120);


-- 来源：007_address_source_data.sql

-- 保留生成器返回的完整资料，基础地址字段仍可独立编辑。
ALTER TABLE addresses ADD COLUMN IF NOT EXISTS source_data JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(source_data) = 'object');


-- 来源：008_bank_card_platform_and_notes.sql
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';

-- 旧银行卡保持空平台、空备注，不回填或改写已有付款信息。
ALTER TABLE bank_cards
  ADD COLUMN IF NOT EXISTS platform TEXT NOT NULL DEFAULT '' CHECK (length(platform) <= 80),
  ADD COLUMN IF NOT EXISTS notes TEXT NOT NULL DEFAULT '' CHECK (length(notes) <= 1000);


COMMIT;

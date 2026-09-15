-- 当前完整表结构快照（截至 012_soft_delete_timestamps）：12 张业务表。
-- 包含字段、默认值、约束、索引和表注释；不包含业务数据或环境凭据。
-- 可在空库独立初始化；已有数据库升级使用 release.sql / 新增编号迁移。
-- 每次表结构变更必须同步本文件，详见项目根目录 agent.md。

BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';

-- 平台用户：保存登录邮箱、密码哈希和用户角色；超级管理员使用后端配置及固定内部身份。
CREATE TABLE IF NOT EXISTS users (
  id BIGSERIAL PRIMARY KEY,
  email TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  role TEXT DEFAULT 'user' CHECK (role IN ('', 'user', 'admin')),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE users IS '平台用户：保存登录邮箱、密码哈希和用户角色；超级管理员使用后端配置及固定内部身份。';

-- 账号分组：按用户管理一批 ChatGPT 账号，组名在同一用户下忽略大小写唯一。
CREATE TABLE IF NOT EXISTS account_groups (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id),
  name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE account_groups IS '账号分组：按用户管理一批 ChatGPT 账号，组名在同一用户下忽略大小写唯一。';

-- ChatGPT 账号：记录所属用户、加密 Session、分组、续订日期和上次登录时间；api_key 为历史兼容字段。
CREATE TABLE IF NOT EXISTS chatgpt_accounts (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id),
  label TEXT NOT NULL,
  email TEXT NOT NULL,
  api_key TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  session_ciphertext TEXT,
  renewal_date DATE,
  group_id BIGINT REFERENCES account_groups(id),
  last_login_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE chatgpt_accounts IS 'ChatGPT 账号：记录所属用户、加密 Session、分组、续订日期和上次登录时间；api_key 为历史兼容字段。';

-- 邮箱验证码：按邮箱及用途保存验证码哈希和过期时间。
CREATE TABLE IF NOT EXISTS email_codes (
  id BIGSERIAL PRIMARY KEY,
  email TEXT NOT NULL,
  purpose TEXT NOT NULL,
  code TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE email_codes IS '邮箱验证码：按邮箱及用途保存验证码哈希和过期时间。';

-- 代币钱包：保存平台用户的代币余额，与银行卡 USD 资金账本独立。
CREATE TABLE IF NOT EXISTS wallets (
  user_id BIGINT PRIMARY KEY REFERENCES users(id),
  balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE wallets IS '代币钱包：保存平台用户的代币余额，与银行卡 USD 资金账本独立。';

-- 钱包充值订单：记录 Stripe 付款状态、USD 最小货币单位金额、代币数量、价格和幂等请求。
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
  quantity INTEGER NOT NULL DEFAULT 1 CHECK (quantity BETWEEN 1 AND 100),
  unit_amount_minor BIGINT,
  price_id TEXT,
  UNIQUE(user_id, request_key),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE topup_orders IS '钱包充值订单：记录 Stripe 付款状态、USD 最小货币单位金额、代币数量、价格和幂等请求。';

-- 代币钱包流水：记录代币收支、交易后余额及业务引用，保留历史充值和续订记录。
CREATE TABLE IF NOT EXISTS wallet_ledger (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id),
  amount BIGINT NOT NULL,
  balance_after BIGINT NOT NULL CHECK (balance_after >= 0),
  kind TEXT NOT NULL,
  reference TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE wallet_ledger IS '代币钱包流水：记录代币收支、交易后余额及业务引用，保留历史充值和续订记录。';

-- 历史账号续订记录：保存代币扣款、续订日期及账号快照；账号删除后仍保留。
CREATE TABLE IF NOT EXISTS account_renewals (
  user_id BIGINT NOT NULL REFERENCES users(id),
  request_key TEXT NOT NULL,
  account_id BIGINT NOT NULL,
  tokens BIGINT NOT NULL CHECK (tokens > 0),
  renewal_date DATE NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  account_label TEXT,
  months INTEGER,
  PRIMARY KEY(user_id, request_key),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE account_renewals IS '历史账号续订记录：保存代币扣款、续订日期及账号快照；账号删除后仍保留。';

-- 续订日期审计：记录管理员设置账号续订日期前后的值和操作时间。
CREATE TABLE IF NOT EXISTS renewal_date_audit (
  id BIGSERIAL PRIMARY KEY,
  account_id BIGINT NOT NULL,
  admin_id BIGINT NOT NULL REFERENCES users(id),
  previous_date DATE,
  renewal_date DATE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE renewal_date_audit IS '续订日期审计：记录管理员设置账号续订日期前后的值和操作时间。';

-- 账单地址库：保存姓名、地址及完整来源资料；无所属用户的记录为共享地址。
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
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  user_id BIGINT REFERENCES users(id),
  full_name TEXT NOT NULL DEFAULT '' CHECK (length(full_name) <= 120),
  source_data JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(source_data) = 'object'),
  deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE addresses IS '账单地址库：保存姓名、地址及完整来源资料；无所属用户的记录为共享地址。';

-- 银行卡：保存所属用户、加密卡号、卡平台、备注及 USD 美分记账余额；不保存安全码。
CREATE TABLE IF NOT EXISTS bank_cards (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id),
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
  platform TEXT NOT NULL DEFAULT '' CHECK (length(platform) <= 80),
  notes TEXT NOT NULL DEFAULT '' CHECK (length(notes) <= 1000),
  balance_usd_minor BIGINT NOT NULL DEFAULT 0 CHECK (balance_usd_minor BETWEEN 0 AND 1000000000000),
  deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE bank_cards IS '银行卡：保存所属用户、加密卡号、卡平台、备注及 USD 美分记账余额；不保存安全码。';

-- 银行卡 USD 对账流水：记录初始余额、存入、账号开通支出及交易后余额，保存账号和 PHP 原价快照，按请求和账号防重复扣款。
CREATE TABLE IF NOT EXISTS bank_card_ledger (
  id BIGSERIAL PRIMARY KEY,
  card_id BIGINT NOT NULL REFERENCES bank_cards(id) ON DELETE RESTRICT,
  actor_id BIGINT NOT NULL,
  request_key TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('opening', 'deposit', 'subscription')),
  amount_usd_minor BIGINT NOT NULL CHECK (amount_usd_minor <> 0 AND abs(amount_usd_minor) <= 1000000000000),
  balance_after_usd_minor BIGINT NOT NULL CHECK (balance_after_usd_minor BETWEEN 0 AND 1000000000000),
  account_id BIGINT,
  account_label TEXT NOT NULL DEFAULT '',
  account_email TEXT NOT NULL DEFAULT '',
  original_php_minor BIGINT,
  notes TEXT NOT NULL DEFAULT '' CHECK (length(notes) <= 1000),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (card_id, request_key),
  CHECK ((kind IN ('opening', 'deposit') AND amount_usd_minor > 0 AND account_id IS NULL AND original_php_minor IS NULL)
    OR (kind = 'subscription' AND amount_usd_minor < 0 AND account_id IS NOT NULL AND original_php_minor > 0)),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE bank_card_ledger IS '银行卡 USD 对账流水：记录初始余额、存入、账号开通支出及交易后余额，保存账号和 PHP 原价快照，按请求和账号防重复扣款。';

-- 主键和 UNIQUE 约束自动建立索引；以下为额外业务索引。
CREATE UNIQUE INDEX IF NOT EXISTS addresses_location_active_unique ON addresses(lower(address_line1),lower(address_line2),lower(city),lower(state),lower(postal_code),country) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS account_groups_user_name_active_unique ON account_groups(user_id,lower(name)) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS chatgpt_accounts_group_id_idx ON chatgpt_accounts(group_id);
CREATE INDEX IF NOT EXISTS addresses_user_id_idx ON addresses(user_id);
CREATE INDEX IF NOT EXISTS bank_card_ledger_history_idx ON bank_card_ledger(card_id, id DESC);
CREATE UNIQUE INDEX IF NOT EXISTS bank_card_ledger_opening_idx ON bank_card_ledger(card_id) WHERE kind = 'opening';
CREATE UNIQUE INDEX IF NOT EXISTS bank_card_ledger_subscription_idx ON bank_card_ledger(account_id) WHERE kind = 'subscription';

CREATE UNIQUE INDEX IF NOT EXISTS users_email_active_unique ON users(email) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS email_codes_active_unique ON email_codes(email,purpose) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS bank_cards_fingerprint_active_unique ON bank_cards(user_id,number_fingerprint) WHERE deleted_at IS NULL;

-- 所有表统一维护更新时间，并保留首次创建时间。
CREATE OR REPLACE FUNCTION aitok_touch_timestamps() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  NEW.created_at := OLD.created_at;
  NEW.updated_at := statement_timestamp();
  RETURN NEW;
END;
$$;

-- 业务表禁止物理删除和清空；删除必须写 deleted_at。
CREATE OR REPLACE FUNCTION aitok_prevent_hard_delete() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'Physical DELETE/TRUNCATE is forbidden for %, use deleted_at', TG_TABLE_NAME USING ERRCODE = '23514';
END;
$$;

CREATE OR REPLACE TRIGGER users_touch_timestamps BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER users_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON users FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER account_groups_touch_timestamps BEFORE UPDATE ON account_groups FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER account_groups_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON account_groups FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER chatgpt_accounts_touch_timestamps BEFORE UPDATE ON chatgpt_accounts FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER chatgpt_accounts_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON chatgpt_accounts FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER email_codes_touch_timestamps BEFORE UPDATE ON email_codes FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER email_codes_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON email_codes FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER wallets_touch_timestamps BEFORE UPDATE ON wallets FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER wallets_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON wallets FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER topup_orders_touch_timestamps BEFORE UPDATE ON topup_orders FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER topup_orders_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON topup_orders FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER wallet_ledger_touch_timestamps BEFORE UPDATE ON wallet_ledger FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER wallet_ledger_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON wallet_ledger FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER account_renewals_touch_timestamps BEFORE UPDATE ON account_renewals FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER account_renewals_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON account_renewals FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER renewal_date_audit_touch_timestamps BEFORE UPDATE ON renewal_date_audit FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER renewal_date_audit_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON renewal_date_audit FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER addresses_touch_timestamps BEFORE UPDATE ON addresses FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER addresses_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON addresses FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER bank_cards_touch_timestamps BEFORE UPDATE ON bank_cards FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER bank_cards_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON bank_cards FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER bank_card_ledger_touch_timestamps BEFORE UPDATE ON bank_card_ledger FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER bank_card_ledger_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON bank_card_ledger FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();

COMMIT;

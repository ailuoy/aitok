-- 当前完整表结构快照（截至 018_admin_two_factor）：21 张业务表。
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
  deleted_at TIMESTAMPTZ,
  session_version BIGINT NOT NULL DEFAULT 0,
  disabled BOOLEAN NOT NULL DEFAULT FALSE,
  permissions TEXT[],
  totp_ciphertext TEXT,
  totp_pending_ciphertext TEXT,
  totp_pending_expires_at TIMESTAMPTZ,
  totp_enabled_at TIMESTAMPTZ,
  totp_last_step BIGINT NOT NULL DEFAULT -1
);
COMMENT ON TABLE users IS '系统用户与角色；管理员验证器密钥加密保存，待绑定密钥限时确认，TOTP 时间步防重放；空角色按普通用户处理';

-- 账号分组：按用户管理一批 ChatGPT 账号，组名在同一用户下忽略大小写唯一。
CREATE TABLE IF NOT EXISTS account_groups (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL,
  name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE account_groups IS '账号分组：按用户管理一批 ChatGPT 账号，组名在同一用户下忽略大小写唯一。';

-- ChatGPT 账号：记录所属用户、加密 Session、分组、续订日期和上次登录时间；api_key 为历史兼容字段。
CREATE TABLE IF NOT EXISTS chatgpt_accounts (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL,
  label TEXT NOT NULL,
  email TEXT NOT NULL,
  api_key TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  session_ciphertext TEXT,
  renewal_date DATE,
  group_id BIGINT,
  last_login_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ,
  verified_plan TEXT NOT NULL DEFAULT '',
  verified_at TIMESTAMPTZ,
  subscription_ends_at DATE,
  renewal_enabled BOOLEAN NOT NULL DEFAULT TRUE
);
COMMENT ON TABLE chatgpt_accounts IS 'ChatGPT 账号：所属用户、加密 Session、分组、上次登录、人工续订日期及有订单凭据的订阅核验状态。';

-- 邮箱验证码：按邮箱及用途保存验证码哈希和过期时间。
CREATE TABLE IF NOT EXISTS email_codes (
  id BIGSERIAL PRIMARY KEY,
  email TEXT NOT NULL,
  purpose TEXT NOT NULL,
  code TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ,
  attempts INTEGER NOT NULL DEFAULT 0
);
COMMENT ON TABLE email_codes IS '邮箱验证码：按邮箱及用途保存验证码哈希和过期时间。';

-- 代币钱包：保存平台用户的代币余额，与银行卡 USD 资金账本独立。
CREATE TABLE IF NOT EXISTS wallets (
  user_id BIGINT PRIMARY KEY,
  balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE wallets IS '代币钱包：保存平台用户的代币余额，与银行卡 USD 资金账本独立。';

-- 钱包充值订单：记录 Stripe 付款状态、USD 最小货币单位金额、代币数量、价格和幂等请求。
CREATE TABLE IF NOT EXISTS topup_orders (
  order_no TEXT PRIMARY KEY,
  user_id BIGINT NOT NULL,
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
  deleted_at TIMESTAMPTZ,
  payment_intent TEXT NOT NULL DEFAULT '',
  refunded_minor BIGINT NOT NULL DEFAULT 0 CHECK (refunded_minor>=0),
  reversed_tokens BIGINT NOT NULL DEFAULT 0 CHECK (reversed_tokens>=0),
  dispute_status TEXT NOT NULL DEFAULT ''
);
COMMENT ON TABLE topup_orders IS '钱包充值订单：记录 Stripe 付款状态、USD 最小货币单位金额、代币数量、价格和幂等请求。';

-- 代币钱包流水：记录代币收支、交易后余额及业务引用，保留历史充值和续订记录。
CREATE TABLE IF NOT EXISTS wallet_ledger (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL,
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
  user_id BIGINT NOT NULL,
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
  admin_id BIGINT NOT NULL,
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
  user_id BIGINT,
  full_name TEXT NOT NULL DEFAULT '' CHECK (length(full_name) <= 120),
  source_data JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(source_data) = 'object'),
  deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE addresses IS '账单地址库：保存姓名、地址及完整来源资料；无所属用户的记录为共享地址。';

-- 银行卡：保存所属用户、加密卡号、卡平台、备注及 USD 美分记账余额；不保存安全码。
CREATE TABLE IF NOT EXISTS bank_cards (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL,
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
  deleted_at TIMESTAMPTZ,
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','frozen','invalid')),
  daily_limit_usd_minor BIGINT NOT NULL DEFAULT 0 CHECK (daily_limit_usd_minor>=0),
  low_balance_usd_minor BIGINT NOT NULL DEFAULT 0 CHECK (low_balance_usd_minor>=0),
  reserved_usd_minor BIGINT NOT NULL DEFAULT 0 CHECK (reserved_usd_minor>=0)
);
COMMENT ON TABLE bank_cards IS '银行卡：加密卡号、平台、备注、USD 美分余额、冻结金额、可用状态和限额；不保存安全码。';

-- 银行卡 USD 对账流水：记录初始余额、存入、账号开通支出及交易后余额，保存账号和 PHP 原价快照，按请求和账号防重复扣款。
CREATE TABLE IF NOT EXISTS bank_card_ledger (
  id BIGSERIAL PRIMARY KEY,
  card_id BIGINT NOT NULL,
  actor_id BIGINT NOT NULL,
  request_key TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('opening','deposit','subscription','refund','reversal','fee','adjustment')),
  amount_usd_minor BIGINT NOT NULL CHECK (amount_usd_minor <> 0 AND abs(amount_usd_minor) <= 1000000000000),
  balance_after_usd_minor BIGINT NOT NULL CHECK (balance_after_usd_minor BETWEEN 0 AND 1000000000000),
  account_id BIGINT,
  account_label TEXT NOT NULL DEFAULT '',
  account_email TEXT NOT NULL DEFAULT '',
  original_php_minor BIGINT,
  notes TEXT NOT NULL DEFAULT '' CHECK (length(notes) <= 4000),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (card_id, request_key),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ,
  order_id BIGINT,
  reversed_at TIMESTAMPTZ,
  reference_id BIGINT,
  external_reference TEXT NOT NULL DEFAULT '',
  period_start DATE,
  period_end DATE,
  currency TEXT NOT NULL DEFAULT 'USD',
  original_amount_minor BIGINT,
  CONSTRAINT bank_card_ledger_sign_check CHECK ((kind IN ('opening','deposit','refund') AND amount_usd_minor>0) OR (kind IN ('subscription','fee') AND amount_usd_minor<0) OR kind IN ('reversal','adjustment'))
);
COMMENT ON TABLE bank_card_ledger IS '银行卡 USD 资金流水：初始余额、存入、周期购买、退款、冲正及费用，记录实际原币价格和历史交易号，冲正不覆盖原流水。';

-- 主键和 UNIQUE 约束自动建立索引；以下为额外业务索引。
CREATE UNIQUE INDEX IF NOT EXISTS addresses_location_active_unique ON addresses(lower(address_line1),lower(address_line2),lower(city),lower(state),lower(postal_code),country) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS account_groups_user_name_active_unique ON account_groups(user_id,lower(name)) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS chatgpt_accounts_group_id_idx ON chatgpt_accounts(group_id);
CREATE INDEX IF NOT EXISTS addresses_user_id_idx ON addresses(user_id);
CREATE INDEX IF NOT EXISTS bank_card_ledger_history_idx ON bank_card_ledger(card_id, id DESC);
CREATE UNIQUE INDEX IF NOT EXISTS bank_card_ledger_opening_idx ON bank_card_ledger(card_id) WHERE kind = 'opening';

CREATE UNIQUE INDEX IF NOT EXISTS users_email_active_unique ON users(email) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS email_codes_active_unique ON email_codes(email,purpose) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS bank_cards_fingerprint_active_unique ON bank_cards(user_id,number_fingerprint) WHERE deleted_at IS NULL;

-- 时间字段由应用层维护：INSERT 使用默认时间，UPDATE 同时写 updated_at，软删除同时写 deleted_at。

-- 充值业务及认证扩展。
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';


CREATE TABLE IF NOT EXISTS recharge_packages (
    auto_usd BOOLEAN NOT NULL DEFAULT FALSE,
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
  plan TEXT NOT NULL CHECK (plan IN ('plus','pro_5x','pro_20x')),
  region TEXT NOT NULL CHECK (length(region) BETWEEN 2 AND 80),
  currency TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
  original_amount_minor BIGINT NOT NULL CHECK (original_amount_minor>0),
  sale_usd_minor BIGINT NOT NULL CHECK (sale_usd_minor>0),
  wallet_tokens BIGINT NOT NULL DEFAULT 0 CHECK (wallet_tokens>=0),
  months INTEGER NOT NULL CHECK (months BETWEEN 1 AND 36),
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  notes TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE recharge_packages IS '充值套餐：保存地区、原币价格、USD 售价、周期及可选钱包代币价格；auto_usd 套餐按最新 PHP/USD 汇率计算，订单保存购买时价格及汇率快照。';

CREATE TABLE IF NOT EXISTS recharge_orders (
  id BIGSERIAL PRIMARY KEY,
  order_no TEXT NOT NULL UNIQUE,
  user_id BIGINT NOT NULL,
  account_id BIGINT NOT NULL,
  account_email TEXT NOT NULL,
  package_id BIGINT NOT NULL,
  package_snapshot JSONB NOT NULL,
  period_start DATE NOT NULL,
  period_end DATE NOT NULL CHECK (period_end>period_start),
  sale_usd_minor BIGINT NOT NULL CHECK (sale_usd_minor>0),
  wallet_tokens BIGINT NOT NULL DEFAULT 0,
  payment_method TEXT NOT NULL DEFAULT '',
  payment_status TEXT NOT NULL DEFAULT 'unpaid' CHECK (payment_status IN ('unpaid','paid','partial_refund','refunded')),
  fulfillment_status TEXT NOT NULL DEFAULT 'pending' CHECK (fulfillment_status IN ('pending','processing','verifying','completed','failed','cancelled')),
  payment_reference TEXT NOT NULL DEFAULT '',
  purchase_reference TEXT NOT NULL DEFAULT '',
  card_id BIGINT,
  cost_usd_minor BIGINT NOT NULL DEFAULT 0,
  refunded_usd_minor BIGINT NOT NULL DEFAULT 0 CHECK (refunded_usd_minor>=0 AND refunded_usd_minor<=sale_usd_minor),
  refunded_tokens BIGINT NOT NULL DEFAULT 0,
  assignee_id BIGINT,
  evidence TEXT NOT NULL DEFAULT '',
  failure_reason TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT '',
  request_key TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 0,
  verified_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ,
  UNIQUE(user_id,request_key)
);
CREATE UNIQUE INDEX IF NOT EXISTS recharge_orders_cycle_unique ON recharge_orders(user_id,account_email,period_start,period_end) WHERE fulfillment_status<>'cancelled';
CREATE UNIQUE INDEX IF NOT EXISTS recharge_orders_payment_reference_unique ON recharge_orders(payment_reference) WHERE payment_reference<>'';
CREATE UNIQUE INDEX IF NOT EXISTS recharge_orders_purchase_reference_unique ON recharge_orders(purchase_reference) WHERE purchase_reference<>'';
CREATE INDEX IF NOT EXISTS recharge_orders_owner_idx ON recharge_orders(user_id,id DESC);
COMMENT ON TABLE recharge_orders IS 'GPT 充值订单：客户收款、官网扣款与开通核验分别记录，按账号邮箱及周期防重，金额为 USD 美分。';

CREATE TABLE IF NOT EXISTS operation_events (
  id BIGSERIAL PRIMARY KEY,
  actor_id BIGINT NOT NULL,
  entity_type TEXT NOT NULL,
  entity_id BIGINT NOT NULL,
  action TEXT NOT NULL,
  request_key TEXT NOT NULL,
  before_data JSONB NOT NULL DEFAULT '{}'::jsonb,
  after_data JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ,
  UNIQUE(entity_type,entity_id,request_key)
);
CREATE INDEX IF NOT EXISTS operation_events_history_idx ON operation_events(entity_type,entity_id,id DESC);
COMMENT ON TABLE operation_events IS '操作审计：记录操作者、操作及非敏感前后快照，同时保存订单操作幂等结果，不记录卡号、密码和 Session。';

CREATE TABLE IF NOT EXISTS auth_limits (
  key TEXT PRIMARY KEY,
  count INTEGER NOT NULL DEFAULT 0,
  window_start TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE auth_limits IS '认证限流：按不可逆摘要保存邮箱或来源的窗口计数，多进程共享，不保存明文凭据。';

CREATE UNIQUE INDEX IF NOT EXISTS bank_card_ledger_legacy_subscription_idx ON bank_card_ledger(account_id) WHERE kind='subscription' AND order_id IS NULL AND period_start IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS bank_card_ledger_order_unique ON bank_card_ledger(order_id) WHERE kind='subscription' AND order_id IS NOT NULL AND reversed_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS bank_card_ledger_cycle_unique ON bank_card_ledger(account_email,period_start,period_end) WHERE kind='subscription' AND period_start IS NOT NULL AND reversed_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS bank_card_ledger_reference_unique ON bank_card_ledger(external_reference) WHERE external_reference<>'';
CREATE UNIQUE INDEX IF NOT EXISTS bank_card_ledger_reversal_unique ON bank_card_ledger(reference_id) WHERE kind='reversal';

CREATE TABLE IF NOT EXISTS card_holds (
  id BIGSERIAL PRIMARY KEY,
  card_id BIGINT NOT NULL,
  amount_usd_minor BIGINT NOT NULL CHECK (amount_usd_minor>0),
  reference TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'held' CHECK (status IN ('held','released','settled')),
  actor_id BIGINT NOT NULL,
  notes TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ,
  UNIQUE(card_id,reference)
);
COMMENT ON TABLE card_holds IS '卡片预授权：记录冻结、释放和结算；冻结占用可用余额，不计为已结算消费。';

CREATE TABLE IF NOT EXISTS card_statement_rows (
  id BIGSERIAL PRIMARY KEY,
  card_id BIGINT NOT NULL,
  external_reference TEXT NOT NULL,
  amount_usd_minor BIGINT NOT NULL CHECK (amount_usd_minor<>0),
  occurred_at TIMESTAMPTZ NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  resolution TEXT NOT NULL DEFAULT '',
  actor_id BIGINT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ,
  UNIQUE(card_id,external_reference)
);
COMMENT ON TABLE card_statement_rows IS '卡平台实际账单：按交易号去重导入，与系统流水逐笔比较；差异处理保留审计。';

CREATE TABLE IF NOT EXISTS proxy_activity (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL,
  device_id TEXT NOT NULL,
  event_id TEXT NOT NULL,
  data JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ,
  UNIQUE(user_id,device_id,event_id)
);
COMMENT ON TABLE proxy_activity IS '本机代理操作同步：按用户设备去重保存使用事件，不上传代理密码或浏览器 Session。';

CREATE TABLE IF NOT EXISTS payment_exceptions (
  id BIGSERIAL PRIMARY KEY,
  event_id TEXT NOT NULL UNIQUE,
  order_no TEXT NOT NULL,
  kind TEXT NOT NULL,
  amount_minor BIGINT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  detail TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE payment_exceptions IS '支付异常：记录 Stripe 退款、拒付与钱包余额不足待处理事项，保留事件幂等及处理状态。';

COMMIT;

CREATE TABLE IF NOT EXISTS exchange_rates (
    id BIGSERIAL PRIMARY KEY,
    base_currency TEXT NOT NULL CHECK (base_currency = 'PHP'),
    quote_currency TEXT NOT NULL CHECK (quote_currency IN ('USD','CNY')),
    rate NUMERIC(20,12) NOT NULL CONSTRAINT exchange_rates_rate_check CHECK (
    (quote_currency='USD' AND rate >= 0.001 AND rate <= 0.1)
    OR (quote_currency='CNY' AND rate >= 0.01 AND rate <= 1)
),
    source TEXT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS exchange_rates_latest_idx ON exchange_rates(base_currency,quote_currency,created_at DESC,id DESC) WHERE deleted_at IS NULL;
COMMENT ON TABLE exchange_rates IS '每日汇率同步历史：rate 表示 1 PHP 折合 quote_currency（USD 或 CNY）的金额；同批币种在一个事务内写入并共享生效与同步时间，保留来源及软删除历史，供套餐折算与订单快照追溯。';

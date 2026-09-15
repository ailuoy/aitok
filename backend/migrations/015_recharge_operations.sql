BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';

ALTER TABLE users ADD COLUMN IF NOT EXISTS session_version BIGINT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS disabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS permissions TEXT[];
ALTER TABLE email_codes ADD COLUMN IF NOT EXISTS attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE chatgpt_accounts ADD COLUMN IF NOT EXISTS verified_plan TEXT NOT NULL DEFAULT '';
ALTER TABLE chatgpt_accounts ADD COLUMN IF NOT EXISTS verified_at TIMESTAMPTZ;
ALTER TABLE chatgpt_accounts ADD COLUMN IF NOT EXISTS subscription_ends_at DATE;
ALTER TABLE chatgpt_accounts ADD COLUMN IF NOT EXISTS renewal_enabled BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE bank_cards ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','frozen','invalid'));
ALTER TABLE bank_cards ADD COLUMN IF NOT EXISTS daily_limit_usd_minor BIGINT NOT NULL DEFAULT 0 CHECK (daily_limit_usd_minor>=0);
ALTER TABLE bank_cards ADD COLUMN IF NOT EXISTS low_balance_usd_minor BIGINT NOT NULL DEFAULT 0 CHECK (low_balance_usd_minor>=0);
ALTER TABLE bank_cards ADD COLUMN IF NOT EXISTS reserved_usd_minor BIGINT NOT NULL DEFAULT 0 CHECK (reserved_usd_minor>=0);
ALTER TABLE topup_orders ADD COLUMN IF NOT EXISTS payment_intent TEXT NOT NULL DEFAULT '';
ALTER TABLE topup_orders ADD COLUMN IF NOT EXISTS refunded_minor BIGINT NOT NULL DEFAULT 0 CHECK (refunded_minor>=0);
ALTER TABLE topup_orders ADD COLUMN IF NOT EXISTS reversed_tokens BIGINT NOT NULL DEFAULT 0 CHECK (reversed_tokens>=0);
ALTER TABLE topup_orders ADD COLUMN IF NOT EXISTS dispute_status TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS recharge_packages (
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
COMMENT ON TABLE recharge_packages IS '充值套餐：保存地区、原币价格、USD 售价、周期及可选钱包代币价格；订单保存购买时快照。';

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

ALTER TABLE bank_card_ledger ADD COLUMN IF NOT EXISTS order_id BIGINT;
ALTER TABLE bank_card_ledger ADD COLUMN IF NOT EXISTS reversed_at TIMESTAMPTZ;
ALTER TABLE bank_card_ledger ADD COLUMN IF NOT EXISTS reference_id BIGINT;
ALTER TABLE bank_card_ledger ADD COLUMN IF NOT EXISTS external_reference TEXT NOT NULL DEFAULT '';
ALTER TABLE bank_card_ledger ADD COLUMN IF NOT EXISTS period_start DATE;
ALTER TABLE bank_card_ledger ADD COLUMN IF NOT EXISTS period_end DATE;
ALTER TABLE bank_card_ledger ADD COLUMN IF NOT EXISTS currency TEXT NOT NULL DEFAULT 'USD';
ALTER TABLE bank_card_ledger ADD COLUMN IF NOT EXISTS original_amount_minor BIGINT;
ALTER TABLE bank_card_ledger DROP CONSTRAINT IF EXISTS bank_card_ledger_notes_check;
ALTER TABLE bank_card_ledger ADD CONSTRAINT bank_card_ledger_notes_check CHECK (length(notes)<=4000);
ALTER TABLE bank_card_ledger DROP CONSTRAINT IF EXISTS bank_card_ledger_kind_check;
ALTER TABLE bank_card_ledger DROP CONSTRAINT IF EXISTS bank_card_ledger_check;
ALTER TABLE bank_card_ledger ADD CONSTRAINT bank_card_ledger_kind_check CHECK (kind IN ('opening','deposit','subscription','refund','reversal','fee','adjustment'));
ALTER TABLE bank_card_ledger DROP CONSTRAINT IF EXISTS bank_card_ledger_sign_check;
ALTER TABLE bank_card_ledger ADD CONSTRAINT bank_card_ledger_sign_check CHECK ((kind IN ('opening','deposit','refund') AND amount_usd_minor>0) OR (kind IN ('subscription','fee') AND amount_usd_minor<0) OR kind IN ('reversal','adjustment'));
DROP INDEX IF EXISTS bank_card_ledger_subscription_idx;
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

COMMENT ON TABLE bank_card_ledger IS '银行卡 USD 资金流水：初始余额、存入、周期购买、退款、冲正及费用，记录实际原币价格和历史交易号，冲正不覆盖原流水。';
COMMENT ON TABLE bank_cards IS '银行卡：加密卡号、平台、备注、USD 美分余额、冻结金额、可用状态和限额；不保存安全码。';
COMMENT ON TABLE chatgpt_accounts IS 'ChatGPT 账号：所属用户、加密 Session、分组、上次登录、人工续订日期及有订单凭据的订阅核验状态。';
COMMIT;

-- 保留历史整数代币单位，仅补充百分之一代币余数；不改变既有余额或自动补扣。
ALTER TABLE wallets ADD COLUMN IF NOT EXISTS balance_subunit SMALLINT NOT NULL DEFAULT 0 CHECK (balance_subunit BETWEEN 0 AND 99);
ALTER TABLE wallet_ledger ADD COLUMN IF NOT EXISTS amount_subunit SMALLINT NOT NULL DEFAULT 0 CHECK (amount_subunit BETWEEN -99 AND 99);
ALTER TABLE wallet_ledger ADD COLUMN IF NOT EXISTS balance_after_subunit SMALLINT NOT NULL DEFAULT 0 CHECK (balance_after_subunit BETWEEN 0 AND 99);

CREATE TABLE IF NOT EXISTS order_wallet_debits (
  id BIGSERIAL PRIMARY KEY,
  order_id BIGINT NOT NULL UNIQUE,
  user_id BIGINT NOT NULL,
  account_id BIGINT NOT NULL,
  account_email TEXT NOT NULL,
  account_label TEXT NOT NULL,
  order_no TEXT NOT NULL,
  amount_usd_minor BIGINT NOT NULL CHECK (amount_usd_minor > 0),
  tokens_minor BIGINT NOT NULL CHECK (tokens_minor > 0),
  tokens_per_usd BIGINT NOT NULL CHECK (tokens_per_usd > 0),
  balance_after_minor BIGINT NOT NULL CHECK (balance_after_minor >= 0),
  wallet_ledger_id BIGINT NOT NULL UNIQUE,
  actor_id BIGINT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE order_wallet_debits IS '账号消费订单：按实收美元快照扣除当前绑定用户的钱包，金额单位为美分及百分之一代币，保留付款人和账号快照；每订单全历史仅扣款一次。';
CREATE INDEX IF NOT EXISTS order_wallet_debits_user_idx ON order_wallet_debits(user_id,id DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS order_wallet_debits_account_idx ON order_wallet_debits(account_id,user_id) WHERE deleted_at IS NULL;

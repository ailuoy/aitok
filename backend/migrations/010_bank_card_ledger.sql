BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';

-- USD 使用整数美分；历史卡片从零开始，不推算已有余额。
ALTER TABLE bank_cards ADD COLUMN IF NOT EXISTS balance_usd_minor BIGINT NOT NULL DEFAULT 0 CHECK (balance_usd_minor BETWEEN 0 AND 1000000000000);
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
    OR (kind = 'subscription' AND amount_usd_minor < 0 AND account_id IS NOT NULL AND original_php_minor > 0))
);
CREATE INDEX IF NOT EXISTS bank_card_ledger_history_idx ON bank_card_ledger(card_id, id DESC);
CREATE UNIQUE INDEX IF NOT EXISTS bank_card_ledger_opening_idx ON bank_card_ledger(card_id) WHERE kind = 'opening';
-- 一次账号开通只记一笔，跨银行卡同样不能重复扣款。
CREATE UNIQUE INDEX IF NOT EXISTS bank_card_ledger_subscription_idx ON bank_card_ledger(account_id) WHERE kind = 'subscription';

COMMIT;

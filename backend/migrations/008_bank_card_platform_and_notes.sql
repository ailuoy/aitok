BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';

-- 旧银行卡保持空平台、空备注，不回填或改写已有付款信息。
ALTER TABLE bank_cards
  ADD COLUMN IF NOT EXISTS platform TEXT NOT NULL DEFAULT '' CHECK (length(platform) <= 80),
  ADD COLUMN IF NOT EXISTS notes TEXT NOT NULL DEFAULT '' CHECK (length(notes) <= 1000);

COMMIT;

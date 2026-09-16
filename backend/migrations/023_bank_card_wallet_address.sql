BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';

-- 钱包地址用于管理员转账时查看和复制，不自动发起任何资金操作。
ALTER TABLE bank_cards
  ADD COLUMN IF NOT EXISTS wallet_address TEXT NOT NULL DEFAULT '' CHECK (char_length(wallet_address)<=200);
COMMENT ON TABLE bank_cards IS '银行卡：加密卡号、平台、备注、转账用钱包地址、USD 美分余额、冻结金额、可用状态和限额；钱包地址仅用于展示和复制，不自动转账；不保存安全码。';
COMMIT;

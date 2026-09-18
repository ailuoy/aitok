BEGIN;

ALTER TABLE bank_cards DROP CONSTRAINT IF EXISTS bank_cards_balance_usd_minor_check;
ALTER TABLE bank_cards ADD CONSTRAINT bank_cards_balance_usd_minor_check CHECK (balance_usd_minor BETWEEN -1000000000000 AND 1000000000000);
ALTER TABLE bank_card_ledger DROP CONSTRAINT IF EXISTS bank_card_ledger_balance_after_usd_minor_check;
ALTER TABLE bank_card_ledger ADD CONSTRAINT bank_card_ledger_balance_after_usd_minor_check CHECK (balance_after_usd_minor BETWEEN -1000000000000 AND 1000000000000);
ALTER TABLE bank_card_ledger ADD COLUMN IF NOT EXISTS pricing_snapshot JSONB;
ALTER TABLE bank_card_ledger DROP CONSTRAINT IF EXISTS bank_card_ledger_pricing_snapshot_check;
ALTER TABLE bank_card_ledger ADD CONSTRAINT bank_card_ledger_pricing_snapshot_check CHECK (pricing_snapshot IS NULL OR jsonb_typeof(pricing_snapshot)='object');
COMMENT ON TABLE bank_card_ledger IS '银行卡 USD 资金流水：初始余额、存入、周期购买、退款、冲正及费用，记录原币价格、交易号及历史补录的套餐与扣款汇率快照；补录允许负余额，冲正不覆盖原流水。';

COMMIT;

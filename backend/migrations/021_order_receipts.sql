BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';

-- 历史收款币种及金额不可推算，保持未记录；新线下收款由应用一次写入快照。
ALTER TABLE recharge_orders
  ADD COLUMN IF NOT EXISTS received_currency TEXT NOT NULL DEFAULT '' CHECK (received_currency IN ('','CNY','USD')),
  ADD COLUMN IF NOT EXISTS received_amount_minor BIGINT NOT NULL DEFAULT 0 CHECK (received_amount_minor>=0),
  ADD COLUMN IF NOT EXISTS received_usd_minor BIGINT NOT NULL DEFAULT 0 CHECK (received_usd_minor>=0),
  ADD COLUMN IF NOT EXISTS received_exchange_rate JSONB,
  ADD COLUMN IF NOT EXISTS received_at TIMESTAMPTZ;
ALTER TABLE recharge_orders DROP CONSTRAINT IF EXISTS recharge_orders_receipt_complete;
ALTER TABLE recharge_orders ADD CONSTRAINT recharge_orders_receipt_complete CHECK (
  (received_currency='' AND received_amount_minor=0 AND received_usd_minor=0 AND received_exchange_rate IS NULL AND received_at IS NULL)
  OR (received_currency IN ('CNY','USD') AND received_amount_minor>0 AND received_usd_minor>0 AND received_exchange_rate IS NOT NULL AND jsonb_typeof(received_exchange_rate)='object' AND received_at IS NOT NULL)
);
COMMENT ON TABLE recharge_orders IS 'GPT 充值订单：保存下单 SKU 和汇率快照、CNY/USD 实收及收款汇率快照；毛利按实收减成本计算，历史未知实收不推算；退款或废弃释放周期且禁止继续操作，金额均为对应币种的分。';
COMMIT;

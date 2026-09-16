BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';

ALTER TABLE recharge_orders ADD COLUMN IF NOT EXISTS order_status TEXT NOT NULL DEFAULT 'active'
  CHECK (order_status IN ('active','refunded','discarded'));

DROP INDEX IF EXISTS recharge_orders_cycle_unique;
CREATE UNIQUE INDEX recharge_orders_cycle_unique ON recharge_orders(user_id,account_email,period_start,period_end)
  WHERE order_status='active' AND payment_status<>'refunded' AND fulfillment_status<>'cancelled';

-- 关联订单的扣款由订单唯一索引及应用层周期锁防重；结束订单允许新的订单重新记账。
DROP INDEX IF EXISTS bank_card_ledger_cycle_unique;
CREATE UNIQUE INDEX bank_card_ledger_cycle_unique ON bank_card_ledger(account_email,period_start,period_end)
  WHERE kind='subscription' AND order_id IS NULL AND period_start IS NOT NULL AND reversed_at IS NULL;

COMMENT ON TABLE recharge_orders IS 'GPT 充值订单：保存下单 SKU 价格及汇率快照；正常、已退款、已废弃状态独立于资金记录，结束订单释放周期且禁止继续操作；金额为 USD 美分。';
COMMIT;

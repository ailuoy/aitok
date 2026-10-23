BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';

-- 实际交易可对应重叠周期的独立订单；请求键、订单扣款和交易号唯一约束保持不变。
DROP INDEX IF EXISTS recharge_orders_cycle_unique;
CREATE INDEX IF NOT EXISTS recharge_orders_cycle_idx ON recharge_orders(user_id,account_email,period_start,period_end)
  WHERE order_status='active' AND payment_status<>'refunded' AND fulfillment_status<>'cancelled';

COMMENT ON TABLE recharge_orders IS 'GPT 充值订单：保存订单来源、下单 SKU 和汇率快照、CNY/USD 实收及收款汇率快照；录入及补录允许独立订单周期重叠，请求键、订单扣款和交易号继续防重；来源从未删除订单汇总，毛利按实收减成本计算，历史未知实收不推算；退款或废弃后禁止继续操作，金额均为对应币种的分。';
COMMIT;

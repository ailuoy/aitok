BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';

-- 来源随订单保存；已有记录保持未填写，不推算或回填业务来源。
ALTER TABLE recharge_orders
  ADD COLUMN IF NOT EXISTS order_source TEXT NOT NULL DEFAULT '' CHECK (char_length(order_source)<=80);
COMMENT ON TABLE recharge_orders IS 'GPT 充值订单：保存订单来源、下单 SKU 和汇率快照、CNY/USD 实收及收款汇率快照；来源可自定义并从未删除订单汇总为下拉选项；毛利按实收减成本计算，历史未知实收不推算；退款或废弃释放周期且禁止继续操作，金额均为对应币种的分。';
COMMIT;

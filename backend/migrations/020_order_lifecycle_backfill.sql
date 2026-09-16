BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';

-- 仅归类历史订单状态，保留订单号、金额、余额、流水及原操作记录；重复执行不改已结束订单。
UPDATE recharge_orders o
SET order_status=CASE
    WHEN payment_status='refunded' OR EXISTS (
      SELECT 1 FROM operation_events e WHERE e.entity_type='order' AND e.entity_id=o.id
        AND e.action='refund_note' AND e.deleted_at IS NULL
    ) THEN 'refunded'
    ELSE 'discarded' END,
    version=version+1, updated_at=NOW()
WHERE o.order_status='active' AND o.deleted_at IS NULL AND (
  payment_status='refunded' OR fulfillment_status='cancelled' OR EXISTS (
    SELECT 1 FROM operation_events e WHERE e.entity_type='order' AND e.entity_id=o.id
      AND e.action='refund_note' AND e.deleted_at IS NULL
  )
);
COMMIT;

BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';

-- 为所有存量行（含软删除历史）自动分配 ID；重放时保留已有 ID 和序列。
-- 原业务主键改为全量唯一索引，保留业务防重及 ON CONFLICT 的行为。
ALTER TABLE wallets ADD COLUMN IF NOT EXISTS id BIGSERIAL;
CREATE UNIQUE INDEX IF NOT EXISTS wallets_user_id_key ON wallets(user_id);
ALTER TABLE wallets DROP CONSTRAINT IF EXISTS wallets_pkey;
ALTER TABLE wallets ADD CONSTRAINT wallets_pkey PRIMARY KEY(id);

ALTER TABLE topup_orders ADD COLUMN IF NOT EXISTS id BIGSERIAL;
CREATE UNIQUE INDEX IF NOT EXISTS topup_orders_order_no_key ON topup_orders(order_no);
ALTER TABLE topup_orders DROP CONSTRAINT IF EXISTS topup_orders_pkey;
ALTER TABLE topup_orders ADD CONSTRAINT topup_orders_pkey PRIMARY KEY(id);

ALTER TABLE account_renewals ADD COLUMN IF NOT EXISTS id BIGSERIAL;
CREATE UNIQUE INDEX IF NOT EXISTS account_renewals_user_id_request_key_key ON account_renewals(user_id,request_key);
ALTER TABLE account_renewals DROP CONSTRAINT IF EXISTS account_renewals_pkey;
ALTER TABLE account_renewals ADD CONSTRAINT account_renewals_pkey PRIMARY KEY(id);

ALTER TABLE auth_limits ADD COLUMN IF NOT EXISTS id BIGSERIAL;
CREATE UNIQUE INDEX IF NOT EXISTS auth_limits_key_key ON auth_limits(key);
ALTER TABLE auth_limits DROP CONSTRAINT IF EXISTS auth_limits_pkey;
ALTER TABLE auth_limits ADD CONSTRAINT auth_limits_pkey PRIMARY KEY(id);

COMMIT;

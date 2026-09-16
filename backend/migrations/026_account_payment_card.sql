BEGIN;

ALTER TABLE chatgpt_accounts ADD COLUMN IF NOT EXISTS payment_card_id BIGINT;
CREATE INDEX IF NOT EXISTS chatgpt_accounts_payment_card_idx ON chatgpt_accounts(payment_card_id) WHERE deleted_at IS NULL AND payment_card_id IS NOT NULL;
COMMENT ON TABLE chatgpt_accounts IS 'ChatGPT 账号：所属用户、加密 Session、分组、上次登录、续订意愿及提醒、人工续订日期、默认付款卡及有订单凭据的订阅核验状态；默认付款卡不代表官网绑卡或自动扣款。';

COMMIT;

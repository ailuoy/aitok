ALTER TABLE chatgpt_accounts ADD COLUMN IF NOT EXISTS subscription_package_id BIGINT CHECK (subscription_package_id > 0);
CREATE INDEX IF NOT EXISTS chatgpt_accounts_subscription_package_idx ON chatgpt_accounts(subscription_package_id) WHERE deleted_at IS NULL AND subscription_package_id IS NOT NULL;
COMMENT ON TABLE chatgpt_accounts IS 'ChatGPT 账号：所属用户、加密 Session、分组、管理员长文本备注、当前订阅产品选型、上次登录、续订意愿及提醒、人工续订日期、默认付款卡、账单地址及有订单凭据的订阅核验状态；产品选型可人工维护，订单开通同步对应套餐；绑定仅供助手默认选择，不代表官网绑卡或自动扣款。';

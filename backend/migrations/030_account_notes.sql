ALTER TABLE chatgpt_accounts ADD COLUMN IF NOT EXISTS notes TEXT NOT NULL DEFAULT '' CHECK (length(notes) <= 20000);
COMMENT ON TABLE chatgpt_accounts IS 'ChatGPT 账号：所属用户、加密 Session、分组、管理员长文本备注、上次登录、续订意愿及提醒、人工续订日期、默认付款卡、账单地址及有订单凭据的订阅核验状态；绑定仅供助手默认选择，不代表官网绑卡或自动扣款。';

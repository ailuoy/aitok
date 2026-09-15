BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';

-- 为已有数据库补齐表说明，与 schema.sql 中的注释保持一致。
COMMENT ON TABLE users IS '平台用户：保存登录邮箱、密码哈希和用户角色；超级管理员使用后端配置及固定内部身份。';
COMMENT ON TABLE account_groups IS '账号分组：按用户管理一批 ChatGPT 账号，组名在同一用户下忽略大小写唯一。';
COMMENT ON TABLE chatgpt_accounts IS 'ChatGPT 账号：记录所属用户、加密 Session、分组、续订日期和上次登录时间；api_key 为历史兼容字段。';
COMMENT ON TABLE email_codes IS '邮箱验证码：按邮箱及用途保存验证码哈希和过期时间。';
COMMENT ON TABLE wallets IS '代币钱包：保存平台用户的代币余额，与银行卡 USD 资金账本独立。';
COMMENT ON TABLE topup_orders IS '钱包充值订单：记录 Stripe 付款状态、USD 最小货币单位金额、代币数量、价格和幂等请求。';
COMMENT ON TABLE wallet_ledger IS '代币钱包流水：记录代币收支、交易后余额及业务引用，保留历史充值和续订记录。';
COMMENT ON TABLE account_renewals IS '历史账号续订记录：保存代币扣款、续订日期及账号快照；账号删除后仍保留。';
COMMENT ON TABLE renewal_date_audit IS '续订日期审计：记录管理员设置账号续订日期前后的值和操作时间。';
COMMENT ON TABLE addresses IS '账单地址库：保存姓名、地址及完整来源资料；无所属用户的记录为共享地址。';
COMMENT ON TABLE bank_cards IS '银行卡：保存所属用户、加密卡号、卡平台、备注及 USD 美分记账余额；不保存安全码。';
COMMENT ON TABLE bank_card_ledger IS '银行卡 USD 对账流水：记录初始余额、存入、账号开通支出及交易后余额，保存账号和 PHP 原价快照，按请求和账号防重复扣款。';

COMMIT;

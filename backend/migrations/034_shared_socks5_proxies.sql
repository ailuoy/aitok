CREATE TABLE IF NOT EXISTS socks5_proxies (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  host TEXT NOT NULL CHECK (length(host) BETWEEN 1 AND 253),
  port INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
  username TEXT NOT NULL DEFAULT '',
  password_ciphertext TEXT NOT NULL DEFAULT '',
  last_test JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ
);
COMMENT ON TABLE socks5_proxies IS '全站共享 SOCKS5 代理；无登录用户或设备归属，密码加密保存，测试结果仅为本机助手上报，不代表服务端网络验证。';
ALTER TABLE chatgpt_accounts ADD COLUMN IF NOT EXISTS socks5_proxy_id BIGINT;
CREATE INDEX IF NOT EXISTS chatgpt_accounts_socks5_proxy_idx ON chatgpt_accounts(socks5_proxy_id) WHERE deleted_at IS NULL AND socks5_proxy_id IS NOT NULL;
COMMENT ON TABLE chatgpt_accounts IS 'ChatGPT 账号：所属用户、加密 Session、分组、管理员长文本备注、使用中或废弃状态及最近废弃理由、当前订阅产品选型、上次登录、续订意愿及提醒、人工续订日期、默认付款卡、账单地址及有订单凭据的订阅核验状态；使用状态仅作管理标记，恢复使用保留最近废弃理由；产品选型可人工维护，订单开通同步对应套餐；绑定仅供助手默认选择，不代表官网绑卡或自动扣款。 SOCKS5 代理通过 socks5_proxy_id 关联全站共享配置，选择不按登录用户或设备区分。';

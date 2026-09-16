-- 新账号默认不续订，保留已有账号明确保存的续订选择。
BEGIN;
ALTER TABLE chatgpt_accounts ALTER COLUMN renewal_enabled SET DEFAULT FALSE;
COMMIT;

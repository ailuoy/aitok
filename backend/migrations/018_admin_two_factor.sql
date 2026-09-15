BEGIN;
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_ciphertext TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_pending_ciphertext TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_pending_expires_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_enabled_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_last_step BIGINT NOT NULL DEFAULT -1;
COMMENT ON TABLE users IS '系统用户与角色；管理员验证器密钥加密保存，待绑定密钥限时确认，TOTP 时间步防重放；空角色按普通用户处理';
COMMIT;

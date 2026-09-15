-- 历史基线：保留最初三张表的定义，供已有数据库逐步升级；后续结构见编号迁移。
-- 平台用户；超级管理员由后端在首次成功登录时创建。
CREATE TABLE IF NOT EXISTS users (
  id BIGSERIAL PRIMARY KEY,
  email TEXT UNIQUE NOT NULL,
  password_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ChatGPT 账号；api_key 为旧版本兼容字段，新版本使用 session_ciphertext。
CREATE TABLE IF NOT EXISTS chatgpt_accounts (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  label TEXT NOT NULL,
  email TEXT NOT NULL,
  api_key TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 邮箱验证码；code 保存验证码哈希。
CREATE TABLE IF NOT EXISTS email_codes (
  email TEXT NOT NULL,
  purpose TEXT NOT NULL,
  code TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY(email, purpose)
);

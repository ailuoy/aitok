BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';

-- 兼容历史空角色；超级管理员由固定内部身份和环境配置确定。
ALTER TABLE users ADD COLUMN IF NOT EXISTS role TEXT DEFAULT 'user' CHECK (role IN ('', 'user', 'admin'));

COMMIT;

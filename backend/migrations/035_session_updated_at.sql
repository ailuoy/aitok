BEGIN;

-- 仅记录实际保存 Session 的时间，历史记录不以普通更新时间推算。
ALTER TABLE chatgpt_accounts ADD COLUMN IF NOT EXISTS session_updated_at TIMESTAMPTZ;

COMMIT;

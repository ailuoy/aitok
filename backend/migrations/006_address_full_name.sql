BEGIN;

-- 旧地址未采集姓名，保留空值供用户补充。
ALTER TABLE addresses ADD COLUMN IF NOT EXISTS full_name TEXT NOT NULL DEFAULT '' CHECK (length(full_name) <= 120);

COMMIT;

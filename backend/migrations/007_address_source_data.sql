BEGIN;

-- 保留生成器返回的完整资料，基础地址字段仍可独立编辑。
ALTER TABLE addresses ADD COLUMN IF NOT EXISTS source_data JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(source_data) = 'object');

COMMIT;

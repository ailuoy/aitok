BEGIN;

CREATE TABLE IF NOT EXISTS account_groups (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS account_groups_user_name_unique ON account_groups(user_id, lower(name));
ALTER TABLE chatgpt_accounts ADD COLUMN IF NOT EXISTS group_id BIGINT REFERENCES account_groups(id) ON DELETE SET NULL;
ALTER TABLE chatgpt_accounts ADD COLUMN IF NOT EXISTS last_login_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS chatgpt_accounts_group_id_idx ON chatgpt_accounts(group_id);

COMMIT;

BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';

-- 未记录过的历史时间使用迁移时间，不猜测既往业务时间。

ALTER TABLE users
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE account_groups
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE chatgpt_accounts
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE email_codes
  ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE wallets
  ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE topup_orders
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE wallet_ledger
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE account_renewals
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE renewal_date_audit
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE addresses
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE bank_cards
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE bank_card_ledger
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

-- 活跃数据使用部分唯一索引；财务幂等键继续跨软删除记录唯一。
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_key;
CREATE UNIQUE INDEX IF NOT EXISTS users_email_active_unique ON users(email) WHERE deleted_at IS NULL;
ALTER TABLE email_codes ADD COLUMN IF NOT EXISTS id BIGSERIAL;
ALTER TABLE email_codes DROP CONSTRAINT IF EXISTS email_codes_pkey;
ALTER TABLE email_codes ADD CONSTRAINT email_codes_pkey PRIMARY KEY(id);
CREATE UNIQUE INDEX IF NOT EXISTS email_codes_active_unique ON email_codes(email,purpose) WHERE deleted_at IS NULL;
DROP INDEX IF EXISTS account_groups_user_name_unique;
CREATE UNIQUE INDEX IF NOT EXISTS account_groups_user_name_active_unique ON account_groups(user_id,lower(name)) WHERE deleted_at IS NULL;
DROP INDEX IF EXISTS addresses_location_unique;
CREATE UNIQUE INDEX IF NOT EXISTS addresses_location_active_unique ON addresses(lower(address_line1),lower(address_line2),lower(city),lower(state),lower(postal_code),country) WHERE deleted_at IS NULL;
ALTER TABLE bank_cards DROP CONSTRAINT IF EXISTS bank_cards_user_id_number_fingerprint_key;
CREATE UNIQUE INDEX IF NOT EXISTS bank_cards_fingerprint_active_unique ON bank_cards(user_id,number_fingerprint) WHERE deleted_at IS NULL;

-- 取消物理级联删除；分组软删除时由事务解除活跃账号的分组绑定。

ALTER TABLE account_groups DROP CONSTRAINT IF EXISTS account_groups_user_id_fkey;
ALTER TABLE account_groups ADD CONSTRAINT account_groups_user_id_fkey FOREIGN KEY(user_id) REFERENCES users(id);

ALTER TABLE chatgpt_accounts DROP CONSTRAINT IF EXISTS chatgpt_accounts_user_id_fkey;
ALTER TABLE chatgpt_accounts ADD CONSTRAINT chatgpt_accounts_user_id_fkey FOREIGN KEY(user_id) REFERENCES users(id);

ALTER TABLE addresses DROP CONSTRAINT IF EXISTS addresses_user_id_fkey;
ALTER TABLE addresses ADD CONSTRAINT addresses_user_id_fkey FOREIGN KEY(user_id) REFERENCES users(id);

ALTER TABLE bank_cards DROP CONSTRAINT IF EXISTS bank_cards_user_id_fkey;
ALTER TABLE bank_cards ADD CONSTRAINT bank_cards_user_id_fkey FOREIGN KEY(user_id) REFERENCES users(id);

ALTER TABLE chatgpt_accounts DROP CONSTRAINT IF EXISTS chatgpt_accounts_group_id_fkey;
ALTER TABLE chatgpt_accounts ADD CONSTRAINT chatgpt_accounts_group_id_fkey FOREIGN KEY(group_id) REFERENCES account_groups(id);

-- 所有表统一维护更新时间，并保留首次创建时间。
CREATE OR REPLACE FUNCTION aitok_touch_timestamps() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  NEW.created_at := OLD.created_at;
  NEW.updated_at := statement_timestamp();
  RETURN NEW;
END;
$$;

-- 业务表禁止物理删除和清空；删除必须写 deleted_at。
CREATE OR REPLACE FUNCTION aitok_prevent_hard_delete() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'Physical DELETE/TRUNCATE is forbidden for %, use deleted_at', TG_TABLE_NAME USING ERRCODE = '23514';
END;
$$;

CREATE OR REPLACE TRIGGER users_touch_timestamps BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER users_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON users FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER account_groups_touch_timestamps BEFORE UPDATE ON account_groups FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER account_groups_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON account_groups FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER chatgpt_accounts_touch_timestamps BEFORE UPDATE ON chatgpt_accounts FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER chatgpt_accounts_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON chatgpt_accounts FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER email_codes_touch_timestamps BEFORE UPDATE ON email_codes FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER email_codes_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON email_codes FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER wallets_touch_timestamps BEFORE UPDATE ON wallets FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER wallets_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON wallets FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER topup_orders_touch_timestamps BEFORE UPDATE ON topup_orders FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER topup_orders_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON topup_orders FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER wallet_ledger_touch_timestamps BEFORE UPDATE ON wallet_ledger FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER wallet_ledger_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON wallet_ledger FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER account_renewals_touch_timestamps BEFORE UPDATE ON account_renewals FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER account_renewals_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON account_renewals FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER renewal_date_audit_touch_timestamps BEFORE UPDATE ON renewal_date_audit FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER renewal_date_audit_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON renewal_date_audit FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER addresses_touch_timestamps BEFORE UPDATE ON addresses FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER addresses_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON addresses FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER bank_cards_touch_timestamps BEFORE UPDATE ON bank_cards FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER bank_cards_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON bank_cards FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();
CREATE OR REPLACE TRIGGER bank_card_ledger_touch_timestamps BEFORE UPDATE ON bank_card_ledger FOR EACH ROW EXECUTE FUNCTION aitok_touch_timestamps();
CREATE OR REPLACE TRIGGER bank_card_ledger_prevent_hard_delete BEFORE DELETE OR TRUNCATE ON bank_card_ledger FOR EACH STATEMENT EXECUTE FUNCTION aitok_prevent_hard_delete();

COMMIT;

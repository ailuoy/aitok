BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';

-- 仅移除数据库业务约束；历史记录不删除。关联、时间及软删除由应用层维护。
ALTER TABLE account_groups DROP CONSTRAINT IF EXISTS account_groups_user_id_fkey;
ALTER TABLE chatgpt_accounts DROP CONSTRAINT IF EXISTS chatgpt_accounts_user_id_fkey;
ALTER TABLE chatgpt_accounts DROP CONSTRAINT IF EXISTS chatgpt_accounts_group_id_fkey;
ALTER TABLE wallets DROP CONSTRAINT IF EXISTS wallets_user_id_fkey;
ALTER TABLE topup_orders DROP CONSTRAINT IF EXISTS topup_orders_user_id_fkey;
ALTER TABLE wallet_ledger DROP CONSTRAINT IF EXISTS wallet_ledger_user_id_fkey;
ALTER TABLE account_renewals DROP CONSTRAINT IF EXISTS account_renewals_user_id_fkey;
ALTER TABLE renewal_date_audit DROP CONSTRAINT IF EXISTS renewal_date_audit_admin_id_fkey;
ALTER TABLE addresses DROP CONSTRAINT IF EXISTS addresses_user_id_fkey;
ALTER TABLE bank_cards DROP CONSTRAINT IF EXISTS bank_cards_user_id_fkey;
ALTER TABLE bank_card_ledger DROP CONSTRAINT IF EXISTS bank_card_ledger_card_id_fkey;
DROP TRIGGER IF EXISTS users_prevent_hard_delete ON users;
DROP TRIGGER IF EXISTS account_groups_prevent_hard_delete ON account_groups;
DROP TRIGGER IF EXISTS chatgpt_accounts_prevent_hard_delete ON chatgpt_accounts;
DROP TRIGGER IF EXISTS email_codes_prevent_hard_delete ON email_codes;
DROP TRIGGER IF EXISTS wallets_prevent_hard_delete ON wallets;
DROP TRIGGER IF EXISTS topup_orders_prevent_hard_delete ON topup_orders;
DROP TRIGGER IF EXISTS wallet_ledger_prevent_hard_delete ON wallet_ledger;
DROP TRIGGER IF EXISTS account_renewals_prevent_hard_delete ON account_renewals;
DROP TRIGGER IF EXISTS renewal_date_audit_prevent_hard_delete ON renewal_date_audit;
DROP TRIGGER IF EXISTS addresses_prevent_hard_delete ON addresses;
DROP TRIGGER IF EXISTS bank_cards_prevent_hard_delete ON bank_cards;
DROP TRIGGER IF EXISTS bank_card_ledger_prevent_hard_delete ON bank_card_ledger;
DROP FUNCTION IF EXISTS aitok_prevent_hard_delete();

COMMIT;

BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';

-- 012 已补齐全部业务表的三个时间字段。
-- 应用在更新及软删除时显式写 updated_at，创建时间仅在 INSERT 时设置。
-- 必须与补齐时间维护逻辑的后端一同上线。
DROP TRIGGER IF EXISTS users_touch_timestamps ON users;
DROP TRIGGER IF EXISTS account_groups_touch_timestamps ON account_groups;
DROP TRIGGER IF EXISTS chatgpt_accounts_touch_timestamps ON chatgpt_accounts;
DROP TRIGGER IF EXISTS email_codes_touch_timestamps ON email_codes;
DROP TRIGGER IF EXISTS wallets_touch_timestamps ON wallets;
DROP TRIGGER IF EXISTS topup_orders_touch_timestamps ON topup_orders;
DROP TRIGGER IF EXISTS wallet_ledger_touch_timestamps ON wallet_ledger;
DROP TRIGGER IF EXISTS account_renewals_touch_timestamps ON account_renewals;
DROP TRIGGER IF EXISTS renewal_date_audit_touch_timestamps ON renewal_date_audit;
DROP TRIGGER IF EXISTS addresses_touch_timestamps ON addresses;
DROP TRIGGER IF EXISTS bank_cards_touch_timestamps ON bank_cards;
DROP TRIGGER IF EXISTS bank_card_ledger_touch_timestamps ON bank_card_ledger;
DROP FUNCTION IF EXISTS aitok_touch_timestamps();

COMMIT;

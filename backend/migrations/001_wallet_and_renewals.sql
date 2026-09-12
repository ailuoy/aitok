BEGIN;
ALTER TABLE chatgpt_accounts ADD COLUMN IF NOT EXISTS session_ciphertext TEXT;
ALTER TABLE chatgpt_accounts ADD COLUMN IF NOT EXISTS renewal_date DATE;

CREATE TABLE IF NOT EXISTS wallets (
  user_id BIGINT PRIMARY KEY REFERENCES users(id),
  balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0)
);
CREATE TABLE IF NOT EXISTS topup_orders (
  order_no TEXT PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id),
  request_key TEXT NOT NULL,
  amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
  tokens BIGINT NOT NULL CHECK (tokens > 0),
  currency TEXT NOT NULL DEFAULT 'usd',
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','paid','expired','failed')),
  session_id TEXT UNIQUE,
  checkout_url TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(user_id, request_key)
);
CREATE TABLE IF NOT EXISTS wallet_ledger (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id),
  amount BIGINT NOT NULL,
  balance_after BIGINT NOT NULL CHECK (balance_after >= 0),
  kind TEXT NOT NULL,
  reference TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS account_renewals (
  user_id BIGINT NOT NULL REFERENCES users(id),
  request_key TEXT NOT NULL,
  account_id BIGINT NOT NULL,
  tokens BIGINT NOT NULL CHECK (tokens > 0),
  renewal_date DATE NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY(user_id, request_key)
);
CREATE TABLE IF NOT EXISTS renewal_date_audit (
  id BIGSERIAL PRIMARY KEY,
  account_id BIGINT NOT NULL,
  admin_id BIGINT NOT NULL REFERENCES users(id),
  previous_date DATE,
  renewal_date DATE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
COMMIT;

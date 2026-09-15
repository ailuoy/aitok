BEGIN;

ALTER TABLE addresses ADD COLUMN IF NOT EXISTS user_id BIGINT REFERENCES users(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS addresses_user_id_idx ON addresses(user_id);
CREATE TABLE IF NOT EXISTS bank_cards (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  label TEXT NOT NULL CHECK (length(label) BETWEEN 1 AND 80),
  cardholder TEXT NOT NULL CHECK (length(cardholder) BETWEEN 1 AND 120),
  number_ciphertext TEXT NOT NULL,
  number_fingerprint TEXT NOT NULL,
  last4 TEXT NOT NULL CHECK (last4 ~ '^[0-9]{4}$'),
  brand TEXT NOT NULL,
  exp_month INTEGER NOT NULL CHECK (exp_month BETWEEN 1 AND 12),
  exp_year INTEGER NOT NULL CHECK (exp_year BETWEEN 2000 AND 9999),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(user_id, number_fingerprint)
);

COMMIT;

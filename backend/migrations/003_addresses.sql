BEGIN;

-- 地址库独立于账号与账单；采集来源仅作为地址溯源信息。
CREATE TABLE IF NOT EXISTS addresses (
  id BIGSERIAL PRIMARY KEY,
  address_line1 TEXT NOT NULL CHECK (length(address_line1) BETWEEN 1 AND 200),
  address_line2 TEXT NOT NULL DEFAULT '' CHECK (length(address_line2) <= 200),
  city TEXT NOT NULL CHECK (length(city) BETWEEN 1 AND 100),
  state TEXT NOT NULL CHECK (length(state) BETWEEN 1 AND 100),
  postal_code TEXT NOT NULL CHECK (length(postal_code) BETWEEN 1 AND 20),
  country TEXT NOT NULL DEFAULT 'US' CHECK (country ~ '^[A-Z]{2}$'),
  source_url TEXT NOT NULL DEFAULT '',
  source_key TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS addresses_location_unique
  ON addresses (lower(address_line1), lower(address_line2), lower(city), lower(state), lower(postal_code), country);

COMMIT;

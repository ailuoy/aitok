-- 只读检查：核对当前 schema 下应用需要的表与字段，不读取业务数据。
-- 所有行的 status 应为 OK，missing_columns 应为空数组。
-- 这不是完整的类型、索引和约束一致性检查；详细定义见 release.sql。
WITH required(table_name, column_names) AS (
  VALUES
    ('users', ARRAY['id', 'email', 'password_hash', 'created_at']),
    ('chatgpt_accounts', ARRAY['id', 'user_id', 'label', 'email', 'api_key', 'created_at', 'session_ciphertext', 'renewal_date']),
    ('email_codes', ARRAY['email', 'purpose', 'code', 'expires_at']),
    ('wallets', ARRAY['user_id', 'balance']),
    ('topup_orders', ARRAY['order_no', 'user_id', 'request_key', 'amount_minor', 'tokens', 'currency', 'status', 'session_id', 'checkout_url', 'created_at', 'quantity', 'unit_amount_minor', 'price_id']),
    ('wallet_ledger', ARRAY['id', 'user_id', 'amount', 'balance_after', 'kind', 'reference', 'description', 'created_at']),
    ('account_renewals', ARRAY['user_id', 'request_key', 'account_id', 'tokens', 'renewal_date', 'created_at', 'account_label', 'months']),
    ('renewal_date_audit', ARRAY['id', 'account_id', 'admin_id', 'previous_date', 'renewal_date', 'created_at'])
), checked AS (
  SELECT
    r.table_name,
    EXISTS (
      SELECT 1 FROM information_schema.tables t
      WHERE t.table_schema = current_schema()
        AND t.table_name = r.table_name
        AND t.table_type = 'BASE TABLE'
    ) AS table_exists,
    ARRAY(
      SELECT name FROM unnest(r.column_names) AS expected(name)
      WHERE NOT EXISTS (
        SELECT 1 FROM information_schema.columns c
        WHERE c.table_schema = current_schema()
          AND c.table_name = r.table_name
          AND c.column_name = expected.name
      )
    ) AS missing_columns
  FROM required r
)
SELECT
  current_database() AS database_name,
  current_schema() AS schema_name,
  table_name,
  CASE
    WHEN NOT table_exists THEN 'MISSING_TABLE'
    WHEN cardinality(missing_columns) > 0 THEN 'MISSING_COLUMNS'
    ELSE 'OK'
  END AS status,
  missing_columns
FROM checked
ORDER BY table_name;

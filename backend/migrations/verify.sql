-- 只读核对所有业务表、字段、独立自增 ID 主键、时间定义及禁用数据库特性。
WITH required(table_name,column_names) AS (VALUES
('users',ARRAY['id','email','password_hash','role','session_version','disabled','permissions','totp_ciphertext','totp_pending_ciphertext','totp_pending_expires_at','totp_enabled_at','totp_last_step','created_at','updated_at','deleted_at']),
('account_groups',ARRAY['id','user_id','name','created_at','updated_at','deleted_at']),
('chatgpt_accounts',ARRAY['id','user_id','label','email','api_key','session_ciphertext','renewal_date','group_id','last_login_at','verified_plan','verified_at','subscription_ends_at','renewal_enabled','payment_card_id','billing_address_id','notes','subscription_package_id','created_at','updated_at','deleted_at']),
('email_codes',ARRAY['id','email','purpose','code','expires_at','attempts','created_at','updated_at','deleted_at']),
('order_wallet_debits',ARRAY['id','order_id','user_id','account_id','account_email','account_label','order_no','amount_usd_minor','tokens_minor','tokens_per_usd','balance_after_minor','wallet_ledger_id','actor_id','created_at','updated_at','deleted_at']),
('wallets',ARRAY['id','user_id','balance','balance_subunit','created_at','updated_at','deleted_at']),
('topup_orders',ARRAY['id','order_no','user_id','request_key','amount_minor','tokens','currency','status','session_id','checkout_url','quantity','unit_amount_minor','price_id','payment_intent','refunded_minor','reversed_tokens','dispute_status','created_at','updated_at','deleted_at']),
('wallet_ledger',ARRAY['id','user_id','amount','balance_after','kind','reference','description','amount_subunit','balance_after_subunit','created_at','updated_at','deleted_at']),
('account_renewals',ARRAY['id','user_id','request_key','account_id','tokens','renewal_date','account_label','months','created_at','updated_at','deleted_at']),
('renewal_date_audit',ARRAY['id','account_id','admin_id','previous_date','renewal_date','created_at','updated_at','deleted_at']),
('addresses',ARRAY['id','address_line1','address_line2','city','state','postal_code','country','source_url','source_key','user_id','full_name','source_data','created_at','updated_at','deleted_at']),
('bank_cards',ARRAY['id','user_id','label','cardholder','number_ciphertext','number_fingerprint','last4','brand','exp_month','exp_year','platform','notes','balance_usd_minor','status','daily_limit_usd_minor','low_balance_usd_minor','reserved_usd_minor','wallet_address','cvc_ciphertext','wallet_qr_image','created_at','updated_at','deleted_at']),
('bank_card_ledger',ARRAY['id','card_id','actor_id','request_key','kind','amount_usd_minor','balance_after_usd_minor','account_id','account_label','account_email','original_php_minor','notes','order_id','reversed_at','reference_id','external_reference','period_start','period_end','currency','original_amount_minor','pricing_snapshot','created_at','updated_at','deleted_at']),
('recharge_packages',ARRAY['auto_usd','id','name','plan','region','currency','original_amount_minor','sale_usd_minor','wallet_tokens','months','enabled','notes','created_at','updated_at','deleted_at']),
('recharge_orders',ARRAY['id','order_no','user_id','account_id','account_email','package_id','package_snapshot','period_start','period_end','sale_usd_minor','wallet_tokens','order_status','payment_method','payment_status','fulfillment_status','payment_reference','purchase_reference','card_id','cost_usd_minor','refunded_usd_minor','refunded_tokens','assignee_id','evidence','failure_reason','notes','request_key','version','verified_at','received_currency','received_amount_minor','received_usd_minor','received_exchange_rate','received_at','order_source','created_at','updated_at','deleted_at']),
('operation_events',ARRAY['id','actor_id','entity_type','entity_id','action','request_key','before_data','after_data','created_at','updated_at','deleted_at']),
('auth_limits',ARRAY['id','key','count','window_start','created_at','updated_at','deleted_at']),
('card_holds',ARRAY['id','card_id','amount_usd_minor','reference','status','actor_id','notes','created_at','updated_at','deleted_at']),
('card_statement_rows',ARRAY['id','card_id','external_reference','amount_usd_minor','occurred_at','description','resolution','actor_id','created_at','updated_at','deleted_at']),
('proxy_activity',ARRAY['id','user_id','device_id','event_id','data','created_at','updated_at','deleted_at']),
('exchange_rates',ARRAY['id','base_currency','quote_currency','rate','source','effective_at','created_at','updated_at','deleted_at']),
('payment_exceptions',ARRAY['id','event_id','order_no','kind','amount_minor','status','detail','created_at','updated_at','deleted_at'])
), checked AS (
SELECT table_name,
 ARRAY(SELECT name FROM unnest(column_names) name WHERE NOT EXISTS(SELECT 1 FROM information_schema.columns c WHERE c.table_schema=current_schema() AND c.table_name=r.table_name AND c.column_name=name)) missing,
 ARRAY(SELECT c.column_name FROM information_schema.columns c WHERE c.table_schema=current_schema() AND c.table_name=r.table_name AND c.column_name IN ('created_at','updated_at','deleted_at') AND (c.data_type<>'timestamp with time zone' OR (c.column_name<>'deleted_at' AND (c.is_nullable<>'NO' OR c.column_default IS NULL)) OR (c.column_name='deleted_at' AND c.is_nullable<>'YES'))) invalid,
 ARRAY(SELECT t.tgname FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid WHERE c.relnamespace=current_schema()::regnamespace AND c.relname=r.table_name AND NOT t.tgisinternal) triggers,
 EXISTS(SELECT 1 FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid WHERE c.relnamespace=current_schema()::regnamespace AND c.relname=r.table_name AND k.contype='f') has_fk,
 NOT EXISTS(
   SELECT 1 FROM pg_class c
   JOIN pg_attribute a ON a.attrelid=c.oid AND a.attname='id' AND NOT a.attisdropped
   JOIN pg_attrdef d ON d.adrelid=c.oid AND d.adnum=a.attnum
   JOIN pg_constraint k ON k.conrelid=c.oid AND k.contype='p' AND k.conkey=ARRAY[a.attnum]
   WHERE c.relnamespace=current_schema()::regnamespace AND c.relname=r.table_name
     AND a.atttypid='bigint'::regtype AND a.attnotnull
     AND pg_get_serial_sequence(format('%I.%I',current_schema(),r.table_name),'id') IS NOT NULL
     AND pg_get_expr(d.adbin,d.adrelid) LIKE 'nextval(%'
 ) invalid_id,
 (r.table_name='chatgpt_accounts' AND NOT EXISTS(SELECT 1 FROM information_schema.columns c WHERE c.table_schema=current_schema() AND c.table_name=r.table_name AND c.column_name='renewal_enabled' AND c.data_type='boolean' AND c.is_nullable='NO' AND c.column_default='false')) invalid_renewal_default,
 (r.table_name='chatgpt_accounts' AND NOT EXISTS(SELECT 1 FROM pg_indexes WHERE schemaname=current_schema() AND tablename=r.table_name AND indexname='chatgpt_accounts_billing_address_idx' AND indexdef LIKE '%WHERE ((deleted_at IS NULL) AND (billing_address_id IS NOT NULL))%')) invalid_address_binding,
 (r.table_name='chatgpt_accounts' AND (NOT EXISTS(SELECT 1 FROM information_schema.columns c WHERE c.table_schema=current_schema() AND c.table_name=r.table_name AND c.column_name='notes' AND c.data_type='text' AND c.is_nullable='NO' AND c.column_default IS NOT NULL) OR NOT EXISTS(SELECT 1 FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid WHERE c.relnamespace=current_schema()::regnamespace AND c.relname=r.table_name AND k.conname='chatgpt_accounts_notes_check' AND pg_get_constraintdef(k.oid) LIKE '%20000%'))) invalid_account_notes,
 (r.table_name='chatgpt_accounts' AND (NOT EXISTS(SELECT 1 FROM information_schema.columns c WHERE c.table_schema=current_schema() AND c.table_name=r.table_name AND c.column_name='subscription_package_id' AND c.data_type='bigint' AND c.is_nullable='YES') OR NOT EXISTS(SELECT 1 FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid WHERE c.relnamespace=current_schema()::regnamespace AND c.relname=r.table_name AND k.conname='chatgpt_accounts_subscription_package_id_check') OR NOT EXISTS(SELECT 1 FROM pg_indexes WHERE schemaname=current_schema() AND tablename=r.table_name AND indexname='chatgpt_accounts_subscription_package_idx'))) invalid_subscription_package,
 (r.table_name='exchange_rates' AND (SELECT count(*) FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid WHERE c.relnamespace=current_schema()::regnamespace AND c.relname=r.table_name AND k.conname IN ('exchange_rates_quote_currency_check','exchange_rates_rate_check') AND pg_get_constraintdef(k.oid) LIKE '%CNY%')<>2) invalid_fx_constraints,
 (r.table_name IN ('bank_cards','bank_card_ledger') AND NOT EXISTS(SELECT 1 FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid WHERE c.relnamespace=current_schema()::regnamespace AND c.relname=r.table_name AND k.conname=CASE r.table_name WHEN 'bank_cards' THEN 'bank_cards_balance_usd_minor_check' ELSE 'bank_card_ledger_balance_after_usd_minor_check' END AND pg_get_constraintdef(k.oid) LIKE '%-1000000000000%')) invalid_negative_balance,
 (r.table_name='bank_card_ledger' AND NOT EXISTS(SELECT 1 FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid WHERE c.relnamespace=current_schema()::regnamespace AND c.relname=r.table_name AND k.conname='bank_card_ledger_pricing_snapshot_check')) invalid_pricing_snapshot
FROM required r
)
SELECT current_database(),current_schema(),table_name,CASE WHEN cardinality(missing)+cardinality(invalid)+cardinality(triggers)>0 OR has_fk OR invalid_fx_constraints OR invalid_id OR invalid_renewal_default OR invalid_negative_balance OR invalid_pricing_snapshot OR invalid_address_binding OR invalid_account_notes OR invalid_subscription_package THEN 'INVALID' ELSE 'OK' END,missing,invalid,triggers,invalid_id FROM checked ORDER BY table_name;

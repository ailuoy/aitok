-- 只读核对所有业务表、字段、时间定义及禁用数据库特性。
WITH required(table_name,column_names) AS (VALUES
('users',ARRAY['created_at','deleted_at','disabled','email','id','password_hash','permissions','role','session_version','totp_ciphertext','totp_pending_ciphertext','totp_pending_expires_at','totp_enabled_at','totp_last_step','updated_at']),
('account_groups',ARRAY['created_at','deleted_at','id','name','updated_at','user_id']),
('chatgpt_accounts',ARRAY['api_key','created_at','deleted_at','email','group_id','id','label','last_login_at','renewal_date','renewal_enabled','session_ciphertext','subscription_ends_at','updated_at','user_id','verified_at','verified_plan']),
('email_codes',ARRAY['attempts','code','created_at','deleted_at','email','expires_at','id','purpose','updated_at']),
('wallets',ARRAY['balance','created_at','deleted_at','updated_at','user_id']),
('topup_orders',ARRAY['amount_minor','checkout_url','created_at','currency','deleted_at','dispute_status','order_no','payment_intent','price_id','quantity','refunded_minor','request_key','reversed_tokens','session_id','status','tokens','unit_amount_minor','updated_at','user_id']),
('wallet_ledger',ARRAY['amount','balance_after','created_at','deleted_at','description','id','kind','reference','updated_at','user_id']),
('account_renewals',ARRAY['account_id','account_label','created_at','deleted_at','months','renewal_date','request_key','tokens','updated_at','user_id']),
('renewal_date_audit',ARRAY['account_id','admin_id','created_at','deleted_at','id','previous_date','renewal_date','updated_at']),
('addresses',ARRAY['address_line1','address_line2','city','country','created_at','deleted_at','full_name','id','postal_code','source_data','source_key','source_url','state','updated_at','user_id']),
('bank_cards',ARRAY['balance_usd_minor','brand','cardholder','created_at','daily_limit_usd_minor','deleted_at','exp_month','exp_year','id','label','last4','low_balance_usd_minor','notes','number_ciphertext','number_fingerprint','platform','reserved_usd_minor','status','updated_at','user_id']),
('bank_card_ledger',ARRAY['reversed_at','account_email','account_id','account_label','actor_id','amount_usd_minor','balance_after_usd_minor','card_id','created_at','currency','deleted_at','external_reference','id','kind','notes','order_id','original_amount_minor','original_php_minor','period_end','period_start','reference_id','request_key','updated_at']),
('recharge_packages',ARRAY['auto_usd','created_at','currency','deleted_at','enabled','id','months','name','notes','original_amount_minor','plan','region','sale_usd_minor','updated_at','wallet_tokens']),
('recharge_orders',ARRAY['account_email','account_id','assignee_id','card_id','cost_usd_minor','created_at','deleted_at','evidence','failure_reason','fulfillment_status','id','notes','order_no','package_id','package_snapshot','payment_method','payment_reference','payment_status','period_end','period_start','purchase_reference','refunded_tokens','refunded_usd_minor','request_key','sale_usd_minor','updated_at','user_id','verified_at','version','wallet_tokens']),
('operation_events',ARRAY['action','actor_id','after_data','before_data','created_at','deleted_at','entity_id','entity_type','id','request_key','updated_at']),
('auth_limits',ARRAY['count','created_at','deleted_at','key','updated_at','window_start']),
('card_holds',ARRAY['actor_id','amount_usd_minor','card_id','created_at','deleted_at','id','notes','reference','status','updated_at']),
('card_statement_rows',ARRAY['actor_id','amount_usd_minor','card_id','created_at','deleted_at','description','external_reference','id','occurred_at','resolution','updated_at']),
('proxy_activity',ARRAY['created_at','data','deleted_at','device_id','event_id','id','updated_at','user_id']),
('exchange_rates',ARRAY['id','base_currency','quote_currency','rate','source','effective_at','created_at','updated_at','deleted_at']),
('payment_exceptions',ARRAY['amount_minor','created_at','deleted_at','detail','event_id','id','kind','order_no','status','updated_at'])
), checked AS (
SELECT table_name,
 ARRAY(SELECT name FROM unnest(column_names) name WHERE NOT EXISTS(SELECT 1 FROM information_schema.columns c WHERE c.table_schema=current_schema() AND c.table_name=r.table_name AND c.column_name=name)) missing,
 ARRAY(SELECT c.column_name FROM information_schema.columns c WHERE c.table_schema=current_schema() AND c.table_name=r.table_name AND c.column_name IN ('created_at','updated_at','deleted_at') AND (c.data_type<>'timestamp with time zone' OR (c.column_name<>'deleted_at' AND (c.is_nullable<>'NO' OR c.column_default IS NULL)) OR (c.column_name='deleted_at' AND c.is_nullable<>'YES'))) invalid,
 ARRAY(SELECT t.tgname FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid WHERE c.relnamespace=current_schema()::regnamespace AND c.relname=r.table_name AND NOT t.tgisinternal) triggers,
 EXISTS(SELECT 1 FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid WHERE c.relnamespace=current_schema()::regnamespace AND c.relname=r.table_name AND k.contype='f') has_fk,
 (r.table_name='exchange_rates' AND (SELECT count(*) FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid WHERE c.relnamespace=current_schema()::regnamespace AND c.relname=r.table_name AND k.conname IN ('exchange_rates_quote_currency_check','exchange_rates_rate_check') AND pg_get_constraintdef(k.oid) LIKE '%CNY%')<>2) invalid_fx_constraints
FROM required r
)
SELECT current_database(),current_schema(),table_name,CASE WHEN cardinality(missing)+cardinality(invalid)+cardinality(triggers)>0 OR has_fk OR invalid_fx_constraints THEN 'INVALID' ELSE 'OK' END,missing,invalid,triggers FROM checked ORDER BY table_name;

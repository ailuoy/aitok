-- 扩展汇率币种；保留所有历史数据，不自动回退已支持的币种。
BEGIN;
SET LOCAL lock_timeout = '5s';
ALTER TABLE exchange_rates DROP CONSTRAINT IF EXISTS exchange_rates_quote_currency_check;
ALTER TABLE exchange_rates ADD CONSTRAINT exchange_rates_quote_currency_check CHECK (quote_currency IN ('USD','CNY'));
ALTER TABLE exchange_rates DROP CONSTRAINT IF EXISTS exchange_rates_rate_check;
ALTER TABLE exchange_rates ADD CONSTRAINT exchange_rates_rate_check CHECK (
    (quote_currency='USD' AND rate >= 0.001 AND rate <= 0.1)
    OR (quote_currency='CNY' AND rate >= 0.01 AND rate <= 1)
);
COMMENT ON TABLE exchange_rates IS '每日汇率同步历史：rate 表示 1 PHP 折合 quote_currency（USD 或 CNY）的金额；同批币种在一个事务内写入并共享生效与同步时间，保留来源及软删除历史，供套餐折算与订单快照追溯。';
COMMIT;

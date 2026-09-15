-- 增量扩展；回退应用时保留新增结构，不自动删除汇率历史。
BEGIN;
SET LOCAL lock_timeout = '5s';

ALTER TABLE recharge_packages ADD COLUMN IF NOT EXISTS auto_usd BOOLEAN NOT NULL DEFAULT FALSE;
COMMENT ON TABLE recharge_packages IS '充值套餐：保存地区、原币价格、USD 售价、周期及可选钱包代币价格；auto_usd 套餐按最新 PHP/USD 汇率计算，订单保存购买时价格及汇率快照。';

CREATE TABLE IF NOT EXISTS exchange_rates (
    id BIGSERIAL PRIMARY KEY,
    base_currency TEXT NOT NULL CHECK (base_currency = 'PHP'),
    quote_currency TEXT NOT NULL CHECK (quote_currency = 'USD'),
    rate NUMERIC(20,12) NOT NULL CHECK (rate >= 0.001 AND rate <= 0.1),
    source TEXT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS exchange_rates_latest_idx ON exchange_rates(base_currency,quote_currency,created_at DESC,id DESC) WHERE deleted_at IS NULL;
COMMENT ON TABLE exchange_rates IS '每日汇率同步历史：rate 表示 1 PHP 折合 USD，保存来源、数据生效时间及同步时间；只追加，供套餐实时折算和订单快照追溯。';
COMMIT;

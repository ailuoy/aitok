-- 充值套餐业务数据导出：2026-09-16，共 3 条未删除套餐。
-- 适用 PostgreSQL 18.3；先完成线上表结构升级（需 recharge_packages.auto_usd 字段）。
-- 不携带本地 ID，使用线上序列生成；保留源价格、配置、备注及时间。
-- 已存在同名、同类型、同地区的未删除套餐时跳过，不覆盖线上配置。
-- 自动定价套餐依赖线上当天有效汇率；不导入本地历史汇率。
-- 执行方式：psql -X -v ON_ERROR_STOP=1 -d <线上数据库> -f <本文件>
BEGIN;
SET LOCAL lock_timeout = '10s';
LOCK TABLE public.recharge_packages IN SHARE ROW EXCLUSIVE MODE;

INSERT INTO public.recharge_packages (
  name, plan, region, currency, original_amount_minor, sale_usd_minor, wallet_tokens, months, enabled, notes, auto_usd, created_at, updated_at, deleted_at
)
SELECT source.name, source.plan, source.region, source.currency, source.original_amount_minor, source.sale_usd_minor, source.wallet_tokens, source.months, source.enabled, source.notes, source.auto_usd, source.created_at::timestamptz, source.updated_at::timestamptz, source.deleted_at::timestamptz
FROM (VALUES
  (E'ChatGPT Plus · 菲律宾 App Store（未税）', E'plus', E'PH', E'PHP', 89196, 1417, 0, 1, TRUE, E'来源：用户确认的菲律宾 App Store 套餐价格截图（2026-09-15），月付。截图含税价 PHP 999.00；增值税 12%；未税价 = 含税价 ÷ 1.12 = PHP 891.96，四舍五入到分。按用户要求使用未税 PHP 价格每日折算 USD；不代表网页版结账价格或实际银行卡扣款。', TRUE, E'2026-09-15T11:14:16.079086+00:00', E'2026-09-15T11:14:16.079086+00:00', NULL),
  (E'ChatGPT 5X · 菲律宾 App Store（未税）', E'pro_5x', E'PH', E'PHP', 579464, 9208, 0, 1, TRUE, E'来源：用户确认的菲律宾 App Store 套餐价格截图（2026-09-15），月付。截图含税价 PHP 6490.00；增值税 12%；未税价 = 含税价 ÷ 1.12 = PHP 5794.64，四舍五入到分。按用户要求使用未税 PHP 价格每日折算 USD；不代表网页版结账价格或实际银行卡扣款。', TRUE, E'2026-09-15T11:14:16.134449+00:00', E'2026-09-15T11:14:16.134449+00:00', NULL),
  (E'ChatGPT 20X · 菲律宾 App Store（未税）', E'pro_20x', E'PH', E'PHP', 891964, 14173, 0, 1, TRUE, E'来源：用户确认的菲律宾 App Store 套餐价格截图（2026-09-15），月付。截图含税价 PHP 9990.00；增值税 12%；未税价 = 含税价 ÷ 1.12 = PHP 8919.64，四舍五入到分。按用户要求使用未税 PHP 价格每日折算 USD；不代表网页版结账价格或实际银行卡扣款。', TRUE, E'2026-09-15T11:14:16.432622+00:00', E'2026-09-15T11:14:16.432622+00:00', NULL)
) AS source (name, plan, region, currency, original_amount_minor, sale_usd_minor, wallet_tokens, months, enabled, notes, auto_usd, created_at, updated_at, deleted_at)
WHERE NOT EXISTS (
  SELECT 1 FROM public.recharge_packages AS existing
  WHERE existing.deleted_at IS NULL
    AND existing.name = source.name
    AND existing.plan = source.plan
    AND existing.region = source.region
)
RETURNING id, name, plan, region, currency, original_amount_minor, sale_usd_minor, auto_usd;

COMMIT;

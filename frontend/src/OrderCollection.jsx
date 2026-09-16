import React, { useEffect, useState } from 'react';
import { request } from './api';
import Select from './Select';
import { formatCardUSD } from './BankCardLedger';
import { formatUTC8 } from './time';

const money = (minor, currency) => new Intl.NumberFormat('zh-CN', { style: 'currency', currency }).format(minor / 100);
const rateLabel = rate => {
  const [numerator, denominator = '1'] = (rate?.usd_per_unit || '0').split('/');
  return (Number(numerator) / Number(denominator)).toFixed(6);
};

export function ReceiptSummary({ order }) {
  if (order.received_currency) return <><small className="cell-secondary">实收 {order.received_currency} {money(order.received_amount_minor, order.received_currency)}</small>{order.received_currency === 'CNY' && <small className="cell-secondary">折合 {formatCardUSD(order.received_usd_minor)}</small>}</>;
  return <small className="cell-secondary">{order.payment_method === 'wallet' ? `${order.wallet_tokens} 代币` : order.payment_status === 'unpaid' ? `订单价 ${formatCardUSD(order.sale_usd_minor)}` : '实收金额未记录'}</small>;
}

export function ProfitSummary({ profit, currency, order }) {
  if (!profit) return <span className="muted">{order?.order_status === 'refunded' || order?.refunded_usd_minor > 0 ? '待退款对账' : order?.order_status === 'discarded' ? '已废弃' : order?.payment_method === 'wallet' ? '代币付款' : '实收金额未记录'}</span>;
  return <div className="order-profit"><span className={profit.usd_minor < 0 ? 'danger' : 'profit-positive'}>{formatCardUSD(profit.usd_minor)} · {profit.rate_percent}%</span>{currency === 'CNY' && <small className="cell-secondary">{money(profit.received_minor, 'CNY')}</small>}{profit.estimated && <small className="cell-secondary">预计毛利</small>}</div>;
}

export function CollectionDetails({ order }) {
  return <div className="collection-details"><div><span className="muted">客户实收</span><ReceiptSummary order={order} /></div><div><span className="muted">毛利润 / 毛利率</span><ProfitSummary profit={order.profit} currency={order.received_currency} order={order} /></div>{order.received_at && <div><span className="muted">收款确认</span><small className="cell-secondary">{formatUTC8(order.received_at)}</small></div>}{order.received_exchange_rate?.batch && <div className="collection-rate"><span>收款汇率：1 CNY ≈ {rateLabel(order.received_exchange_rate)} USD</span><small className="cell-secondary">{order.received_exchange_rate.batch.source} · 数据时间 {formatUTC8(order.received_exchange_rate.batch.effective_at)}</small></div>}</div>;
}

export default function CollectionFields({ token, order, draft, onChange, onQuoteChange, disabled = false }) {
  const [quote, setQuote] = useState(null), [error, setError] = useState(''), [loading, setLoading] = useState(false), [revision, refresh] = useState(0);
  const currency = draft.received_currency, amount = draft.received_amount || '';
  useEffect(() => {
    const controller = new AbortController();
    setQuote(null); onQuoteChange(null); setError(''); setLoading(false);
    if ((!order.id && !order.package_id) || !/^[0-9]+(\.[0-9]{1,2})?$/.test(amount) || Number(amount) <= 0) return () => controller.abort();
    setLoading(true);
    const timer = setTimeout(() => {
      request((order.id ? `/orders/${order.id}/collection-quote?` : '/orders/collection-quote?') + new URLSearchParams({ currency, amount, ...(!order.id ? { package_id: order.package_id } : {}) }), token, { signal: controller.signal })
        .then(value => { if (!controller.signal.aborted) { setQuote(value); onQuoteChange({ ...value, amountInput: amount }); } })
        .catch(e => { if (!controller.signal.aborted) setError(e.message); })
        .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    }, 250);
    return () => { controller.abort(); clearTimeout(timer); };
  }, [token, order.id, order.package_id, currency, amount, revision, onQuoteChange]);
  const refreshButton = <button type="button" className="outline small" disabled={loading || disabled} onClick={() => refresh(value => value + 1)}>重新计算</button>;
  return <div className="collection-fields"><div className="proxy-fields"><label>收款币种<Select disabled={disabled} label="收款币种" value={currency} onChange={value => onChange('received_currency', value)} options={[{ value: 'CNY', label: 'CNY 人民币' }, { value: 'USD', label: 'USD 美元' }]} /></label><label>实收金额（{currency}，必填）<input disabled={disabled} aria-label="实收金额" inputMode="decimal" pattern="[0-9]+(\.[0-9]{1,2})?" value={amount} onChange={event => onChange('received_amount', event.target.value)} required /></label></div>
    {loading && <p className="muted" role="status">正在计算收款与毛利…</p>}{error && <p className="error" role="alert">{error}</p>}
    {quote && <div className="collection-preview"><div><span className="muted">折合收入 USD</span><strong>{formatCardUSD(quote.usd_minor)}</strong></div><div><span className="muted">订单成本 USD</span><strong>{formatCardUSD(quote.profit.cost_usd_minor)}</strong></div><div><span className="muted">毛利润 / 毛利率</span><ProfitSummary profit={quote.profit} currency={currency} /></div><div className="collection-quote-actions">{quote.exchange_rate.batch && <div className="collection-rate"><span>1 CNY ≈ {rateLabel(quote.exchange_rate)} USD</span><small className="cell-secondary">汇率数据时间 {formatUTC8(quote.exchange_rate.batch.effective_at)} · 同步于 {formatUTC8(quote.exchange_rate.batch.synced_at)}</small></div>}{refreshButton}</div></div>}
    {!quote && error && refreshButton}
  </div>;
}

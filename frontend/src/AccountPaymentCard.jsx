import React, { useRef, useState } from 'react';
import Select from './Select';
import { request } from './api';
import { formatCardUSD } from './BankCardLedger';

export default function AccountPaymentCard({ account, cards, loading, fundingLoading, token, onChange, onError }) {
  const [busy, setBusy] = useState(false);
  const writing = useRef(false);
  const options = [{ value: '', label: '未绑定付款卡' }, ...cards.map(card => ({ value: String(card.id), label: `${card.label} · •••• ${card.last4}` }))];
  if (account.payment_card_id && !cards.some(card => card.id === account.payment_card_id)) {
    options.push({ value: String(account.payment_card_id), label: `${account.payment_card_label || '原付款卡'} · •••• ${account.payment_card_last4 || '—'}（不可用）`, disabled: true });
  }
  async function bind(value) {
    if (writing.current) return;
    writing.current = true; setBusy(true);
    try {
      const data = await request(`/accounts/${account.id}/payment-card`, token, { method: 'PATCH', body: { payment_card_id: value ? Number(value) : null } });
      const card = cards.find(card => card.id === data.payment_card_id);
      onChange(account.id, { ...data, payment_card_label: card?.label || '', payment_card_last4: card?.last4 || '', payment_card_available: Boolean(card) });
    } catch (error) { onError(error.message); }
    finally { writing.current = false; setBusy(false); }
  }
  const card = cards.find(card => card.id === account.payment_card_id);
  const status = fundingLoading || busy ? 'loading' : card?.funding_status || 'unknown';
  const ready = card && !fundingLoading && !busy && Number.isSafeInteger(card.balance_usd_minor);
  const statusLabel = { sufficient: '充足', insufficient: '不足', unknown: '待核算' }[status];
  return <>
    <Select label={`账号 ${account.email} 的付款卡`} value={account.payment_card_id ?? ''} options={options} disabled={loading || busy} onChange={bind} searchPlaceholder="搜索卡片名称或尾号…" />
    {account.payment_card_id && <div className="account-card-funding" aria-live="polite" title="系统 USD 余额；按当前时间、分组和搜索的全部结果，合计已开启续订且有续订日期的账号，每个账号预估扣款一次。使用当前产品价格，预授权占用不计入可用余额。">
      <small className="cell-secondary account-card-balance" data-status={ready ? status : 'unknown'}>{ready ? `余额 ${formatCardUSD(card.balance_usd_minor)} · ${statusLabel}` : loading || fundingLoading && card ? '余额核算中…' : '余额暂不可用'}</small>
      {ready && <>
        <small className="cell-secondary">本筛选 {card.renewal_count} 个续订 · {card.unknown_count ? '已知需' : '需'} {formatCardUSD(card.required_usd_minor)}</small>
        {card.unknown_count > 0 && <small className="cell-secondary">{card.unknown_count} 个账号待核算</small>}
        {card.reserved_usd_minor > 0 && <small className="cell-secondary">可用 {formatCardUSD(card.balance_usd_minor - card.reserved_usd_minor)}</small>}
      </>}
    </div>}
  </>;
}

import React, { useRef, useState } from 'react';
import Select from './Select';
import { request } from './api';

export default function AccountPaymentCard({ account, cards, loading, token, onChange, onError }) {
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
  return <Select label={`账号 ${account.email} 的付款卡`} value={account.payment_card_id ?? ''} options={options} disabled={loading || busy} onChange={bind} searchPlaceholder="搜索卡片名称或尾号…" />;
}

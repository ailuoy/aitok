import React, { useEffect, useRef, useState } from 'react';
import Dialog from './Dialog';
import { request } from './api';

function AddressSummary({ address }) {
  return <span className="account-address-summary"><strong>{address.address_line1}</strong><span>{[address.address_line2, address.city, address.state, address.postal_code, address.country].filter(Boolean).join(', ')}</span>{address.full_name && <small>{address.full_name}</small>}</span>;
}

function AddressBinding({ account, token, onChange, onClose }) {
  const [query, setQuery] = useState('');
  const [page, setPage] = useState(1);
  const [result, setResult] = useState({ addresses: [], total: 0 });
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const writing = useRef(false);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError('');
    const timer = setTimeout(() => {
      request(`/addresses?unbound=true&q=${encodeURIComponent(query.trim())}&page=${page}&page_size=10`, token, { signal: controller.signal })
        .then(data => { if (!controller.signal.aborted) setResult(data); })
        .catch(error => { if (!controller.signal.aborted) { setResult({ addresses: [], total: 0 }); setError(error.message); } })
        .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    }, 200);
    return () => { clearTimeout(timer); controller.abort(); };
  }, [token, query, page]);
  async function bind(body) {
    if (writing.current) return;
    writing.current = true; setBusy(true); setError('');
    try {
      const data = await request(`/accounts/${account.id}/billing-address`, token, { method: 'PATCH', body });
      onChange(account.id, data); onClose();
    } catch (error) { setError(error.message); }
    finally { writing.current = false; setBusy(false); }
  }
  return <Dialog title="绑定账单地址" className="account-address-dialog" onClose={() => { if (!writing.current) onClose(); }}>
    <p className="muted">{account.email}</p>
    <p>当前地址：{account.billing_address_label || (account.billing_address_id ? '原地址不可用' : '未绑定')}</p>
    <div className="browser-buttons"><button className="primary" disabled={busy} onClick={() => bind({ random: true })}>随机绑定</button><button className="outline" disabled={busy || !account.billing_address_id} onClick={() => bind({ billing_address_id: null })}>解除绑定</button></div>
    <p className="muted">仅显示未绑定的地址，每个地址只能绑定一个账号。</p>
    <label>搜索地址<input aria-label="搜索绑定地址" autoFocus maxLength={200} value={query} disabled={busy} placeholder="输入姓名、街道、城市或邮编" onChange={event => { setQuery(event.target.value); setPage(1); }} /></label>
    {error && <p className="error" role="alert">{error}</p>}
    <div className="address-binding-results" aria-busy={loading}>
      {loading ? <p className="muted" role="status">正在搜索…</p> : result.addresses.length ? result.addresses.map(address => <div className="address-binding-row" key={address.id}>
        <AddressSummary address={address} />
        <button className="outline" disabled={busy || address.id === account.billing_address_id} onClick={() => bind({ billing_address_id: address.id })}>{address.id === account.billing_address_id ? '已绑定' : '绑定此地址'}</button>
      </div>) : <p className="muted">没有匹配的地址</p>}
    </div>
    <div className="browser-buttons"><button className="outline" disabled={busy || loading || page === 1} onClick={() => setPage(value => value - 1)}>上一页</button><span>第 {page} 页 · 共 {result.total} 条</span><button className="outline" disabled={busy || loading || page * 10 >= result.total} onClick={() => setPage(value => value + 1)}>下一页</button></div>
  </Dialog>;
}

export default function AccountBillingAddress({ account, token, onChange }) {
  const [open, setOpen] = useState(false);
  const label = account.billing_address_label || (account.billing_address_id ? '原地址不可用' : '绑定地址');
  return <><button className={'account-address-button ' + (account.billing_address ? 'account-address-bound' : 'outline')} title={account.billing_address ? '点击更换账单地址' : label} aria-label={`账号 ${account.email} 的账单地址`} onClick={() => setOpen(true)}>{account.billing_address ? <AddressSummary address={account.billing_address} /> : label}</button>{open && <AddressBinding account={account} token={token} onChange={onChange} onClose={() => setOpen(false)} />}</>;
}

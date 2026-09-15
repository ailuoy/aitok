import React, { useEffect, useState } from 'react';
import { CreditCard, Plus, RefreshCw } from 'lucide-react';
import { request } from './api';
import Dialog from './Dialog';
import Select from './Select';

export default function BankCardManager({ token }) {
  const [data, setData] = useState({ cards: [], total: 0 });
  const [page, setPage] = useState(1);
  const [query, setQuery] = useState('');
  const [draft, setDraft] = useState('');
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(true);
  const [dialog, setDialog] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [formError, setFormError] = useState('');
  const [showNumber, setShowNumber] = useState(false);
  const [platform, setPlatform] = useState('');
  const [month, setMonth] = useState('1');
  const [year, setYear] = useState(String(new Date().getFullYear()));
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError('');
    request('/bank-cards?' + new URLSearchParams({ q: query, page }), token, { signal: controller.signal })
      .then(data => { if (!controller.signal.aborted) setData(data); })
      .catch(error => { if (!controller.signal.aborted) setError(error.message); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [token, query, page, revision]);
  async function edit(card) {
    if (busy) return; setBusy(true); setError(''); setFormError(''); setShowNumber(false);
    try {
      const value = card ? (await request('/bank-cards/' + card.id, token)).card : {};
      setMonth(String(value.exp_month || new Date().getMonth() + 1)); setYear(String(value.exp_year || new Date().getFullYear()));
      setPlatform(value.platform || '');
      setDialog({ type: 'edit', card: value });
    } catch (error) { setError(error.message); } finally { setBusy(false); }
  }
  async function submit(event) {
    event.preventDefault(); if (busy) return; setBusy(true); setFormError('');
    try {
      const deleting = dialog.type === 'delete';
      await request('/bank-cards' + (dialog.card.id ? '/' + dialog.card.id : ''), token, { method: deleting ? 'DELETE' : dialog.card.id ? 'PATCH' : 'POST', ...(deleting ? {} : { body: { ...Object.fromEntries(new FormData(event.currentTarget)), exp_month: Number(month), exp_year: Number(year) } }) });
      setDialog(null);
      if (deleting && data.cards.length === 1 && page > 1) setPage(page - 1);
      if (!deleting && !dialog.card.id) { setPage(1); setQuery(''); setDraft(''); }
      setRevision(value => value + 1);
    } catch (error) { setFormError(error.message); } finally { setBusy(false); }
  }
  return <section className="account-section bank-card-manager"><div className="section-title"><h2>银行卡管理 <span className="muted">{data.total} 张</span></h2><div className="browser-buttons"><button className="text-btn" disabled={loading} onClick={() => setRevision(value => value + 1)}><RefreshCw size={15} />刷新</button><button className="primary small" disabled={busy} onClick={() => edit()}><Plus size={15} />添加银行卡</button></div></div>
    <p className="muted">管理自己的付款卡，账号浏览器中可选择使用。卡号加密保存，列表只显示尾号。</p>
    <form className="address-search" onSubmit={event => { event.preventDefault(); setQuery(draft.trim()); setPage(1); }}><input aria-label="搜索银行卡" placeholder="搜索名称、持卡人、尾号、平台或备注" value={draft} maxLength={200} onChange={event => setDraft(event.target.value)} /><button className="outline small">搜索</button></form>
    {error && <p className="error" role="alert">{error}</p>}
    {loading ? <p className="muted">正在加载银行卡…</p> : <div className="bank-card-list">{data.cards.map(card => <article className="bank-card" key={card.id}><div className="card-symbol"><CreditCard size={24} /></div><div className="bank-card-info"><strong>{card.label}</strong><span className="card-number">{card.brand} · •••• {card.last4}</span><span>{card.cardholder} · {String(card.exp_month).padStart(2, '0')}/{card.exp_year}</span><span>卡平台：{card.platform || '未设置'}</span>{card.notes && <span className="bank-card-notes">备注：{card.notes}</span>}</div><div className="browser-buttons"><button className="outline small" disabled={busy} onClick={() => edit(card)}>编辑</button><button className="text-btn danger" onClick={() => { setDialog({ type: 'delete', card }); setFormError(''); }}>删除</button></div></article>)}{!data.total && <p className="empty muted">暂无银行卡，添加后可在账号浏览器中选择。</p>}</div>}
    <div className="address-pagination"><span className="muted">第 {page} / {Math.max(1, Math.ceil(data.total / 20))} 页</span><button className="outline small" disabled={loading || page === 1} onClick={() => setPage(page - 1)}>上一页</button><button className="outline small" disabled={loading || page * 20 >= data.total} onClick={() => setPage(page + 1)}>下一页</button></div>
    {dialog && <Dialog title={dialog.type === 'delete' ? '删除银行卡' : dialog.card.id ? '编辑银行卡' : '添加银行卡'} onClose={() => { if (!busy) setDialog(null); }}><form onSubmit={submit}>
      {dialog.type === 'delete' ? <p>确认删除「{dialog.card.label} · 尾号 {dialog.card.last4}」？</p> : <><label>名称<input name="label" defaultValue={dialog.card.label} required maxLength={80} autoFocus /></label><label>卡平台<Select name="platform" label="卡平台" value={platform} onChange={setPlatform} disabled={busy} options={[{ value: '', label: '未设置' }, ...Array.from(new Set([...(data.platforms || []), platform].filter(Boolean))).map(value => ({ value, label: value }))]} searchPlaceholder="输入或搜索卡平台…" createLabel="使用输入的平台" onCreate={value => { const name = value.trim().replace(/\s+/g, ' '); if (!name || Array.from(name).length > 80) { setFormError('请输入 1 至 80 字的卡平台名称'); return; } setPlatform(name); setFormError(''); }} /></label><p className="muted">选择已有平台，或在下拉框输入名称后点击“使用输入的平台”。</p><label>持卡人姓名<input name="cardholder" defaultValue={dialog.card.cardholder} required maxLength={120} autoComplete="off" /></label><label>卡号<input name="number" type={showNumber ? 'text' : 'password'} inputMode="numeric" defaultValue={dialog.card.number} required maxLength={24} autoComplete="off" /></label><button type="button" className="text-btn" onClick={() => setShowNumber(value => !value)}>{showNumber ? '隐藏卡号' : '显示卡号'}</button><div className="proxy-fields"><label>到期月份<Select label="到期月份" value={month} onChange={setMonth} options={Array.from({ length: 12 }, (_, index) => ({ value: String(index + 1), label: String(index + 1).padStart(2, '0') }))} /></label><label>到期年份<Select label="到期年份" value={year} onChange={setYear} options={Array.from({ length: 26 }, (_, index) => ({ value: String(new Date().getFullYear() + index), label: String(new Date().getFullYear() + index) }))} /></label></div><label>备注<textarea name="notes" defaultValue={dialog.card.notes} rows={3} maxLength={1000} placeholder="填写用途、使用限制等备注（选填）" /></label><p className="muted">安全码在账号浏览器填充时输入，不保存到数据库。</p></>}
      {formError && <p className="error" role="alert">{formError}</p>}<div className="browser-buttons"><button className="primary" disabled={busy}>{busy ? '处理中…' : dialog.type === 'delete' ? '确认删除' : '保存银行卡'}</button><button className="outline" type="button" disabled={busy} onClick={() => setDialog(null)}>取消</button></div>
    </form></Dialog>}
  </section>;
}

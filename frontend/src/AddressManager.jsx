import React, { useCallback, useEffect, useState } from 'react';
import { Plus, RefreshCw, Search } from 'lucide-react';
import { request } from './api';
import Dialog from './Dialog';
import AddressSourceDetails from './AddressSourceDetails';

const fields = [['full_name', '账单姓名（可选）', 120], ['address_line1', '街道地址', 200], ['address_line2', '公寓 / 房间（可选）', 200], ['city', '城市', 100], ['state', '州 / 省', 100], ['postal_code', '邮编', 20], ['country', '国家代码', 2]];
const countryNames = new Intl.DisplayNames(['zh-CN'], { type: 'region' });
function countryLabel(code) { return /^[A-Z]{2}$/.test(code) ? `${countryNames.of(code)}（${code}）` : code || '未填写'; }

export default function AddressManager({ token }) {
  const [data, setData] = useState({ addresses: [], total: 0, page_size: 20 });
  const [page, setPage] = useState(1);
  const [query, setQuery] = useState('');
  const [draftQuery, setDraftQuery] = useState('');
  const [dialog, setDialog] = useState(null);
  const [error, setError] = useState('');
  const [dialogError, setDialogError] = useState('');
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  const refresh = useCallback(() => setRevision(value => value + 1), []);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError('');
    request('/addresses?' + new URLSearchParams({ q: query, page }), token, { signal: controller.signal })
      .then(result => { if (!controller.signal.aborted) setData(result); })
      .catch(error => { if (!controller.signal.aborted) setError(error.message); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [token, page, query, revision]);
  function open(type, address = { country: 'US', state: 'OR' }) { setDialog({ type, address }); setDialogError(''); }
  async function submit(event) {
    event.preventDefault();
    if (busy) return;
    setBusy(true); setDialogError('');
    const { type, address } = dialog;
    try {
      await request('/addresses' + (address.id ? '/' + address.id : ''), token, {
        method: type === 'delete' ? 'DELETE' : address.id ? 'PATCH' : 'POST',
        ...(type !== 'delete' ? { body: Object.fromEntries(new FormData(event.currentTarget)) } : {}),
      });
      setDialog(null);
      if (type === 'delete' && data.addresses.length === 1 && page > 1) setPage(page - 1);
      else if (type === 'edit' && !address.id) { setPage(1); setQuery(''); setDraftQuery(''); }
      refresh();
    } catch (error) { setDialogError(error.message); }
    finally { setBusy(false); }
  }
  const pages = Math.max(1, Math.ceil(data.total / data.page_size));
  return <section className="account-section address-manager">
    <div className="section-title"><h2>地址管理 <span className="muted">{data.total} 条</span></h2><div className="browser-buttons"><button className="text-btn" onClick={refresh} disabled={loading}><RefreshCw size={15} />刷新</button><button className="primary small" onClick={() => open('edit')}><Plus size={15} />添加地址</button></div></div>
    <form className="address-search" onSubmit={event => { event.preventDefault(); setPage(1); setQuery(draftQuery.trim()); refresh(); }}><input aria-label="搜索地址" placeholder="搜索姓名、街道、城市、州或邮编" maxLength={200} value={draftQuery} onChange={event => setDraftQuery(event.target.value)} /><button className="outline small"><Search size={15} />搜索</button></form>
    {error && <p className="error" role="alert">{error}</p>}
    {loading ? <p className="muted" role="status">正在加载地址…</p> : <div className="address-list">{data.addresses.map(address => <article className="address-card" key={address.id}>
      <dl className="address-details">
        <div><dt>账单姓名</dt><dd className={!address.full_name ? 'muted' : undefined}>{address.full_name || '未填写'}</dd></div>
        <div className="address-street"><dt>街道地址</dt><dd>{address.address_line1}</dd></div>
        {address.address_line2 && <div><dt>公寓 / 房间</dt><dd>{address.address_line2}</dd></div>}
        <div><dt>城市</dt><dd>{address.city}</dd></div>
        <div><dt>州 / 省</dt><dd>{address.state}</dd></div>
        <div><dt>邮编</dt><dd>{address.postal_code}</dd></div>
        <div><dt>国家 / 地区</dt><dd>{countryLabel(address.country)}</dd></div>
        {address.source_data?.Telephone && <div><dt>电话</dt><dd>{address.source_data.Telephone}</dd></div>}
        {address.source_data?.Temporary_mail && <div className="address-street"><dt>邮箱</dt><dd>{address.source_data.Temporary_mail}</dd></div>}
      </dl>
      {address.can_edit && <div className="browser-buttons"><button className="outline small" onClick={() => open('edit', address)}>编辑</button><button className="text-btn danger" onClick={() => open('delete', address)}>删除</button></div>}
      <AddressSourceDetails data={address.source_data} />
    </article>)}</div>}
    {!loading && !error && data.total === 0 && <p className="empty muted">没有找到地址。</p>}
    <div className="address-pagination"><span className="muted">第 {page} / {pages} 页</span><button className="outline small" disabled={loading || page <= 1} onClick={() => setPage(page - 1)}>上一页</button><button className="outline small" disabled={loading || page >= pages} onClick={() => setPage(page + 1)}>下一页</button></div>
    {dialog && <Dialog title={dialog.type === 'delete' ? '删除地址' : dialog.address.id ? '编辑地址' : '添加地址'} onClose={() => { if (!busy) setDialog(null); }}><form onSubmit={submit}>
      {dialog.type === 'delete' ? <p>确认删除「{dialog.address.address_line1}，{dialog.address.city}」？此操作无法撤销。</p> : <div className="proxy-fields">{fields.map(([name, label, max]) => <label key={name}>{label}<input name={name} required={!['full_name', 'address_line2'].includes(name)} maxLength={max} defaultValue={dialog.address[name] || ''} disabled={busy} autoFocus={name === 'full_name'} /></label>)}</div>}
      {dialogError && <p className="error" role="alert">{dialogError}</p>}
      <div className="browser-buttons"><button className="primary" disabled={busy}>{busy ? '处理中…' : dialog.type === 'delete' ? '确认删除' : '保存地址'}</button><button type="button" className="outline" disabled={busy} onClick={() => setDialog(null)}>取消</button></div>
    </form></Dialog>}
  </section>;
}

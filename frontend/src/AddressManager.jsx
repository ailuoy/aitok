import Pagination from './Pagination';
import React, { useCallback, useEffect, useState } from 'react';
import { Plus, RefreshCw, Search } from 'lucide-react';
import { request } from './api';
import Dialog from './Dialog';
import AddressSourceDetails from './AddressSourceDetails';
import DataTable from './DataTable';
import { matchingStates, stateLabel } from './addressStates';

const fields = [['full_name', '账单姓名（可选）', 120], ['address_line1', '街道地址', 200], ['address_line2', '公寓 / 房间（可选）', 200], ['city', '城市', 100], ['state', '州 / 省', 100], ['postal_code', '邮编', 20], ['country', '国家代码', 2]];
const countryNames = new Intl.DisplayNames(['zh-CN'], { type: 'region' });
function countryLabel(code) { return /^[A-Z]{2}$/.test(code) ? `${countryNames.of(code)}（${code}）` : code || '未填写'; }

export default function AddressManager({ token }) {
  const [pageSize, setPageSize] = useState(20);
  const [data, setData] = useState({ addresses: [], total: 0, page_size: 20 });
  const [page, setPage] = useState(1);
  const [query, setQuery] = useState('');
  const [draftQuery, setDraftQuery] = useState('');
  const [dialog, setDialog] = useState(null);
  const [viewing, setViewing] = useState(null);
  const [error, setError] = useState('');
  const [dialogError, setDialogError] = useState('');
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  const refresh = useCallback(() => setRevision(value => value + 1), []);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError('');
    request('/addresses?' + new URLSearchParams({ q: query, state_codes: matchingStates(query), page, page_size: pageSize }), token, { signal: controller.signal })
      .then(result => { if (!controller.signal.aborted) setData(result); })
      .catch(error => { if (!controller.signal.aborted) setError(error.message); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [token, page, pageSize, query, revision]);
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
  return <section className="account-section address-manager">
    <div className="section-title"><h2>地址管理 <span className="muted">{data.total} 条</span></h2><div className="browser-buttons"><button className="text-btn" onClick={refresh} disabled={loading}><RefreshCw size={15} />刷新</button><button className="primary small" onClick={() => open('edit')}><Plus size={15} />添加地址</button></div></div>
    <form className="address-search" onSubmit={event => { event.preventDefault(); setPage(1); setQuery(draftQuery.trim()); refresh(); }}><input aria-label="搜索地址" placeholder="搜索姓名、街道、城市、州或邮编" maxLength={200} value={draftQuery} onChange={event => setDraftQuery(event.target.value)} /><button className="outline small"><Search size={15} />搜索</button></form>
    {error && <p className="error" role="alert">{error}</p>}
    {loading ? <p className="muted" role="status">正在加载地址…</p> : <DataTable searchQuery={query} label="地址列表" className="address-table" columns={['姓名', '国家 / 地区', '州', '城市', '街道', '邮编', '电话', '操作']} empty={!data.addresses.length && '没有找到地址。'}>
      {data.addresses.map(address => <tr className="address-row" key={address.id}>
        <td className="table-text"><strong>{address.full_name || '未填写'}</strong></td>
        <td>{countryLabel(address.country)}</td><td>{stateLabel(address)}</td><td>{address.city}</td>
        <td className="table-text">{address.address_line1}{address.address_line2 && <small className="cell-secondary">{address.address_line2}</small>}</td>
        <td className="table-mono">{address.postal_code}</td>
        <td className="table-text">{address.source_data?.Telephone || '—'}{address.source_data?.Temporary_mail && <small className="cell-secondary">{address.source_data.Temporary_mail}</small>}</td>
        <td className="table-actions"><div className="row-actions">{Object.keys(address.source_data || {}).length > 0 && <button className="outline small" onClick={() => setViewing(address)}>完整资料</button>}{address.can_edit && <><button className="outline small" disabled={busy} onClick={() => open('edit', address)}>编辑</button><button className="text-btn danger" disabled={busy} onClick={() => open('delete', address)}>删除</button></>}{!address.can_edit && <span className="muted">只读</span>}</div></td>
      </tr>)}
    </DataTable>}
    {viewing && <Dialog title="地址完整资料" onClose={() => setViewing(null)}><AddressSourceDetails data={viewing.source_data} expanded /></Dialog>}
    <Pagination page={page} pageSize={pageSize} total={data.total} onPageChange={setPage} onPageSizeChange={setPageSize} disabled={loading} />
    {dialog && <Dialog title={dialog.type === 'delete' ? '删除地址' : dialog.address.id ? '编辑地址' : '添加地址'} onClose={() => { if (!busy) setDialog(null); }}><form onSubmit={submit}>
      {dialog.type === 'delete' ? <p>确认删除「{dialog.address.address_line1}，{dialog.address.city}」？删除后将从列表隐藏，历史记录会保留。</p> : <div className="proxy-fields">{fields.map(([name, label, max]) => <label key={name}>{label}<input name={name} required={!['full_name', 'address_line2'].includes(name)} maxLength={max} defaultValue={dialog.address[name] || ''} disabled={busy} autoFocus={name === 'full_name'} /></label>)}</div>}
      {dialogError && <p className="error" role="alert">{dialogError}</p>}
      <div className="browser-buttons"><button className="primary" disabled={busy}>{busy ? '处理中…' : dialog.type === 'delete' ? '确认删除' : '保存地址'}</button><button type="button" className="outline" disabled={busy} onClick={() => setDialog(null)}>取消</button></div>
    </form></Dialog>}
  </section>;
}

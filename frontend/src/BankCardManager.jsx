import Pagination from './Pagination';
import React, { useEffect, useState } from 'react';
import { Plus, RefreshCw } from 'lucide-react';
import { request } from './api';
import Dialog from './Dialog';
import ImageUpload from './ImageUpload';
import ImagePreview from './ImagePreview';
import Select from './Select';
import DataTable from './DataTable';
import CardOperations from './CardOperations';
import BankCardLedger, { formatCardUSD } from './BankCardLedger';

export default function BankCardManager({ token, accounts = [] }) {
  const [pageSize, setPageSize] = useState(20);
  const [data, setData] = useState({ cards: [], total: 0 });
  const [archived,setArchived]=useState(false), [operations,setOperations]=useState(null);
  const [page, setPage] = useState(1);
  const [query, setQuery] = useState('');
  const [draft, setDraft] = useState('');
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(true);
  const [dialog, setDialog] = useState(null);
  const [ledgerCard, setLedgerCard] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [formError, setFormError] = useState('');
  const [showNumber, setShowNumber] = useState(false);
  const [walletQR, setWalletQR] = useState(''), [imageBusy, setImageBusy] = useState(false);
  const [platform, setPlatform] = useState('');
  const [month, setMonth] = useState('1');
  const [year, setYear] = useState(String(new Date().getFullYear()));
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError('');
    request('/bank-cards?' + new URLSearchParams({ q: query, page, page_size: pageSize, archived:archived ? '1':'0', include_numbers: '1' }), token, { signal: controller.signal })
      .then(data => { if (!controller.signal.aborted) setData(data); })
      .catch(error => { if (!controller.signal.aborted) setError(error.message); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [token, query, page, pageSize, revision,archived]);
  async function edit(card) {
    if (busy) return; setBusy(true); setError(''); setFormError(''); setShowNumber(false);
    try {
      const value = card ? (await request('/bank-cards/' + card.id, token)).card : {};
      setMonth(String(value.exp_month || new Date().getMonth() + 1)); setYear(String(value.exp_year || new Date().getFullYear()));
      setPlatform(value.platform || ''); setWalletQR(value.wallet_qr_image || ''); setImageBusy(false);
      setDialog({ type: 'edit', card: value });
    } catch (error) { setError(error.message); } finally { setBusy(false); }
  }
  async function submit(event) {
    event.preventDefault(); if (busy || imageBusy) return; setBusy(true); setFormError('');
    try {
      const deleting = dialog.type === 'delete';
      await request('/bank-cards' + (dialog.card.id ? '/' + dialog.card.id : ''), token, { method: deleting ? 'DELETE' : dialog.card.id ? 'PATCH' : 'POST', ...(deleting ? {} : { body: { ...Object.fromEntries(new FormData(event.currentTarget)), exp_month: Number(month), exp_year: Number(year), wallet_qr_image: walletQR } }) });
      setDialog(null);
      if (deleting && data.cards.length === 1 && page > 1) setPage(page - 1);
      if (!deleting && !dialog.card.id) { setPage(1); setQuery(''); setDraft(''); }
      setRevision(value => value + 1);
    } catch (error) { setFormError(error.message); } finally { setBusy(false); }
  }
  return <section className="account-section bank-card-manager"><div className="section-title"><h2>银行卡管理 <span className="muted">{data.total} 张</span></h2><div className="browser-buttons"><button className="text-btn" disabled={loading} onClick={() => setRevision(value => value + 1)}><RefreshCw size={15} />刷新</button><button className="primary small" disabled={busy || archived || data.can_manage===false} onClick={() => edit()}><Plus size={15} />添加银行卡</button></div></div>
    <form className="address-search" onSubmit={event => { event.preventDefault(); setQuery(draft.trim()); setPage(1); }}><input aria-label="搜索银行卡" placeholder="搜索名称、持卡人、尾号、平台、钱包地址或备注" value={draft} maxLength={200} onChange={event => setDraft(event.target.value)} /><button className="outline small">搜索</button><Select label="银行卡归档筛选" value={String(archived)} onChange={v=>{setArchived(v==='true');setPage(1);}} options={[{value:'false',label:'当前卡片'},{value:'true',label:'已删除卡片（历史对账）'}]}/></form>
    {error && <p className="error" role="alert">{error}</p>}
    {loading ? <p className="muted">正在加载银行卡…</p> : <DataTable searchQuery={query} label="银行卡列表" className="bank-card-table" columns={['名称', '卡平台', '卡类型 / 卡号', 'CVC', '持卡人', '有效期', '可用 / 余额 · USD', '状态', '钱包地址二维码', '备注', '操作']} empty={!data.cards.length && '暂无银行卡，添加后可在账号浏览器中选择。'}>
      {data.cards.map(card => <tr className="bank-card-row" key={card.id}>
        <td className="table-text"><strong>{card.label}</strong></td><td className="table-text">{card.platform || '未设置'}</td><td className="table-mono">{card.brand} · {card.number || ('•••• ' + card.last4)}</td><td>{card.has_cvc ? '***' : '未填写'}</td><td className="table-text">{card.cardholder}</td><td>{String(card.exp_month).padStart(2, '0')}/{String(card.exp_year).slice(-2)}</td><td>{formatCardUSD(card.balance_usd_minor-(card.reserved_usd_minor||0))}<small className="cell-secondary">余额 {formatCardUSD(card.balance_usd_minor)}</small></td><td>{card.deleted_at ? '已删除' : ({active:'正常',frozen:'冻结',invalid:'失效'}[card.status] || '正常')}</td><td className="bank-card-wallet">{card.wallet_qr_image ? <ImagePreview src={card.wallet_qr_image} alt="钱包地址二维码" /> : '—'}</td><td className="table-text bank-card-notes">{card.notes || '—'}</td>
        <td className="table-actions"><div className="row-actions"><button className="outline small" disabled={busy || data.can_finance===false} onClick={() => setLedgerCard(card)}>余额 / 对账单</button><button className="outline small" onClick={()=>setOperations(card)}>运营 / 核对</button><button className="outline small" disabled={busy || archived || data.can_manage===false || data.can_numbers===false} onClick={() => edit(card)}>编辑</button><button className="text-btn danger" disabled={busy || archived || data.can_manage===false} onClick={() => { setDialog({ type: 'delete', card }); setFormError(''); }}>删除</button></div></td>
      </tr>)}
    </DataTable>}
    {operations && <CardOperations card={operations} token={token} onClose={()=>setOperations(null)} onChange={()=>setRevision(v=>v+1)}/>}
    {ledgerCard && <BankCardLedger card={ledgerCard} token={token} accounts={accounts} onChange={() => setRevision(value => value + 1)} onClose={() => setLedgerCard(null)} />}
    <Pagination page={page} pageSize={pageSize} total={data.total} onPageChange={setPage} onPageSizeChange={setPageSize} disabled={loading} />
    {dialog && <Dialog className="bank-card-dialog" title={dialog.type === 'delete' ? '删除银行卡' : dialog.card.id ? '编辑银行卡' : '添加银行卡'} onClose={() => { if (!busy && !imageBusy) setDialog(null); }}><form className={dialog.type === 'edit' ? 'bank-card-form' : undefined} onSubmit={submit}>
      {dialog.type === 'delete' ? <p>确认删除「{dialog.card.label} · 尾号 {dialog.card.last4}」？卡片将从列表隐藏，余额及对账记录会保留。</p> : <><label>名称<input name="label" defaultValue={dialog.card.label} required maxLength={80} autoFocus /></label><label>卡平台<Select name="platform" label="卡平台" value={platform} onChange={setPlatform} disabled={busy} options={[{ value: '', label: '未设置' }, ...Array.from(new Set([...(data.platforms || []), platform].filter(Boolean))).map(value => ({ value, label: value }))]} searchPlaceholder="输入或搜索卡平台…" createLabel="使用输入的平台" onCreate={value => { const name = value.trim().replace(/\s+/g, ' '); if (!name || Array.from(name).length > 80) { setFormError('请输入 1 至 80 字的卡平台名称'); return; } setPlatform(name); setFormError(''); }} /></label><label>持卡人姓名<input name="cardholder" defaultValue={dialog.card.cardholder} required maxLength={120} autoComplete="off" /></label><label>卡号<input name="number" type={showNumber ? 'text' : 'password'} inputMode="numeric" defaultValue={dialog.card.number} required maxLength={24} autoComplete="off" /></label><button type="button" className="text-btn" onClick={() => setShowNumber(value => !value)}>{showNumber ? '隐藏卡号' : '显示卡号'}</button><div className="proxy-fields"><label>到期月份<Select label="到期月份" value={month} onChange={setMonth} options={Array.from({ length: 12 }, (_, index) => ({ value: String(index + 1), label: String(index + 1).padStart(2, '0') }))} /></label><label>到期年份<Select label="到期年份" value={year} onChange={setYear} options={Array.from({ length: 26 }, (_, index) => ({ value: String(new Date().getFullYear() + index), label: String(new Date().getFullYear() + index) }))} /></label></div><label>CVC 安全码<input name="cvc" type="text" inputMode="numeric" defaultValue={dialog.card.cvc || ''} pattern="[0-9]{3,4}" maxLength={4} autoComplete="off" placeholder="3 或 4 位数字" /></label><ImageUpload label="钱包地址二维码" value={walletQR} onChange={setWalletQR} onBusyChange={setImageBusy} disabled={busy} /><label>备注<textarea name="notes" defaultValue={dialog.card.notes} rows={3} maxLength={1000} placeholder="填写用途、使用限制等备注（选填）" /></label></>}
      {formError && <p className="error" role="alert">{formError}</p>}<div className="browser-buttons"><button className="primary" disabled={busy || imageBusy}>{busy ? '处理中…' : dialog.type === 'delete' ? '确认删除' : '保存银行卡'}</button><button className="outline" type="button" disabled={busy || imageBusy} onClick={() => setDialog(null)}>取消</button></div>
    </form></Dialog>}
  </section>;
}

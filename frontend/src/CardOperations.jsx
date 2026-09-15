import Pagination from './Pagination';
import React, { useEffect, useRef, useState } from 'react';
import { request } from './api';
import Dialog from './Dialog';
import Select from './Select';
import DataTable from './DataTable';
import { downloadCSV } from './RechargeManager';
import { formatUTC8 } from './time';
import { formatCardUSD } from './BankCardLedger';

export default function CardOperations({ card, token, onClose, onChange }) {
  const [pageSize, setPageSize] = useState(20);
  const [data, setData] = useState({ statements: [], holds: [], unmatched_ledger: [] }), [page, setPage] = useState(1), [revision, refresh] = useState(0);
  const [action, setAction] = useState('configure'), [kind, setKind] = useState('refund'), [state, setState] = useState(card.status || 'active');
  const [error, setError] = useState(''), [busy, setBusy] = useState(false), [csv, setCSV] = useState(''), [confirm, setConfirm] = useState(null);
  const pending = useRef(null), writing = useRef(false);
  useEffect(() => { const c = new AbortController(); request('/card-operations/' + card.id + '?page=' + page + '&page_size=' + pageSize, token, { signal: c.signal }).then(value => { if (!c.signal.aborted) setData(value); }).catch(e => { if (!c.signal.aborted) setError(e.message); }); return () => c.abort(); }, [card.id, token, page, pageSize, revision]);
  function prepare(event) {
    event.preventDefault(); const input = Object.fromEntries(new FormData(event.currentTarget));
    const body = { ...input, action, kind, status: state, csv, reference_id: Number(input.reference_id || 0), daily_limit_usd_minor: Math.round(Number(input.limit || 0) * 100), low_balance_usd_minor: Math.round(Number(input.warning || 0) * 100) };
    setConfirm(body); setError('');
  }
  async function save() {
    if (writing.current) return; writing.current = true; setBusy(true); setError('');
    const fingerprint = JSON.stringify(confirm);
    if (pending.current?.fingerprint !== fingerprint) pending.current = { fingerprint, key: crypto.randomUUID() };
    try { await request('/card-operations/' + card.id, token, { method: 'POST', body: { ...confirm, request_key: pending.current.key } }); pending.current = null; setConfirm(null); refresh(v => v + 1); onChange(); }
    catch (e) { setError(e.message); } finally { writing.current = false; setBusy(false); }
  }
  return <Dialog title={`${card.label} · 卡片运营与账单核对`} onClose={() => { if (!busy) onClose(); }}>
    <p className="muted">冻结金额占用可用余额；实际收支以结算流水为准。此处记录实际交易，不向卡平台发起支付。</p>
    {error && <p className="error" role="alert">{error}</p>}
    <button className="outline small" onClick={() => downloadCSV('/card-operations/' + card.id + '?export=1', token, '银行卡流水.csv').catch(e => setError(e.message))}>导出全部流水</button>
    {!card.deleted_at && <><Select label="卡片运营操作" value={action} onChange={value => { setAction(value); setConfirm(null); }} options={[{ value: 'configure', label: '状态、限额与余额预警' }, ...(data.can_adjust ? [{ value: 'posting', label: '退款、冲正与调整' }] : []), { value: 'hold', label: '记录预授权冻结' }, { value: 'import', label: '导入卡平台实际账单' }]} />
      {confirm ? <div className="card-ledger-confirm"><h3>确认保存本次操作</h3><p>{confirm.action === 'posting' ? `${confirm.kind} · USD ${confirm.amount_usd} · 原流水 ${confirm.reference_id || '无'}` : confirm.action === 'import' ? '核对 CSV 后导入；相同交易号不会重复入库，内容冲突时整批拒绝。' : confirm.action === 'settle' ? '确认该预授权已实际结算？结算将扣除记账余额。' : confirm.action === 'release' ? '确认释放这笔预授权冻结？' : '确认按填写内容更新卡片记录？'}</p>{['settle','resolve'].includes(confirm.action) && <>{confirm.action==='settle' && <label>实际结算交易号<input value={confirm.reference || ''} onChange={e=>setConfirm({...confirm,reference:e.target.value})} maxLength={200}/></label>}<label>凭证及处理说明<textarea value={confirm.notes || ''} onChange={e=>setConfirm({...confirm,notes:e.target.value})} maxLength={2000}/></label></>}<div className="browser-buttons"><button className="primary" disabled={busy || (['settle','resolve'].includes(confirm.action) && !confirm.notes?.trim()) || (confirm.action==='settle' && !confirm.reference?.trim())} onClick={save}>确认</button><button className="outline" disabled={busy} onClick={() => setConfirm(null)}>返回修改</button></div></div> : <form onSubmit={prepare}>
        {action === 'configure' && <><label>卡片状态<Select label="卡片状态" value={state} onChange={setState} options={[{ value: 'active', label: '正常' }, { value: 'frozen', label: '冻结' }, { value: 'invalid', label: '失效' }]} /></label><div className="proxy-fields"><label>每日支出限额 USD（0 不限制）<input type="number" name="limit" min="0" step="0.01" defaultValue={(card.daily_limit_usd_minor || 0) / 100} /></label><label>低余额提醒 USD<input type="number" name="warning" min="0" step="0.01" defaultValue={(card.low_balance_usd_minor || 0) / 100} /></label></div></>}
        {action === 'posting' && <><label>调整类型<Select label="资金调整类型" value={kind} onChange={setKind} options={[{ value: 'refund', label: '购买成本退款入卡' }, { value: 'reversal', label: '原流水冲正' }, { value: 'fee', label: '额外手续费' }, { value: 'adjustment', label: '其他余额调整' }]} /></label><label>原流水 ID<input name="reference_id" type="number" min="1" required={['refund', 'reversal'].includes(kind)} /></label><p className="muted">退款金额为正数；手续费为负数；冲正金额必须与原流水金额相反。已包含在实际扣款中的手续费不要再次记账。</p></>}
        {['posting', 'hold'].includes(action) && <><label>金额 USD<input name="amount_usd" required inputMode="decimal" /></label><label>实际交易号<input name="reference" required maxLength={200} /></label><label>依据 / 原因<textarea name="notes" required maxLength={2000} /></label></>}
        {action === 'import' && <><p className="muted">CSV 表头：transaction_id,amount_usd,occurred_at,description。支出使用负数；时间使用带时区的 ISO 格式，如 2026-09-15T10:00:00+08:00。每次最多 1000 行。</p><label>选择 CSV<input type="file" accept=".csv,text/csv" onChange={async e => { const file = e.target.files?.[0]; if (!file) return; if (file.size > 1024 * 1024) { setError('文件不能超过 1 MB'); return; } setCSV(await file.text()); }} /></label><label>账单内容<textarea value={csv} onChange={e => setCSV(e.target.value)} rows={6} required /></label></>}
        <button className="primary small" disabled={busy}>核对并保存</button>
      </form>}
    </>}
    <h3>预授权记录</h3><DataTable label="预授权" stickyActions={false} columns={['交易号', '冻结 USD', '状态', '操作']} empty={!data.holds.length && '暂无预授权记录。'}>{data.holds.map(h => <tr key={h.id}><td>{h.reference}</td><td>{formatCardUSD(h.amount_usd_minor)}</td><td>{{ held: '冻结中', released: '已释放', settled: '已结算' }[h.status]}</td><td>{!card.deleted_at && h.status === 'held' && <div className="row-actions"><button className="outline small" onClick={() => setConfirm({ action: 'release', hold_id: h.id })}>释放冻结</button><button className="outline small" onClick={() => setConfirm({action:'settle',hold_id:h.id,reference:'',notes:''})}>记录结算</button></div>}</td></tr>)}</DataTable>
    <h3>实际账单逐笔核对</h3><DataTable label="实际账单" stickyActions={false} columns={['时间', '交易号', '实际 USD', '流水 ID', '核对结果', '处理说明']} empty={!data.statements.length && '尚未导入实际账单。'}>{data.statements.map(row => <tr key={row.id}><td>{formatUTC8(row.occurred_at)}</td><td className="table-text">{row.external_reference}</td><td>{formatCardUSD(row.amount_usd_minor)}</td><td>{row.ledger_id || '—'}</td><td className={row.result === 'matched' ? 'credit' : 'danger'}>{{ matched: '一致', missing: '缺少系统流水', amount_mismatch: '金额不一致', wrong_card: '银行卡不一致' }[row.result]}</td><td>{row.resolution || '—'}{!card.deleted_at && row.result !== 'matched' && <button className="text-btn" onClick={() => setConfirm({action:'resolve',row_id:row.id,notes:''})}>记录处理</button>}</td></tr>)}</DataTable>
    <Pagination page={page} pageSize={pageSize} total={data.total || 0} onPageChange={setPage} onPageSizeChange={setPageSize} disabled={false} />
    <h3>尚未匹配实际账单的系统流水</h3><DataTable label="未匹配流水" stickyActions={false} columns={['流水 ID', '交易号', '金额 USD']} empty={!data.unmatched_ledger.length && '暂无未匹配流水。'}>{data.unmatched_ledger.map(row => <tr key={row.id}><td>{row.id}</td><td>{row.reference || '未填交易号'}</td><td>{formatCardUSD(row.amount)}</td></tr>)}</DataTable>
  </Dialog>;
}

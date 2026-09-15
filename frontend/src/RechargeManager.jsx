import Pagination from './Pagination';
import React, { useEffect, useRef, useState } from 'react';
import { request, API } from './api';
import { adminAuditHeaders } from './adminActivity';
import Dialog from './Dialog';
import Select from './Select';
import DataTable from './DataTable';
import { formatUTC8 } from './time';
import { formatCardUSD } from './BankCardLedger';

export async function downloadCSV(path, token, name) {
  const response = await fetch(API + path, { headers: { Authorization: `Bearer ${token}`, ...adminAuditHeaders() } });
  if (!response.ok) throw new Error((await response.json()).error || '导出失败');
  const url = URL.createObjectURL(await response.blob()); const a = document.createElement('a'); a.href = url; a.download = name; a.click(); URL.revokeObjectURL(url);
}
const fulfillment = { pending: '待处理', processing: '处理中', verifying: '待开通核验', completed: '开通完成', failed: '核验失败', cancelled: '已取消' };
const payment = { unpaid: '未收款', paid: '已收款', partial_refund: '部分退款', refunded: '已退款' };
const cnyFormatter = new Intl.NumberFormat('zh-CN', { style: 'currency', currency: 'CNY' });
const plans = [{ value: 'plus', label: 'Plus' }, { value: 'pro_5x', label: '5X' }, { value: 'pro_20x', label: '20X' }];
const actionLabels = { collect: '确认客户收款', purchase: '记录官网扣款', verify: '核验开通结果', assign: '分配处理人', retry: '重新核验', refund: '记录客户退款', cancel: '取消订单' };

export default function RechargeManager({ token, accounts, packagesOnly = false }) {
  const [pageSize, setPageSize] = useState(20);
  const [data, setData] = useState({ orders: [], packages: [], total: 0 });
  const [exchangeRate, setExchangeRate] = useState(null);
  const [packages, setPackages] = useState([]), [cards, setCards] = useState([]), [operators, setOperators] = useState([]);
  const [page, setPage] = useState(1), [query, setQuery] = useState(''), [status, setStatus] = useState('');
  const [revision, refresh] = useState(0), [error, setError] = useState(''), [loading, setLoading] = useState(false);
  const [dialog, setDialog] = useState(null), [busy, setBusy] = useState(false), [formError, setFormError] = useState('');
  const [draft, setDraft] = useState({}), [history, setHistory] = useState(null);
  const pending = useRef(null), writing = useRef(false);
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError('');
    request(packagesOnly ? '/packages' : '/orders?' + new URLSearchParams({ page, page_size: pageSize, q: query, status }), token, { signal: controller.signal })
      .then(value => { if (!controller.signal.aborted) setData(value); }).catch(e => { if (!controller.signal.aborted) setError(e.message); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [token, packagesOnly, page, pageSize, query, status, revision]);
  useEffect(() => { const controller = new AbortController(); request('/packages', token, { signal: controller.signal }).then(value => { if (!controller.signal.aborted) { setPackages(value.packages); setExchangeRate(value.exchange_rate); } }).catch(e => { if (!controller.signal.aborted) setError(e.message); }); return () => controller.abort(); }, [token, revision]);
  useEffect(() => { const timer = setInterval(() => refresh(v => v + 1), 60000); return () => clearInterval(timer); }, []);
  function open(type, item) {
    pending.current = null; setFormError(''); setDialog({ type, item });
    setDraft(type === 'package' ? { plan: 'plus', region: 'PH', currency: 'PHP', months: 1, enabled: true, wallet_tokens: 0, auto_usd: false, ...item } : { method: data.can_finance ? 'manual' : 'wallet', success: true, plan: item?.package_snapshot?.plan || 'plus', period_end: item?.period_end || '', period_start: new Date().toLocaleDateString('en-CA', { timeZone: 'Asia/Shanghai' }) });
    if (type === 'purchase') request('/bank-cards', token).then(value => setCards(value.cards)).catch(e => setFormError(e.message));
    if (type === 'assign') request('/order-operators', token).then(value => setOperators(value.users)).catch(e => setFormError(e.message));
  }
  const field = (key, value) => setDraft(current => ({ ...current, [key]: value }));
  async function save(event) {
    event.preventDefault(); if (writing.current) return; writing.current = true; setBusy(true); setFormError('');
    try {
      const { type, item } = dialog;
      const values = { ...draft, ...Object.fromEntries(new FormData(event.currentTarget)) };
      let path = '/orders', method = 'POST', body;
      if (type === 'package') {
        path = '/packages' + (item?.id ? '/' + item.id : ''); method = item?.id ? 'PATCH' : 'POST';
        body = { ...values, original_amount_minor: Math.round(Number(values.original_amount) * 100), sale_usd_minor: draft.auto_usd ? 0 : Math.round(Number(values.sale_usd) * 100), wallet_tokens: Number(values.wallet_tokens), months: Number(values.months), enabled: draft.enabled, auto_usd: draft.auto_usd };
      } else if (type === 'delete-package') { path = '/packages/' + item.id; method = 'DELETE'; }
      else if (type === 'create') body = { account_id: Number(draft.account_id), package_id: Number(draft.package_id), expected_sale_usd_minor: packages.find(p => p.id === Number(draft.package_id))?.sale_usd_minor, period_start: values.period_start, notes: values.notes || '' };
      else { path += '/' + item.id; body = { action: type, ...values, card_id: Number(draft.card_id || 0), assignee_id: Number(draft.assignee_id || 0), success: draft.success, version: item.version }; }
      if (body && type !== 'package') {
        const fingerprint = JSON.stringify(body);
        if (pending.current?.fingerprint !== fingerprint) pending.current = { fingerprint, key: crypto.randomUUID() };
        body.request_key = pending.current.key;
      }
      await request(path, token, { method, body }); setDialog(null); pending.current = null; refresh(v => v + 1);
    } catch (e) { setFormError(e.message); } finally { writing.current = false; setBusy(false); }
  }
  async function details(order) {
    try { setHistory(await request('/orders/' + order.id, token)); } catch (e) { setError(e.message); }
  }
  return <section className="account-section">
    <div className="section-title"><h2>{packagesOnly ? '充值套餐' : '充值订单'}</h2><div className="browser-buttons"><button className="outline small" onClick={() => refresh(v => v + 1)}>刷新</button>{(!packagesOnly || data.can_manage) && <button className="primary small" onClick={() => open(packagesOnly ? 'package' : 'create')}>{packagesOnly ? '新增套餐' : '创建充值订单'}</button>}{!packagesOnly && <button className="outline small" onClick={() => downloadCSV('/orders/export?' + new URLSearchParams({ q: query, status }), token, '充值订单.csv').catch(e => setError(e.message))}>导出订单</button>}</div></div>
    {packagesOnly ? exchangeRate && <p className="muted">1 PHP = {Number(exchangeRate.rate).toFixed(6)} USD{exchangeRate.cny_rate && <> / {Number(exchangeRate.cny_rate).toFixed(6)} CNY</>} · 更新于 {formatUTC8(exchangeRate.synced_at)} · <a href="https://www.exchangerate-api.com" target="_blank" rel="noreferrer">ExchangeRate-API</a>{data.exchange_rate_fresh === false && <span className="danger"> · 汇率已过期，等待同步</span>}</p> : <p className="muted">客户收款、官网扣款和会员开通分别确认。</p>}
    {!packagesOnly && <div className="address-search recharge-search"><input aria-label="搜索充值订单" placeholder="搜索订单号、账号或备注" value={query} onChange={e => { setQuery(e.target.value); setPage(1); }} /><Select label="订单状态" value={status} onChange={value => { setStatus(value); setPage(1); }} options={[{ value: '', label: '全部状态' }, ...Object.entries(fulfillment).map(([value, label]) => ({ value, label }))]} /></div>}
    {error && <p className="error" role="alert">{error}</p>}
    {loading ? <p role="status">正在加载…</p> : packagesOnly ? <DataTable searchQuery={query} label="充值套餐" columns={['名称', '套餐 / 地区', '原币价格', '售价 USD', '人民币 CNY', '代币价格', '周期', '状态', '操作']} empty={!data.packages?.length && '尚未配置套餐。请根据实际可购买的套餐设置价格。'}>{(data.packages || []).map(p => <tr key={p.id}><td>{p.name}</td><td>{plans.find(v => v.value === p.plan)?.label} / {p.region}</td><td>{p.currency} {(p.original_amount_minor / 100).toFixed(2)}</td><td>{p.auto_usd && !p.sale_usd_minor ? '—' : formatCardUSD(p.sale_usd_minor)}{p.auto_usd && <small className={'cell-secondary' + (p.price_ready === false ? ' danger' : '')}>{p.price_ready === false ? '等待汇率同步' : '每日汇率折算'}</small>}</td><td>{p.sale_cny_minor == null ? '—' : cnyFormatter.format(p.sale_cny_minor / 100)}{!p.cny_price_ready && <small className="cell-secondary danger">等待汇率同步</small>}</td><td>{p.wallet_tokens || '不启用'}</td><td>{p.months} 个月</td><td>{p.enabled ? '上架' : '下架'}</td><td>{data.can_manage && <div className="row-actions"><button className="outline small" onClick={() => open('package', p)}>编辑</button><button className="text-btn danger" onClick={() => open('delete-package', p)}>删除</button></div>}</td></tr>)}</DataTable> : <DataTable searchQuery={query} label="充值订单" columns={['订单 / 账号', '套餐 / 周期', '客户收款', '官网成本 USD', '开通状态', '处理人', '操作']} empty={!data.orders.length && '暂无充值订单。'}>{data.orders.map(o => <tr key={o.id}><td className="table-text">{o.account_email}<small className="cell-secondary">{o.order_no}</small></td><td>{o.package_snapshot.name}<small className="cell-secondary">{o.period_start} 至 {o.period_end}</small></td><td>{payment[o.payment_status]}<small className="cell-secondary">{formatCardUSD(o.sale_usd_minor)}{o.refunded_usd_minor > 0 && ` / 已退 ${formatCardUSD(o.refunded_usd_minor)}`}</small></td><td>{formatCardUSD(o.cost_usd_minor)}</td><td>{fulfillment[o.fulfillment_status]}{o.failure_reason && <small className="cell-secondary danger">{o.failure_reason}</small>}</td><td>{o.assignee_id || '未分配'}</td><td><div className="row-actions"><button className="outline small" onClick={() => details(o)}>详情</button>{o.payment_status === 'unpaid' && o.fulfillment_status !== 'cancelled' && <><button className="outline small" onClick={() => open('collect', o)}>收款 / 钱包付款</button><button className="text-btn danger" onClick={() => open('cancel', o)}>取消</button></>}{data.can_manage && !['completed', 'cancelled'].includes(o.fulfillment_status) && <button className="outline small" onClick={() => open('assign', o)}>分配</button>}{data.can_finance && o.payment_status === 'paid' && !o.cost_usd_minor && <button className="outline small" onClick={() => open('purchase', o)}>官网扣款</button>}{data.can_manage && o.cost_usd_minor > 0 && ['paid','partial_refund'].includes(o.payment_status) && !['completed', 'cancelled'].includes(o.fulfillment_status) && <button className="outline small" onClick={() => open('verify', o)}>核验开通</button>}{(data.can_manage || data.can_refund) && o.payment_status==='refunded' && !o.cost_usd_minor && o.fulfillment_status!=='cancelled' && <button className="text-btn danger" onClick={()=>open('cancel',o)}>取消</button>}{data.can_refund && ['paid', 'partial_refund'].includes(o.payment_status) && <button className="text-btn danger" onClick={() => open('refund', o)}>退款</button>}</div></td></tr>)}</DataTable>}
    {!packagesOnly && <Pagination page={page} pageSize={pageSize} total={data.total} onPageChange={setPage} onPageSizeChange={setPageSize} disabled={loading} />}
    {dialog && <Dialog title={dialog.type === 'package' ? '配置充值套餐' : dialog.type === 'delete-package' ? '确认删除套餐' : dialog.type === 'create' ? '创建充值订单' : actionLabels[dialog.type]} onClose={() => { if (!busy) setDialog(null); }}><form onSubmit={save}>
      {dialog.item?.order_no && <p>{dialog.item.account_email} · {dialog.item.order_no}</p>}
      {dialog.type === 'package' && <><label>套餐名称<input name="name" defaultValue={draft.name} required maxLength={100} /></label><div className="proxy-fields"><label>官方套餐<Select label="官方套餐" value={draft.plan} onChange={v => field('plan', v)} options={plans} /></label><label>地区<input name="region" defaultValue={draft.region} required maxLength={80} /></label><label>美元定价<Select label="美元定价方式" value={String(draft.auto_usd)} onChange={v => setDraft(current => ({ ...current, auto_usd: v === 'true', ...(v === 'true' ? { currency: 'PHP' } : {}) }))} options={[{ value: 'false', label: '固定 USD 售价' }, { value: 'true', label: 'PHP 每日汇率折算' }]} /></label><label>原币种<input name="currency" value={draft.currency} onChange={e => field('currency', e.target.value)} readOnly={draft.auto_usd} required pattern="[A-Z]{3}" maxLength={3} /></label><label>原币价格<input name="original_amount" onChange={e => field('original_amount_minor', Math.round(Number(e.target.value) * 100))} type="number" min="0.01" step="0.01" defaultValue={draft.original_amount_minor ? (draft.original_amount_minor / 100).toFixed(2) : ''} required /></label><label>{draft.auto_usd ? '折算 USD' : '售价 USD'}{draft.auto_usd ? <input aria-label="折算 USD" value={exchangeRate && draft.original_amount_minor > 0 ? (draft.original_amount_minor / 100 * Number(exchangeRate.rate)).toFixed(2) : '等待汇率同步'} readOnly /> : <input name="sale_usd" type="number" min="0.01" step="0.01" defaultValue={draft.sale_usd_minor ? (draft.sale_usd_minor / 100).toFixed(2) : ''} required />}</label><label>代币价格<input name="wallet_tokens" type="number" min="0" step="1" defaultValue={draft.wallet_tokens} required /></label><label>周期（月）<input name="months" type="number" min="1" max="36" defaultValue={draft.months} required /></label><label>上架状态<Select label="套餐上架状态" value={String(draft.enabled)} onChange={v => field('enabled', v === 'true')} options={[{ value: 'true', label: '上架' }, { value: 'false', label: '下架' }]} /></label></div><label>备注<textarea name="notes" defaultValue={draft.notes} maxLength={2000} /></label></>}
      {dialog.type === 'create' && <><label>账号<Select label="订单账号" value={draft.account_id || ''} onChange={v => field('account_id', v)} options={[{ value: '', label: '请选择账号' }, ...accounts.map(a => ({ value: String(a.id), label: a.email }))]} /></label><label>套餐<Select label="订单套餐" value={draft.package_id || ''} onChange={v => field('package_id', v)} options={[{ value: '', label: '请选择上架套餐' }, ...packages.filter(p => p.enabled && p.price_ready !== false).map(p => ({ value: String(p.id), label: `${p.name} · ${formatCardUSD(p.sale_usd_minor)} / ${p.months}个月` }))]} /></label><label>本次周期开始<input type="date" name="period_start" defaultValue={draft.period_start} required /></label><label>订单备注<textarea name="notes" maxLength={2000} /></label></>}
      {dialog.type === 'collect' && <><label>收款方式<Select label="收款方式" value={draft.method} onChange={v => field('method', v)} options={[...(data.can_finance ? [{ value: 'manual', label: '已收到线下付款' }] : []), { value: 'wallet', label: `本人钱包付款 · ${dialog.item.wallet_tokens} 代币` }]} /></label><p>确认收取 {formatCardUSD(dialog.item.sale_usd_minor)}；钱包付款将立即扣除已配置的代币。</p></>}
      {dialog.type === 'purchase' && <><label>查找付款卡<input placeholder="按名称、尾号或平台搜索" onChange={e => request('/bank-cards?q=' + encodeURIComponent(e.target.value), token).then(v => setCards(v.cards)).catch(e => setFormError(e.message))} /></label><label>付款卡<Select label="订单付款卡" value={draft.card_id || ''} onChange={v => field('card_id', v)} options={[{ value: '', label: '选择已发生付款的银行卡' }, ...cards.map(c => ({ value: String(c.id), label: `${c.label} · ${c.last4} · ${formatCardUSD(c.balance_usd_minor)}` }))]} /></label><p className="muted">这里只记录已发生的官网购买，不会再次发起付款。请填写卡平台实际结算金额，包含已收取的手续费。</p></>}
      {['purchase', 'refund'].includes(dialog.type) && <label>{dialog.type === 'refund' ? '本次客户退款 USD' : '实际扣款 USD'}<input name="amount_usd" inputMode="decimal" pattern="[0-9]+(\.[0-9]{1,2})?" required /></label>}
      {dialog.type === 'refund' && <p className="notice">钱包订单退回代币；线下订单请先确认实际退款。此操作不会向官网或银行卡发起退款，官网成本退款在卡片对账中单独记录。</p>}
      {(dialog.type === 'purchase' || dialog.type === 'refund' || (dialog.type === 'collect' && draft.method !== 'wallet')) && <label>交易号 / 凭证编号<input name="reference" required maxLength={200} /></label>}
      {dialog.type === 'verify' && <><label>核验结果<Select label="开通核验结果" value={String(draft.success)} onChange={v => field('success', v === 'true')} options={[{ value: 'true', label: '确认已按订单开通' }, { value: 'false', label: '未开通或套餐不匹配' }]} /></label>{draft.success && <div className="proxy-fields"><label>官网显示套餐<Select label="核验套餐" value={draft.plan} onChange={v => field('plan', v)} options={plans} /></label><label>官网显示到期日<input name="period_end" type="date" defaultValue={draft.period_end} required /></label></div>}</>}
      {(dialog.type === 'refund' || (dialog.type === 'verify' && !draft.success)) && <label>原因<textarea name="reason" maxLength={2000} required /></label>}
      {['purchase', 'refund', 'verify'].includes(dialog.type) || (dialog.type === 'collect' && draft.method !== 'wallet') ? <label>核对依据 / 凭证<textarea name="evidence" maxLength={4000} placeholder="填写账单号、凭证链接及核对说明，不要填写卡号、密码或 Session" required /></label> : null}
      {dialog.type === 'assign' && <label>处理人<Select label="订单处理人" value={draft.assignee_id || ''} onChange={v => field('assignee_id', v)} options={[{ value: '', label: '请选择处理人' }, ...operators.map(u => ({ value: String(u.id), label: u.email === '__superadmin__' ? '超级管理员' : u.email }))]} /></label>}
      {dialog.type === 'cancel' && <p>确认取消此订单？仅支持未收款，或已全额退款且没有未冲正官网成本的订单。</p>}
      {dialog.type === 'delete-package' && <p>确认删除「{dialog.item.name}」？套餐将隐藏，历史订单快照保留。</p>}
      {formError && <p className="error" role="alert">{formError}</p>}<div className="browser-buttons"><button className="primary" disabled={busy}>{busy ? '处理中…' : '确认并保存'}</button><button className="outline" type="button" disabled={busy} onClick={() => setDialog(null)}>取消</button></div>
    </form></Dialog>}
    {history && <Dialog title="订单详情与操作记录" onClose={() => setHistory(null)}><p>{history.order.order_no} · {history.order.account_email}</p><p>收款凭证：{history.order.payment_reference || '—'}<br />官网交易：{history.order.purchase_reference || '—'}<br />核验时间：{history.order.verified_at ? formatUTC8(history.order.verified_at) : '尚未核验'}</p><p className="table-text">核验依据：{history.order.evidence || '—'}</p><DataTable searchQuery={query} label="订单操作记录" columns={['时间', '操作者', '操作', '说明']} stickyActions={false}>{history.events.map(e => <tr key={e.id}><td>{formatUTC8(e.created_at)}</td><td>{e.actor_id}</td><td>{actionLabels[e.action] || e.action}</td><td className="table-text">{e.after_data?.input?.reason || e.after_data?.input?.evidence || '订单已创建'}</td></tr>)}</DataTable></Dialog>}
  </section>;
}

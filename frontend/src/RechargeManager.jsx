import Pagination from './Pagination';
import React, { useEffect, useRef, useState } from 'react';
import { request, API } from './api';
import { adminAuditHeaders } from './adminActivity';
import Dialog from './Dialog';
import Select from './Select';
import DataTable from './DataTable';
import CollectionFields, { CollectionDetails, ReceiptSummary, ProfitSummary } from './OrderCollection';
import EvidenceEditor, { EvidenceView, hasEvidence } from './EvidenceEditor';
import { formatUTC8 } from './time';
import { formatCardUSD } from './BankCardLedger';

export async function downloadCSV(path, token, name) {
  const response = await fetch(API + path, { headers: { Authorization: `Bearer ${token}`, ...adminAuditHeaders() } });
  if (!response.ok) throw new Error((await response.json()).error || '导出失败');
  const url = URL.createObjectURL(await response.blob()); const a = document.createElement('a'); a.href = url; a.download = name; a.click(); URL.revokeObjectURL(url);
}
const fulfillment = { pending: '待处理', processing: '处理中', verifying: '历史待处理', completed: '开通完成', failed: '历史处理失败', cancelled: '已取消' };
const orderStatuses = { active: '正常', refunded: '已退款', discarded: '已废弃' };
const canOperate = order => (order.order_status || 'active') === 'active' && order.payment_status !== 'refunded' && order.fulfillment_status !== 'cancelled';
const payment = { unpaid: '实收未记录', paid: '已收款', partial_refund: '部分退款', refunded: '已退款' };
const cnyFormatter = new Intl.NumberFormat('zh-CN', { style: 'currency', currency: 'CNY' });
const plans = [{ value: 'plus', label: 'Plus' }, { value: 'pro_5x', label: '5X' }, { value: 'pro_20x', label: '20X' }];
const actionLabels = { record: '补录订单', refund: '登记客户退款', refund_note: '退款登记（余额未退回）', cancel: '取消订单', discard: '废弃订单' };

const eventLabels = { ...actionLabels, record: '录入订单', verify: '开通核验（历史）', retry: '重新核验（历史）', collect: '客户收款（历史）', purchase: '官网扣款（历史）', refund: '客户退款（历史）', assign: '分配处理人（历史）' };

export default function RechargeManager({ token, accounts, packagesOnly = false }) {
  const [pageSize, setPageSize] = useState(20);
  const [data, setData] = useState({ orders: [], packages: [], total: 0 });
  const [exchangeRate, setExchangeRate] = useState(null);
  const [packages, setPackages] = useState([]), [cards, setCards] = useState([]);
  const [page, setPage] = useState(1), [query, setQuery] = useState(''), [status, setStatus] = useState('');
  const [revision, refresh] = useState(0), [error, setError] = useState(''), [loading, setLoading] = useState(false);
  const [dialog, setDialog] = useState(null), [busy, setBusy] = useState(false), [formError, setFormError] = useState('');
  const [draft, setDraft] = useState({}), [history, setHistory] = useState(null);
  const [collectionQuote, setCollectionQuote] = useState(null);
  const needsCollection = dialog?.type === 'create' || (dialog?.type === 'record' && dialog.item.payment_status === 'unpaid');
  const collectionReady = !needsCollection || (draft.received_amount && collectionQuote && collectionQuote.currency === draft.received_currency && collectionQuote.amountInput === draft.received_amount);
  const pending = useRef(null), writing = useRef(false);
  const [evidenceBusy, setEvidenceBusy] = useState(false), [cardsLoading, setCardsLoading] = useState(false);
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError('');
    request(packagesOnly ? '/packages' : '/orders?' + new URLSearchParams({ page, page_size: pageSize, q: query, status }), token, { signal: controller.signal })
      .then(value => { if (!controller.signal.aborted) setData(value); }).catch(e => { if (!controller.signal.aborted) setError(e.message); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [token, packagesOnly, page, pageSize, query, status, revision]);
  useEffect(() => { const controller = new AbortController(); request('/packages', token, { signal: controller.signal }).then(value => { if (!controller.signal.aborted) { setPackages(value.packages); setExchangeRate(value.exchange_rate); } }).catch(e => { if (!controller.signal.aborted) setError(e.message); }); return () => controller.abort(); }, [token, revision]);
  useEffect(() => { const timer = setInterval(() => refresh(v => v + 1), 60000); return () => clearInterval(timer); }, []);
  useEffect(() => {
    if (!['create', 'record'].includes(dialog?.type)) return;
    const controller = new AbortController(); setCards([]); setCardsLoading(true);
    async function loadCards() {
      const rows = []; let page = 1, total;
      do {
        const result = await request('/bank-cards?' + new URLSearchParams({ page, page_size: 100 }), token, { signal: controller.signal });
        rows.push(...result.cards); total = result.total; page++;
        if (!result.cards.length) break;
      } while (rows.length < total);
      if (!controller.signal.aborted) setCards(rows);
    }
    loadCards().catch(e => { if (!controller.signal.aborted) setFormError(e.message); }).finally(() => { if (!controller.signal.aborted) setCardsLoading(false); });
    return () => controller.abort();
  }, [dialog, token]);
  function open(type, item) {
    pending.current = null; setCollectionQuote(null); setEvidenceBusy(false); setFormError(''); setDialog({ type, item });
    setDraft(type === 'package' ? { plan: 'plus', region: 'PH', currency: 'PHP', months: 1, enabled: true, wallet_tokens: 0, auto_usd: false, ...item } : { received_currency: 'CNY', received_amount: '', order_source: item?.order_source || '' });
  }
  const field = (key, value) => { if (['received_currency', 'received_amount', 'package_id'].includes(key)) setCollectionQuote(null); setDraft(current => ({ ...current, [key]: value })); };
  async function save(event) {
    event.preventDefault(); if (writing.current || evidenceBusy) return; writing.current = true; setBusy(true); setFormError('');
    try {
      const { type, item } = dialog;
      if (['create', 'record', 'refund'].includes(type) && !hasEvidence(draft.evidence)) throw new Error('请填写核对说明或添加凭据图片');
      if (['create', 'record'].includes(type) && !collectionReady) throw new Error('请填写实收币种、金额并等待毛利计算完成');
      const values = { ...draft, ...Object.fromEntries(new FormData(event.currentTarget)) };
      let path = '/orders', method = 'POST', body;
      if (type === 'package') {
        path = '/packages' + (item?.id ? '/' + item.id : ''); method = item?.id ? 'PATCH' : 'POST';
        body = { ...values, original_amount_minor: Math.round(Number(values.original_amount) * 100), sale_usd_minor: draft.auto_usd ? 0 : Math.round(Number(values.sale_usd) * 100), wallet_tokens: Number(values.wallet_tokens), months: Number(values.months), enabled: draft.enabled, auto_usd: draft.auto_usd };
      } else if (type === 'delete-package') { path = '/packages/' + item.id; method = 'DELETE'; }
      else if (type === 'create') {
        path = '/orders/record';
        body = { account_id: Number(draft.account_id), package_id: Number(draft.package_id), expected_sale_usd_minor: packages.find(p => p.id === Number(draft.package_id))?.sale_usd_minor, notes: values.notes || '' };
      } else { path += '/' + item.id; body = { ...values, action: type === 'refund' ? 'refund_note' : type, version: item.version }; }
      if (['create', 'record'].includes(type)) {
        if (!draft.card_id) throw new Error('请选择付款卡');
        Object.assign(body, { order_source: draft.order_source || '', card_id: Number(draft.card_id), reference: values.reference, evidence: draft.evidence });
        if (draft.received_amount) Object.assign(body, { received_currency: draft.received_currency, received_amount: draft.received_amount, collection_rate_id: collectionQuote.exchange_rate.batch?.id || 0 });
        else { delete body.received_currency; delete body.received_amount; }
      }
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
    <div className="section-title"><h2>{packagesOnly ? '充值套餐' : '充值订单'}</h2><div className="browser-buttons"><button className="outline small" onClick={() => refresh(v => v + 1)}>刷新</button>{(!packagesOnly || data.can_manage) && <button className="primary small" onClick={() => open(packagesOnly ? 'package' : 'create')}>{packagesOnly ? '新增套餐' : '录入充值订单'}</button>}{!packagesOnly && <button className="outline small" onClick={() => downloadCSV('/orders/export?' + new URLSearchParams({ q: query, status }), token, '充值订单.csv').catch(e => setError(e.message))}>导出订单</button>}</div></div>
    {packagesOnly ? exchangeRate && <p className="muted">1 PHP = {Number(exchangeRate.rate).toFixed(6)} USD{exchangeRate.cny_rate && <> / {Number(exchangeRate.cny_rate).toFixed(6)} CNY</>} · 更新于 {formatUTC8(exchangeRate.synced_at)} · <a href="https://www.exchangerate-api.com" target="_blank" rel="noreferrer">ExchangeRate-API</a>{data.exchange_rate_fresh === false && <span className="danger"> · 汇率已过期，等待同步</span>}</p> : <p className="muted">选择账号和付款卡，一次录入订单与支出。</p>}
    {!packagesOnly && <div className="address-search recharge-search"><input aria-label="搜索充值订单" placeholder="搜索订单号、来源、账号或备注" value={query} onChange={e => { setQuery(e.target.value); setPage(1); }} /><Select label="订单状态" value={status} onChange={value => { setStatus(value); setPage(1); }} options={[{ value: '', label: '全部状态' }, ...Object.entries(orderStatuses).map(([value, label]) => ({ value, label }))]} /></div>}
    {error && <p className="error" role="alert">{error}</p>}
    {loading ? <p role="status">正在加载…</p> : packagesOnly ? <DataTable searchQuery={query} label="充值套餐" columns={['名称', '套餐 / 地区', '原币价格', '售价 USD', '人民币 CNY', '代币价格', '周期', '状态', '操作']} empty={!data.packages?.length && '尚未配置套餐。请根据实际可购买的套餐设置价格。'}>{(data.packages || []).map(p => <tr key={p.id}><td>{p.name}</td><td>{plans.find(v => v.value === p.plan)?.label} / {p.region}</td><td>{p.currency} {(p.original_amount_minor / 100).toFixed(2)}</td><td>{p.auto_usd && !p.sale_usd_minor ? '—' : formatCardUSD(p.sale_usd_minor)}{p.auto_usd && <small className={'cell-secondary' + (p.price_ready === false ? ' danger' : '')}>{p.price_ready === false ? '等待汇率同步' : '每日汇率折算'}</small>}</td><td>{p.sale_cny_minor == null ? '—' : cnyFormatter.format(p.sale_cny_minor / 100)}{!p.cny_price_ready && <small className="cell-secondary danger">等待汇率同步</small>}</td><td>{p.wallet_tokens || '不启用'}</td><td>{p.months} 个月</td><td>{p.enabled ? '上架' : '下架'}</td><td>{data.can_manage && <div className="row-actions"><button className="outline small" onClick={() => open('package', p)}>编辑</button><button className="text-btn danger" onClick={() => open('delete-package', p)}>删除</button></div>}</td></tr>)}</DataTable> : <DataTable searchQuery={query} label="充值订单" columns={['订单 / 账号', '订单来源', '套餐 / 周期', '订单状态', '客户收款', '官网成本 USD', '毛利润 / 毛利率', '开通状态', '操作']} empty={!data.orders.length && '暂无充值订单。'}>{data.orders.map(o => <tr key={o.id}><td className="table-text">{o.account_email}<small className="cell-secondary">{o.order_no}</small></td><td className="table-text">{o.order_source || "—"}</td><td>{o.package_snapshot.name}<small className="cell-secondary">{o.period_start} 至 {o.period_end}</small></td><td><span className={'order-status order-status-' + o.order_status}>{orderStatuses[o.order_status] || '正常'}</span></td><td>{payment[o.payment_status]}<ReceiptSummary order={o} />{o.refunded_usd_minor > 0 && <small className="cell-secondary">已退 {formatCardUSD(o.refunded_usd_minor)}</small>}</td><td>{formatCardUSD(o.cost_usd_minor)}</td><td><ProfitSummary profit={o.profit} currency={o.received_currency} order={o} /></td><td>{fulfillment[o.fulfillment_status]}{o.failure_reason && <small className="cell-secondary danger">{o.failure_reason}</small>}</td><td><div className="row-actions"><button className="outline small" onClick={() => details(o)}>详情</button>{canOperate(o) && <>{data.can_finance && !o.cost_usd_minor && <button className="outline small" onClick={() => open('record', o)}>补录订单</button>}{data.can_refund && ['paid', 'partial_refund'].includes(o.payment_status) && <button className="outline small" onClick={() => open('refund', o)}>退款</button>}{data.can_manage && <button className="outline small" onClick={() => open('discard', o)}>废弃</button>}</>}</div></td></tr>)}</DataTable>}
    {!packagesOnly && <Pagination page={page} pageSize={pageSize} total={data.total} onPageChange={setPage} onPageSizeChange={setPageSize} disabled={loading} />}
    {dialog && <Dialog className={['create', 'record'].includes(dialog.type) ? 'order-record-dialog' : ''} title={dialog.type === 'package' ? '配置充值套餐' : dialog.type === 'delete-package' ? '确认删除套餐' : dialog.type === 'create' ? '录入充值订单' : actionLabels[dialog.type]} onClose={() => { if (!busy) setDialog(null); }}><form className={['create', 'record'].includes(dialog.type) ? 'order-record-form' : undefined} onSubmit={save}>
      {dialog.item?.order_no && <p>{dialog.item.account_email} · {dialog.item.order_no}</p>}
      {dialog.type === 'package' && <><label>套餐名称<input name="name" defaultValue={draft.name} required maxLength={100} /></label><div className="proxy-fields"><label>官方套餐<Select label="官方套餐" value={draft.plan} onChange={v => field('plan', v)} options={plans} /></label><label>地区<input name="region" defaultValue={draft.region} required maxLength={80} /></label><label>美元定价<Select label="美元定价方式" value={String(draft.auto_usd)} onChange={v => setDraft(current => ({ ...current, auto_usd: v === 'true', ...(v === 'true' ? { currency: 'PHP' } : {}) }))} options={[{ value: 'false', label: '固定 USD 售价' }, { value: 'true', label: 'PHP 每日汇率折算' }]} /></label><label>原币种<input name="currency" value={draft.currency} onChange={e => field('currency', e.target.value)} readOnly={draft.auto_usd} required pattern="[A-Z]{3}" maxLength={3} /></label><label>原币价格<input name="original_amount" onChange={e => field('original_amount_minor', Math.round(Number(e.target.value) * 100))} type="number" min="0.01" step="0.01" defaultValue={draft.original_amount_minor ? (draft.original_amount_minor / 100).toFixed(2) : ''} required /></label><label>{draft.auto_usd ? '折算 USD' : '售价 USD'}{draft.auto_usd ? <input aria-label="折算 USD" value={exchangeRate && draft.original_amount_minor > 0 ? (draft.original_amount_minor / 100 * Number(exchangeRate.rate)).toFixed(2) : '等待汇率同步'} readOnly /> : <input name="sale_usd" type="number" min="0.01" step="0.01" defaultValue={draft.sale_usd_minor ? (draft.sale_usd_minor / 100).toFixed(2) : ''} required />}</label><label>代币价格<input name="wallet_tokens" type="number" min="0" step="1" defaultValue={draft.wallet_tokens} required /></label><label>周期（月）<input name="months" type="number" min="1" max="36" defaultValue={draft.months} required /></label><label>上架状态<Select label="套餐上架状态" value={String(draft.enabled)} onChange={v => field('enabled', v === 'true')} options={[{ value: 'true', label: '上架' }, { value: 'false', label: '下架' }]} /></label></div><label>备注<textarea name="notes" defaultValue={draft.notes} maxLength={2000} /></label></>}
      {dialog.type === 'create' && <><label>账号<Select label="订单账号" value={draft.account_id || ''} onChange={v => field('account_id', v)} options={[{ value: '', label: '请选择账号' }, ...accounts.map(a => ({ value: String(a.id), label: a.email }))]} /></label><label>套餐<Select label="订单套餐" value={draft.package_id || ''} onChange={v => field('package_id', v)} options={[{ value: '', label: '请选择上架套餐' }, ...packages.filter(p => p.enabled && p.price_ready !== false).map(p => ({ value: String(p.id), label: `${p.name} · ${formatCardUSD(p.sale_usd_minor)} / ${p.months}个月` }))]} /></label><label>订单备注<textarea name="notes" rows={2} maxLength={2000} /></label></>}
      {['create', 'record'].includes(dialog.type) && <><label>付款卡<Select disabled={cardsLoading || busy} label="订单付款卡" value={draft.card_id || ''} onChange={v => field('card_id', v)} options={[{ value: '', label: '选择付款卡' }, ...cards.map(c => ({ value: String(c.id), label: `${c.label} · ${c.last4} · ${formatCardUSD(c.balance_usd_minor)}${c.platform ? ' · ' + c.platform : ''}` }))]} /></label>
        <label>扣款 USD<input aria-label="扣款 USD" value={(() => { const price = dialog.item?.sale_usd_minor ?? packages.find(p => p.id === Number(draft.package_id))?.sale_usd_minor; return price ? (price / 100).toFixed(2) : ''; })()} readOnly /></label>
        {needsCollection ? <CollectionFields disabled={busy} token={token} order={dialog.item || { package_id: draft.package_id }} draft={draft} onChange={field} onQuoteChange={setCollectionQuote} /> : <CollectionDetails order={dialog.item} />}
      </>}
      {['create', 'record'].includes(dialog.type) && <label className="order-source-field">订单来源<Select name="order_source" label="订单来源" value={draft.order_source || ''} onChange={value => field('order_source', value)} disabled={busy} options={[{ value: '', label: '未填写' }, ...Array.from(new Set([...(data.sources || []), draft.order_source].filter(Boolean))).map(value => ({ value, label: value }))]} searchPlaceholder="输入或搜索订单来源…" createLabel="使用输入的来源" onCreate={value => { const source = value.trim().replace(/\s+/g, ' '); if (!source || Array.from(source).length > 80) { setFormError('请输入 1 至 80 字的订单来源'); return; } field('order_source', source); setFormError(''); }} /></label>}
      {['create', 'record', 'refund'].includes(dialog.type) && <label>交易号 / 凭证编号<input name="reference" required maxLength={200} /></label>}
      {['refund', 'discard'].includes(dialog.type) && <label>原因<textarea name="reason" maxLength={2000} required /></label>}
      {['create', 'record', 'refund'].includes(dialog.type) ? <EvidenceEditor value={draft.evidence || ''} onChange={value => field('evidence', value)} onBusyChange={setEvidenceBusy} disabled={busy} /> : null}

      {dialog.type === 'discard' && <p>确认废弃此订单？废弃后只能查看详情，可重新创建同周期订单；已有资金记录保留，余额不退回。</p>}
      {dialog.type === 'cancel' && <p>确认取消此订单？仅支持未收款，或已全额退款且没有未冲正官网成本的订单。</p>}
      {dialog.type === 'delete-package' && <p>确认删除「{dialog.item.name}」？套餐将隐藏，历史订单快照保留。</p>}
      {formError && <p className="error" role="alert">{formError}</p>}<div className="browser-buttons"><button className="primary" disabled={busy || evidenceBusy || (['create', 'record'].includes(dialog.type) && (cardsLoading || !collectionReady))}>{busy ? '处理中…' : ['create', 'record'].includes(dialog.type) ? '确认录入并记账' : '确认并保存'}</button><button className="outline" type="button" disabled={busy} onClick={() => setDialog(null)}>取消</button></div>
    </form></Dialog>}
    {history && <Dialog title="订单详情与操作记录" onClose={() => setHistory(null)}>{history.order.order_source && <p>订单来源：{history.order.order_source}</p>}<p>{history.order.order_no} · {history.order.account_email}</p><p>订单状态：{orderStatuses[history.order.order_status] || '正常'}<br />收款凭证：{history.order.payment_reference || '—'}<br />官网交易：{history.order.purchase_reference || '—'}<br />生效日期：{history.order.cost_usd_minor > 0 ? history.order.period_start : '尚未扣款'}<br />到期日期：{history.order.cost_usd_minor > 0 ? history.order.period_end : '—'}</p><CollectionDetails order={history.order} /><EvidenceView value={history.order.evidence} /><DataTable searchQuery={query} label="订单操作记录" columns={['时间', '操作者', '操作', '说明']} stickyActions={false}>{history.events.map(e => <tr key={e.id}><td>{formatUTC8(e.created_at)}</td><td>{e.actor_id}</td><td>{eventLabels[e.action] || e.action}</td><td className="table-text">{e.after_data?.input?.amount_usd && <p>金额 USD：{e.after_data.input.amount_usd}</p>}{e.after_data?.input?.reason && <p>{e.after_data.input.reason}</p>}<EvidenceView value={e.after_data?.input?.evidence} />{!e.after_data?.input && '订单已创建'}</td></tr>)}</DataTable></Dialog>}
  </section>;
}

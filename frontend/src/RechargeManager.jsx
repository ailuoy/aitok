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

export default function RechargeManager({ token, packagesOnly = false, quickAccount = null, onQuickClose, onQuickCreated }) {
  const [pageSize, setPageSize] = useState(20);
  const [data, setData] = useState({ orders: [], packages: [], total: 0 });
  const [exchangeRate, setExchangeRate] = useState(null);
  const [packages, setPackages] = useState([]), [orderAccounts, setOrderAccounts] = useState([]);
  const [page, setPage] = useState(1), [query, setQuery] = useState(''), [status, setStatus] = useState('');
  const [revision, refresh] = useState(0), [error, setError] = useState(''), [loading, setLoading] = useState(false);
  const [dialog, setDialog] = useState(() => quickAccount ? { type: 'create' } : null), [busy, setBusy] = useState(false), [formError, setFormError] = useState('');
  const [draft, setDraft] = useState(() => quickAccount ? { account_id: String(quickAccount.id), package_id: '', received_currency: 'CNY', received_amount: '', order_source: '' } : {}), [history, setHistory] = useState(null);
  const [collectionQuote, setCollectionQuote] = useState(null);
  const needsCollection = dialog?.type === 'create' || (dialog?.type === 'record' && dialog.item.payment_status === 'unpaid');
  const collectionReady = !needsCollection || (draft.received_amount && collectionQuote && collectionQuote.currency === draft.received_currency && collectionQuote.amountInput === draft.received_amount);
  const pending = useRef(null), writing = useRef(false);
  const [evidenceBusy, setEvidenceBusy] = useState(false), [accountsLoading, setAccountsLoading] = useState(false);
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
    const controller = new AbortController(); setOrderAccounts([]); setAccountsLoading(true);
    request('/accounts', token, { signal: controller.signal })
      .then(value => {
        if (controller.signal.aborted) return;
        setOrderAccounts(value.accounts);
        if (quickAccount) {
          const account = value.accounts.find(item => item.id === quickAccount.id);
          setDraft(current => ({ ...current, package_id: account?.subscription_package_id ? String(account.subscription_package_id) : '' }));
        }
      })
      .catch(error => { if (!controller.signal.aborted) setFormError(error.message); })
      .finally(() => { if (!controller.signal.aborted) setAccountsLoading(false); });
    return () => controller.abort();
  }, [dialog, token, quickAccount]);
  const orderAccount = orderAccounts.find(account => account.id === Number(dialog?.type === 'record' ? dialog.item.account_id : draft.account_id));
  const boundCardReady = Boolean(orderAccount?.payment_card_id && orderAccount.payment_card_available);
  const boundCardText = accountsLoading ? '正在读取账号绑定的付款卡…' : !orderAccount ? (dialog?.type === 'record' ? '订单关联账号不可用' : '请先选择账号') : !orderAccount.payment_card_id ? '未绑定付款卡' : `${orderAccount.payment_card_label || '付款卡'} · •••• ${orderAccount.payment_card_last4 || '—'}${orderAccount.payment_card_available ? '' : '（不可用）'}`;
  const selectedPackage = packages.find(pkg => pkg.id === Number(draft.package_id));
  const quickReady = !quickAccount || Boolean(orderAccount && selectedPackage?.enabled && selectedPackage.months === 1 && selectedPackage.price_ready !== false);
  const quickStart = orderAccount?.renewal_date || new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date());
  const quickEnd = (() => {
    const [year, month, day] = quickStart.split('-').map(Number);
    return new Date(Date.UTC(year, month, Math.min(day, new Date(Date.UTC(year, month + 1, 0)).getUTCDate()))).toISOString().slice(0, 10);
  })();
  const closeDialog = () => { if (quickAccount) onQuickClose(); else setDialog(null); };
  function open(type, item) {
    pending.current = null; setCollectionQuote(null); setEvidenceBusy(false); setFormError(''); setDialog({ type, item });
    setDraft(type === 'package' ? { plan: 'plus', region: 'PH', currency: 'PHP', months: 1, enabled: true, wallet_tokens: 0, auto_usd: false, ...item } : { received_currency: 'CNY', received_amount: '', order_source: item?.order_source || '' });
  }
  const field = (key, value) => {
    if (['received_currency', 'received_amount', 'package_id'].includes(key)) setCollectionQuote(null);
    setDraft(current => ({ ...current, [key]: value }));
  };
  async function save(event) {
    event.preventDefault(); if (writing.current || evidenceBusy) return; writing.current = true; setBusy(true); setFormError('');
    try {
      const { type, item } = dialog;
      if (['create', 'record', 'refund'].includes(type) && !hasEvidence(draft.evidence)) throw new Error('请填写核对说明或添加凭据图片');
      if (quickAccount && !quickReady) throw new Error('请先为账号选择已上架且价格可用的一个月套餐');
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
        if (quickAccount) Object.assign(body, { quick_month: true, expected_renewal_date: orderAccount.renewal_date || '' });
      } else { path += '/' + item.id; body = { ...values, action: type === 'refund' ? 'refund_note' : type, version: item.version }; }
      if (['create', 'record'].includes(type)) {
        if (accountsLoading || !boundCardReady) throw new Error('请先在账号管理中绑定可用的付款卡');
        Object.assign(body, { order_source: draft.order_source || '', card_id: orderAccount.payment_card_id, reference: values.reference, evidence: draft.evidence });
        if (draft.received_amount) Object.assign(body, { received_currency: draft.received_currency, received_amount: draft.received_amount, collection_rate_id: collectionQuote.exchange_rate.batch?.id || 0 });
        else { delete body.received_currency; delete body.received_amount; }
      }
      if (body && type !== 'package') {
        const fingerprint = JSON.stringify(body);
        if (pending.current?.fingerprint !== fingerprint) pending.current = { fingerprint, key: crypto.randomUUID() };
        body.request_key = pending.current.key;
      }
      await request(path, token, { method, body });
      pending.current = null;
      if (quickAccount) { onQuickCreated(); return; }
      setDialog(null); refresh(v => v + 1);
    } catch (e) { setFormError(e.message); } finally { writing.current = false; setBusy(false); }
  }
  async function details(order) {
    try { setHistory(await request('/orders/' + order.id, token)); } catch (e) { setError(e.message); }
  }
  return <>{!quickAccount && <section className="account-section">
    <div className="section-title"><h2>{packagesOnly ? '充值套餐' : '充值订单'}</h2><div className="browser-buttons"><button className="outline small" onClick={() => refresh(v => v + 1)}>刷新</button>{(!packagesOnly || data.can_manage) && <button className="primary small" onClick={() => open(packagesOnly ? 'package' : 'create')}>{packagesOnly ? '新增套餐' : '录入充值订单'}</button>}{!packagesOnly && <button className="outline small" onClick={() => downloadCSV('/orders/export?' + new URLSearchParams({ q: query, status }), token, '充值订单.csv').catch(e => setError(e.message))}>导出订单</button>}</div></div>
    {packagesOnly ? exchangeRate && <p className="muted">1 PHP = {Number(exchangeRate.rate).toFixed(6)} USD{exchangeRate.cny_rate && <> / {Number(exchangeRate.cny_rate).toFixed(6)} CNY</>} · 更新于 {formatUTC8(exchangeRate.synced_at)} · <a href="https://www.exchangerate-api.com" target="_blank" rel="noreferrer">ExchangeRate-API</a>{data.exchange_rate_fresh === false && <span className="danger"> · 汇率已过期，等待同步</span>}</p> : <p className="muted">选择账号和套餐，使用账号绑定的付款卡录入订单与支出。</p>}
    {!packagesOnly && <div className="address-search recharge-search"><input aria-label="搜索充值订单" placeholder="搜索订单号、来源、账号或备注" value={query} onChange={e => { setQuery(e.target.value); setPage(1); }} /><Select label="订单状态" value={status} onChange={value => { setStatus(value); setPage(1); }} options={[{ value: '', label: '全部状态' }, ...Object.entries(orderStatuses).map(([value, label]) => ({ value, label }))]} /></div>}
    {error && <p className="error" role="alert">{error}</p>}
    {loading ? <p role="status">正在加载…</p> : packagesOnly ? <DataTable searchQuery={query} label="充值套餐" columns={['名称', '套餐 / 地区', '原币价格', '售价 USD', '人民币 CNY', '代币价格', '周期', '状态', '操作']} empty={!data.packages?.length && '尚未配置套餐。请根据实际可购买的套餐设置价格。'}>{(data.packages || []).map(p => <tr key={p.id}><td>{p.name}</td><td>{plans.find(v => v.value === p.plan)?.label} / {p.region}</td><td>{p.currency} {(p.original_amount_minor / 100).toFixed(2)}</td><td>{p.auto_usd && !p.sale_usd_minor ? '—' : formatCardUSD(p.sale_usd_minor)}{p.auto_usd && <small className={'cell-secondary' + (p.price_ready === false ? ' danger' : '')}>{p.price_ready === false ? '等待汇率同步' : '每日汇率折算'}</small>}</td><td>{p.sale_cny_minor == null ? '—' : cnyFormatter.format(p.sale_cny_minor / 100)}{!p.cny_price_ready && <small className="cell-secondary danger">等待汇率同步</small>}</td><td>{p.wallet_tokens || '不启用'}</td><td>{p.months} 个月</td><td>{p.enabled ? '上架' : '下架'}</td><td>{data.can_manage && <div className="row-actions"><button className="outline small" onClick={() => open('package', p)}>编辑</button><button className="text-btn danger" onClick={() => open('delete-package', p)}>删除</button></div>}</td></tr>)}</DataTable> : <DataTable searchQuery={query} label="充值订单" columns={['订单 / 账号', '订单来源', '套餐 / 周期', '订单状态', '客户收款', '官网成本 USD', '毛利润 / 毛利率', '开通状态', '操作']} empty={!data.orders.length && '暂无充值订单。'}>{data.orders.map(o => <tr key={o.id}><td className="table-text">{o.account_email}<small className="cell-secondary">{o.order_no}</small></td><td className="table-text">{o.order_source || "—"}</td><td>{o.package_snapshot.name}<small className="cell-secondary">{o.period_start} 至 {o.period_end}</small></td><td><span className={'order-status order-status-' + o.order_status}>{orderStatuses[o.order_status] || '正常'}</span></td><td>{payment[o.payment_status]}<ReceiptSummary order={o} />{o.refunded_usd_minor > 0 && <small className="cell-secondary">已退 {formatCardUSD(o.refunded_usd_minor)}</small>}</td><td>{formatCardUSD(o.cost_usd_minor)}</td><td><ProfitSummary profit={o.profit} currency={o.received_currency} order={o} /></td><td>{fulfillment[o.fulfillment_status]}{o.failure_reason && <small className="cell-secondary danger">{o.failure_reason}</small>}</td><td><div className="row-actions"><button className="outline small" onClick={() => details(o)}>详情</button>{canOperate(o) && <>{data.can_finance && !o.cost_usd_minor && <button className="outline small" onClick={() => open('record', o)}>补录订单</button>}{data.can_refund && ['paid', 'partial_refund'].includes(o.payment_status) && <button className="outline small" onClick={() => open('refund', o)}>退款</button>}{data.can_manage && <button className="outline small" onClick={() => open('discard', o)}>废弃</button>}</>}</div></td></tr>)}</DataTable>}
    {!packagesOnly && <Pagination page={page} pageSize={pageSize} total={data.total} onPageChange={setPage} onPageSizeChange={setPageSize} disabled={loading} />}
    </section>}
    {dialog && <Dialog className={['create', 'record'].includes(dialog.type) ? 'order-record-dialog' : ''} title={quickAccount ? '快速创建一个月订单' : dialog.type === 'package' ? '配置充值套餐' : dialog.type === 'delete-package' ? '确认删除套餐' : dialog.type === 'create' ? '录入充值订单' : actionLabels[dialog.type]} onClose={() => { if (!busy) closeDialog(); }}><form className={['create', 'record'].includes(dialog.type) ? 'order-record-form' : undefined} onSubmit={save}>
      {dialog.item?.order_no && <p>{dialog.item.account_email} · {dialog.item.order_no}</p>}
      {dialog.type === 'package' && <><label>套餐名称<input name="name" defaultValue={draft.name} required maxLength={100} /></label><div className="proxy-fields"><label>官方套餐<Select label="官方套餐" value={draft.plan} onChange={v => field('plan', v)} options={plans} /></label><label>地区<input name="region" defaultValue={draft.region} required maxLength={80} /></label><label>美元定价<Select label="美元定价方式" value={String(draft.auto_usd)} onChange={v => setDraft(current => ({ ...current, auto_usd: v === 'true', ...(v === 'true' ? { currency: 'PHP' } : {}) }))} options={[{ value: 'false', label: '固定 USD 售价' }, { value: 'true', label: 'PHP 每日汇率折算' }]} /></label><label>原币种<input name="currency" value={draft.currency} onChange={e => field('currency', e.target.value)} readOnly={draft.auto_usd} required pattern="[A-Z]{3}" maxLength={3} /></label><label>原币价格<input name="original_amount" onChange={e => field('original_amount_minor', Math.round(Number(e.target.value) * 100))} type="number" min="0.01" step="0.01" defaultValue={draft.original_amount_minor ? (draft.original_amount_minor / 100).toFixed(2) : ''} required /></label><label>{draft.auto_usd ? '折算 USD' : '售价 USD'}{draft.auto_usd ? <input aria-label="折算 USD" value={exchangeRate && draft.original_amount_minor > 0 ? (draft.original_amount_minor / 100 * Number(exchangeRate.rate)).toFixed(2) : '等待汇率同步'} readOnly /> : <input name="sale_usd" type="number" min="0.01" step="0.01" defaultValue={draft.sale_usd_minor ? (draft.sale_usd_minor / 100).toFixed(2) : ''} required />}</label><label>代币价格<input name="wallet_tokens" type="number" min="0" step="1" defaultValue={draft.wallet_tokens} required /></label><label>周期（月）<input name="months" type="number" min="1" max="36" defaultValue={draft.months} required /></label><label>上架状态<Select label="套餐上架状态" value={String(draft.enabled)} onChange={v => field('enabled', v === 'true')} options={[{ value: 'true', label: '上架' }, { value: 'false', label: '下架' }]} /></label></div><label>备注<textarea name="notes" defaultValue={draft.notes} maxLength={2000} /></label></>}
      {dialog.type === 'create' && (quickAccount ? <>
        <label>账号<input aria-label="月订单账号" value={orderAccount?.email || quickAccount.email} readOnly /></label>
        <label>套餐<input aria-label="月订单套餐" value={selectedPackage ? `${selectedPackage.name} · ${selectedPackage.region} · ${formatCardUSD(selectedPackage.sale_usd_minor)} / ${selectedPackage.months}个月` : '请先设置账号产品选型'} readOnly /></label>
        {!accountsLoading && !quickReady && <p className="error" role="alert">请先在账号产品选型中选择已上架且价格可用的一个月套餐。</p>}
        <p className="muted quick-order-period">续订日期：{quickStart} → {quickEnd}（记账成功后更新）</p>
      </> : <><label>账号<Select disabled={accountsLoading || busy} label="订单账号" value={draft.account_id || ''} onChange={v => field('account_id', v)} options={[{ value: '', label: '请选择账号' }, ...orderAccounts.map(a => ({ value: String(a.id), label: a.email }))]} /></label><label>套餐<Select label="订单套餐" value={draft.package_id || ''} onChange={v => field('package_id', v)} options={[{ value: '', label: '请选择上架套餐' }, ...packages.filter(p => p.enabled && p.price_ready !== false).map(p => ({ value: String(p.id), label: `${p.name} · ${formatCardUSD(p.sale_usd_minor)} / ${p.months}个月` }))]} /></label><label>订单备注<textarea name="notes" rows={2} maxLength={2000} /></label></>)}

      {['create', 'record'].includes(dialog.type) && <><label>付款卡（账号已绑定）<input aria-label="订单付款卡" value={boundCardText} readOnly />{!accountsLoading && orderAccount && !boundCardReady && <small className="danger">请先在账号管理中绑定可用的付款卡，再重新打开此窗口。</small>}</label>
        <label>扣款 USD<input aria-label="扣款 USD" value={(() => { const price = dialog.item?.sale_usd_minor ?? packages.find(p => p.id === Number(draft.package_id))?.sale_usd_minor; return price ? (price / 100).toFixed(2) : ''; })()} readOnly /></label>
        {needsCollection ? <CollectionFields currencyReadOnly={Boolean(quickAccount)} disabled={busy} token={token} order={dialog.item || { package_id: draft.package_id }} draft={draft} onChange={field} onQuoteChange={setCollectionQuote} /> : <CollectionDetails order={dialog.item} />}
      </>}
      {['create', 'record'].includes(dialog.type) && <label className="order-source-field">订单来源<Select name="order_source" label="订单来源" value={draft.order_source || ''} onChange={value => field('order_source', value)} disabled={busy} options={[{ value: '', label: '未填写' }, ...Array.from(new Set([...(data.sources || []), draft.order_source].filter(Boolean))).map(value => ({ value, label: value }))]} searchPlaceholder="输入或搜索订单来源…" createLabel="使用输入的来源" onCreate={value => { const source = value.trim().replace(/\s+/g, ' '); if (!source || Array.from(source).length > 80) { setFormError('请输入 1 至 80 字的订单来源'); return; } field('order_source', source); setFormError(''); }} /></label>}
      {['create', 'record', 'refund'].includes(dialog.type) && <label>交易号 / 凭证编号<input name="reference" required maxLength={200} /></label>}
      {['refund', 'discard'].includes(dialog.type) && <label>原因<textarea name="reason" maxLength={2000} required /></label>}
      {['create', 'record', 'refund'].includes(dialog.type) ? <EvidenceEditor value={draft.evidence || ''} onChange={value => field('evidence', value)} onBusyChange={setEvidenceBusy} disabled={busy} /> : null}

      {dialog.type === 'discard' && <p>确认废弃此订单？废弃后只能查看详情，可重新创建同周期订单；已有资金记录保留，余额不退回。</p>}
      {dialog.type === 'cancel' && <p>确认取消此订单？仅支持未收款，或已全额退款且没有未冲正官网成本的订单。</p>}
      {dialog.type === 'delete-package' && <p>确认删除「{dialog.item.name}」？套餐将隐藏，历史订单快照保留。</p>}
      {quickAccount && error && <p className="error" role="alert">{error}</p>}{formError && <p className="error" role="alert">{formError}</p>}<div className="browser-buttons"><button className="primary" disabled={busy || evidenceBusy || (['create', 'record'].includes(dialog.type) && (accountsLoading || !quickReady || !boundCardReady || !collectionReady))}>{busy ? '处理中…' : ['create', 'record'].includes(dialog.type) ? '确认录入并记账' : '确认并保存'}</button><button className="outline" type="button" disabled={busy} onClick={closeDialog}>取消</button></div>
    </form></Dialog>}
    {history && <Dialog title="订单详情与操作记录" onClose={() => setHistory(null)}>{history.order.order_source && <p>订单来源：{history.order.order_source}</p>}<p>{history.order.order_no} · {history.order.account_email}</p><p>订单状态：{orderStatuses[history.order.order_status] || '正常'}<br />收款凭证：{history.order.payment_reference || '—'}<br />官网交易：{history.order.purchase_reference || '—'}<br />生效日期：{history.order.cost_usd_minor > 0 ? history.order.period_start : '尚未扣款'}<br />到期日期：{history.order.cost_usd_minor > 0 ? history.order.period_end : '—'}</p><CollectionDetails order={history.order} /><EvidenceView value={history.order.evidence} /><DataTable searchQuery={query} label="订单操作记录" columns={['时间', '操作者', '操作', '说明']} stickyActions={false}>{history.events.map(e => <tr key={e.id}><td>{formatUTC8(e.created_at)}</td><td>{e.actor_id}</td><td>{eventLabels[e.action] || e.action}</td><td className="table-text">{e.after_data?.input?.amount_usd && <p>金额 USD：{e.after_data.input.amount_usd}</p>}{e.after_data?.input?.reason && <p>{e.after_data.input.reason}</p>}<EvidenceView value={e.after_data?.input?.evidence} />{!e.after_data?.input && '订单已创建'}</td></tr>)}</DataTable></Dialog>}
  </>;
}

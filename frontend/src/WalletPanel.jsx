import React, { useState, useRef } from 'react';
import { ArrowUpRight, Coins, RefreshCw } from 'lucide-react';
import Dialog from './Dialog';
import { Link } from './router';
import { request } from './api';

const statuses = { pending: '等待支付确认', paid: '已到账', expired: '已过期', failed: '支付失败' };
const money = value => new Intl.NumberFormat('zh-CN', { style: 'currency', currency: 'USD' }).format(value / 100);

export default function WalletPanel({ wallet, token, refresh, user, notice }) {
  const [refund,setRefund]=useState(null), [refundError,setRefundError]=useState('');
  const [selected, setSelected] = useState(100);
  const [quantity, setQuantity] = useState('1');
  const [history, setHistory] = useState('orders');
  const [syncing, setSyncing] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const pending = useRef(null);
  const count = Number(quantity);
  const validQuantity = Number.isInteger(count) && count >= 1 && count <= (wallet?.max_topup_quantity || 100);
  async function checkout() {
    if (!validQuantity) return;
    setBusy(true); setError('');
    if (!pending.current || pending.current.amount !== selected || pending.current.quantity !== count) pending.current = { amount: selected, quantity: count, key: crypto.randomUUID() };
    try {
      const result = await request('/wallet/topups', token, { method: 'POST', body: { amount_minor: selected, quantity: count, request_key: pending.current.key } });
      window.location.assign(result.checkout_url);
    } catch (error) { setError(error.message); setBusy(false); }
  }
  async function syncOrder(orderNo) {
    setSyncing(orderNo); setError('');
    try {
      await request('/wallet/topups/' + encodeURIComponent(orderNo) + '/sync', token, { method: 'POST' });
      await refresh();
    } catch (error) { setError(error.message); } finally { setSyncing(''); }
  }
  if (!wallet) return <p className="muted" role="status">正在加载钱包…</p>;
  const option = wallet.topup_options.find(item => item.amount_minor === selected);
  return <section className="wallet-panel" aria-label="我的钱包">
    <div className="wallet-grid">
      <div className="wallet-balance"><span className="eyebrow">AITOK WALLET</span><h2>你的每一份灵感，都有余量。</h2>
        <div className="balance-value"><Coins size={28} /><strong>{wallet.balance.toLocaleString()}</strong><span>代币</span></div>
        <p>1 美元 = {wallet.tokens_per_usd} 代币</p>{['admin', 'super_admin'].includes(user.role) && <Link to="/admin/orders">创建充值订单并使用钱包付款</Link>}
      </div>
      <div className="topup-card"><h3>充值代币</h3><p className="muted">通过 Stripe 安全支付，支付确认后自动到账。</p>
        <div className="topup-options" role="radiogroup" aria-label="充值金额">{wallet.topup_options.map(item => <button key={item.amount_minor} role="radio" aria-checked={selected === item.amount_minor} disabled={busy} className={selected === item.amount_minor ? 'selected' : ''} onClick={() => setSelected(item.amount_minor)}><strong>{item.tokens} <small>代币</small></strong><span>{money(item.amount_minor)}</span></button>)}</div>
        <div className="quantity-row"><label htmlFor="topup-quantity">充值数量<input id="topup-quantity" type="number" min="1" max={wallet.max_topup_quantity || 100} step="1" inputMode="numeric" value={quantity} disabled={busy} onChange={event => setQuantity(event.target.value)} /></label><p className="muted">每份 {money(selected)} · {option?.tokens ?? 0} 代币</p></div>
        {!validQuantity && <p className="error" role="alert">请输入 1 到 {wallet.max_topup_quantity || 100} 的整数数量。</p>}
        {!wallet.stripe_enabled && <p className="notice">{user.role === 'super_admin' ? '请配置 Stripe 密钥和 Webhook 后开通充值。' : '充值暂未开通，请联系管理员。'}</p>}
        {error && <p className="error" role="alert">{error}</p>}
        <button className="primary full" disabled={busy || !wallet.stripe_enabled || !validQuantity} onClick={checkout}>{busy ? '正在前往支付…' : `充值 ${money(validQuantity ? selected * count : 0)} · 获得 ${validQuantity ? (option?.tokens ?? 0) * count : 0} 代币`}<ArrowUpRight size={16} /></button>
      </div>
    </div>
    {notice && <p className="notice" role="status">{notice}</p>}
    {wallet.sync_warning && <p className="notice" role="status">{wallet.sync_warning}</p>}
    <div className="section-title"><h2>交易记录</h2><button className="text-btn" disabled={!!syncing} onClick={async () => { setSyncing('all'); try { await refresh(true); } catch (e) { setError(e.message); } finally { setSyncing(''); } }}><RefreshCw size={15} />{syncing === 'all' ? '正在核对…' : '刷新并核对支付'}</button></div>
    <div className="history-tabs" role="tablist" aria-label="交易记录分类">{[['orders','充值记录'],['renewals','账号扣款记录'],['ledger','全部流水']].map(([key,title]) => <button key={key} role="tab" aria-selected={history === key} className={history === key ? 'active' : ''} onClick={() => setHistory(key)}>{title}</button>)}</div>
    {history === 'orders' && <div className="table-wrap"><table><thead><tr><th>时间 / 订单号</th><th>单价</th><th>数量</th><th>总金额</th><th>代币</th><th>状态</th><th>操作</th></tr></thead><tbody>{wallet.orders.map(order => <tr key={order.order_no}><td>{new Date(order.created_at).toLocaleString('zh-CN')}<small className="order-number">{order.order_no}</small></td><td>{money(order.unit_amount_minor ?? order.amount_minor)}</td><td>{order.quantity ?? 1}</td><td>{money(order.amount_minor)}</td><td>{order.tokens}</td><td className={order.status === 'paid' ? 'credit' : ''}>{statuses[order.status] || order.status}{order.refunded_minor>0 && <small className="cell-secondary">已退 {money(order.refunded_minor)}</small>}{order.dispute_status && <small className="danger">争议：{order.dispute_status}</small>}</td><td>{order.status === 'pending' ? <button className="text-btn" disabled={!!syncing} onClick={() => syncOrder(order.order_no)}>{syncing === order.order_no ? '核对中…' : '核对支付'}</button> : order.status==='paid' && (order.refunded_minor||0)<order.amount_minor ? <button className="text-btn" onClick={()=>{setRefundError('');setRefund({order,key:crypto.randomUUID(),fingerprint:''})}}>申请原路退款</button> : '—'}</td></tr>)}</tbody></table>{wallet.orders.length === 0 && <p className="table-empty">暂无充值记录</p>}</div>}
    {history === 'renewals' && <div className="table-wrap"><table><thead><tr><th>扣款时间</th><th>ChatGPT 账号</th><th>续订时长</th><th>续订至</th><th>扣除代币</th><th>扣款后余额</th></tr></thead><tbody>{(wallet.renewals || []).map((item,index) => <tr key={index}><td>{new Date(item.created_at).toLocaleString('zh-CN')}</td><td>{item.account_label}<small className="order-number">账号 #{item.account_id}</small></td><td>{item.months ? item.months + ' 个月' : '—'}</td><td>{item.renewal_date}</td><td>-{item.tokens}</td><td>{item.balance_after ?? '—'}</td></tr>)}</tbody></table>{!wallet.renewals?.length && <p className="table-empty">暂无历史账号扣款记录。</p>}</div>}
    {history === 'ledger' && <div className="table-wrap"><table><thead><tr><th>交易时间</th><th>类型</th><th>说明</th><th>代币变动</th><th>变动后余额</th></tr></thead><tbody>{wallet.ledger.map(item => <tr key={item.id}><td>{new Date(item.created_at).toLocaleString('zh-CN')}</td><td>{item.kind === 'stripe_topup' ? '充值入账' : item.kind === 'renewal' ? '账号续订' : item.kind === 'order_consumption' ? '账号消费' : item.kind}</td><td>{item.description}</td><td className={item.amount > 0 ? 'credit' : ''}>{item.amount > 0 ? '+' : ''}{item.amount}</td><td>{item.balance_after}</td></tr>)}</tbody></table>{wallet.ledger.length === 0 && <p className="table-empty">暂无钱包流水</p>}</div>}
    {refund&&<Dialog title="申请钱包充值退款" onClose={()=>{if(!busy)setRefund(null)}}><form onSubmit={async e=>{e.preventDefault();if(busy)return;const fields=Object.fromEntries(new FormData(e.currentTarget));const fingerprint=JSON.stringify(fields);const key=refund.fingerprint===fingerprint?refund.key:crypto.randomUUID();setRefund(current=>({...current,key,fingerprint}));setBusy(true);setRefundError('');try{await request('/wallet/topups/'+encodeURIComponent(refund.order.order_no)+'/sync',token,{method:'POST'});await request('/wallet/topups/'+encodeURIComponent(refund.order.order_no)+'/refund',token,{method:'POST',body:{...fields,request_key:key}});setRefund(null);await refresh()}catch(error){setRefundError(error.message)}finally{setBusy(false)}}}><p>{refund.order.order_no}；退款需管理员审核。渠道退款成功后回收对应代币。</p><label>退款金额 USD<input name="amount_usd" type="number" min="0.01" step="0.01" max={(refund.order.amount_minor-(refund.order.refunded_minor||0))/100} required/></label><label>退款原因<textarea name="reason" required maxLength={1000}/></label>{refundError&&<p className="error">{refundError}</p>}<div className="browser-buttons"><button className="primary" disabled={busy}>确认申请</button><button className="outline" type="button" disabled={busy} onClick={()=>setRefund(null)}>取消</button></div></form></Dialog>}
  </section>;
}

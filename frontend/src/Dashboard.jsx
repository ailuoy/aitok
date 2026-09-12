import React, { useState, useEffect, useCallback, useRef } from 'react';
import { Plus, Trash2, MessageSquare, CalendarDays, Coins, RefreshCw, KeyRound, X } from 'lucide-react';
import { request } from './api';
import WalletPanel from './WalletPanel';
import { Link, navigate } from './router';

function Dialog({ title, onClose, children }) {
  const ref = useRef(null);
  useEffect(() => { const dialog = ref.current; dialog.showModal(); return () => dialog.close(); }, []);
  return <dialog ref={ref} className="workspace-dialog" aria-labelledby="dialog-title" onCancel={event => { event.preventDefault(); onClose(); }}>
    <button className="close" aria-label="关闭" onClick={onClose}><X size={20} /></button><h2 id="dialog-title">{title}</h2>{children}
  </dialog>;
}

export default function Dashboard({ user, token, accounts, setAccounts, route }) {
  const tab = route.pathname === '/wallet' ? 'wallet' : 'accounts';
  const [returnNotice, setReturnNotice] = useState('');
  const [wallet, setWallet] = useState(null);
  const [dialog, setDialog] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [dialogError, setDialogError] = useState('');
  const [message, setMessage] = useState('');
  const admin = user?.role === 'super_admin';
  const refresh = useCallback(async (syncPending = false) => {
    const [accountData, initialWallet] = await Promise.all([request('/accounts', token), request('/wallet', token)]);
    let walletData = initialWallet;
    let warning = '';
    if (syncPending) {
      const pending = initialWallet.orders.filter(item => item.status === 'pending').slice(0, 10);
      if (pending.length) {
        const results = await Promise.allSettled(pending.map(item => request('/wallet/topups/' + encodeURIComponent(item.order_no) + '/sync', token, { method: 'POST' })));
        if (results.some(result => result.status === 'rejected')) warning = '部分订单暂时无法核对，请稍后点击“核对支付”。';
        walletData = await request('/wallet', token);
      }
    }
    setAccounts(accountData.accounts); setWallet({ ...walletData, sync_warning: warning }); setError('');
    return walletData;
  }, [token, setAccounts]);
  useEffect(() => { refresh(true).catch(e => setError(e.message)); }, [refresh]);
  const params = route.searchParams;
  const topupStatus = params.get('topup');
  const orderNo = params.get('order');
  const order = wallet?.orders.find(item => item.order_no === orderNo);
  useEffect(() => {
    if (topupStatus !== 'success' || ['paid', 'expired', 'failed'].includes(order?.status)) return;
    let active = true, timer, attempts = 0;
    const poll = async () => {
      try {
        const data = await refresh(true);
        if (data.orders.some(item => item.order_no === orderNo && item.status === 'paid')) return;
      } catch (error) { if (active) setError(error.message); }
      if (active && ++attempts < 20) timer = setTimeout(poll, 3000);
    };
    timer = setTimeout(poll, 3000);
    return () => { active = false; clearTimeout(timer); };
  }, [topupStatus, orderNo, order?.status, refresh]);
  const notice = order?.status === 'paid' ? '充值已到账，代币已计入钱包。' : order?.status === 'expired' ? '该充值订单已过期。' : order?.status === 'failed' ? '该订单支付失败，请重新发起充值。' : topupStatus === 'cancelled' ? '已返回钱包；仅在支付确认后入账。' : topupStatus === 'success' ? '正在确认支付结果。若尚未到账，可稍后刷新；请勿重复支付。' : returnNotice;
  useEffect(() => {
    if (tab !== 'wallet' || !topupStatus) return;
    if (topupStatus === 'cancelled' || ['paid', 'expired', 'failed'].includes(order?.status)) {
      setReturnNotice(notice);
      navigate('/wallet', { replace: true });
    }
  }, [tab, topupStatus, order?.status, notice]);
  function open(type, account) { setDialogError(''); setDialog({ type, account, key: crypto.randomUUID() }); }
  async function submit(event) {
    event.preventDefault(); setBusy(true); setDialogError('');
    const fields = new FormData(event.currentTarget);
    try {
      if (dialog.type === 'add') {
        const raw = fields.get('session_json');
        let session;
        try { session = JSON.parse(raw); } catch { throw new Error('Session JSON 格式不正确，请输入完整 JSON 对象'); }
        if (!session || typeof session !== 'object' || Array.isArray(session) || Object.keys(session).length === 0) throw new Error('Session JSON 必须是非空 JSON 对象');
        await request('/accounts', token, { method: 'POST', body: { label: fields.get('label'), email: fields.get('email'), session_json: raw } });
        setMessage('账号已保存');
      } else if (dialog.type === 'date') {
        await request(`/accounts/${dialog.account.id}/renewal-date`, token, { method: 'PATCH', body: { renewal_date: fields.get('renewal_date') } });
        setMessage('续订日期已更新');
      } else if (dialog.type === 'renew') {
        const result = await request(`/accounts/${dialog.account.id}/renew`, token, { method: 'POST', body: { request_key: dialog.key, expected_cost: wallet.renewal_token_cost, expected_months: wallet.renewal_months } });
        setMessage(`续订成功，新的续订日期为 ${result.renewal_date}`);
      } else if (dialog.type === 'delete') {
        await request(`/accounts/${dialog.account.id}`, token, { method: 'DELETE' });
        setMessage('账号已删除');
      }
      setDialog(null);
      refresh().catch(e => setError(e.message));
    } catch (error) { setDialogError(error.message); } finally { setBusy(false); }
  }
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date());
  const activeCount = accounts.filter(a => a.renewal_date && a.renewal_date > today).length;
  return <main className="dashboard">
    <div className="dash-head"><div><span className="eyebrow">AITOK WORKSPACE</span><h1>{admin ? '账号与续订管理' : '你好，准备好开始了吗？'}</h1><p>{admin ? '管理全部 ChatGPT 账号的续订日期。' : '管理你的 ChatGPT 账号、会员日期和钱包。'}</p></div><button className="primary" onClick={() => open('add')}><Plus size={17} />添加账号</button></div>
    <div className="stats"><div><span>{admin ? '全部账号' : '我的账号'}</span><b>{accounts.length}</b></div><div><span>有效会员账号</span><b>{activeCount}</b></div><div><span>我的钱包</span><button className="balance-link" onClick={() => navigate('/wallet')}><b>{wallet?.balance ?? '—'}</b> 代币 <Coins size={16} /></button></div></div>
    <nav className="workspace-tabs" aria-label="工作台"><Link aria-current={tab === 'accounts' ? 'page' : undefined} className={tab === 'accounts' ? 'active' : ''} to="/accounts"><MessageSquare size={16} />ChatGPT 账号</Link><Link aria-current={tab === 'wallet' ? 'page' : undefined} className={tab === 'wallet' ? 'active' : ''} to="/wallet"><Coins size={16} />钱包与充值</Link></nav>
    {error && <p className="error" role="alert">{error}<button className="text-btn" onClick={() => refresh().catch(e => setError(e.message))}>重试</button></p>}
    {message && <p className="success" role="status">{message}</p>}
    {tab === 'wallet' ? <WalletPanel wallet={wallet} token={token} refresh={refresh} user={user || {}} notice={notice} /> : <section className="account-section">
      <div className="section-title"><h2>{admin ? '全部 ChatGPT 账号' : '你的 ChatGPT 账号'}</h2><button className="text-btn" onClick={() => refresh().catch(e => setError(e.message))}><RefreshCw size={15} />刷新</button></div>
      {accounts.length === 0 ? <div className="empty"><div className="empty-icon"><KeyRound size={24} /></div><h3>还没有添加账号</h3><p>粘贴 Session JSON，保存你的 ChatGPT 账号。</p><button className="outline" onClick={() => open('add')}>添加第一个账号</button></div> : <div className="account-list">{accounts.map(account => <div className="account" key={account.id}>
        <div className="account-avatar"><MessageSquare size={20} /></div><div className="account-info"><b>{account.label}</b><span>{account.email}</span>{admin && <span>所属用户：{account.owner_email}</span>}<span>{account.has_session ? 'Session 已保存' : '未保存 Session'}</span></div>
        <div className="renewal-info"><span>续订日期</span><strong>{account.renewal_date || '未设置'}</strong><small className={account.renewal_date && account.renewal_date > today ? 'credit' : 'muted'}>{!account.renewal_date ? '待续订' : account.renewal_date > today ? '会员有效' : '已到续订日'}</small></div>
        <div className="account-actions">{admin && <button className="outline small" onClick={() => open('date', account)}><CalendarDays size={15} />设置日期</button>}{account.user_id === user?.id && <><button className="primary small" disabled={!wallet} onClick={() => open('renew', account)}>代币续订</button><button className="icon-btn" aria-label={`删除 ${account.label}`} onClick={() => open('delete', account)}><Trash2 size={17} /></button></>}</div>
      </div>)}</div>}
    </section>}
    {dialog && <Dialog title={{ add: '添加 ChatGPT 账号', date: '设置续订日期', renew: '续订账号会员', delete: '删除账号' }[dialog.type]} onClose={() => { if (!busy) setDialog(null); }}><form onSubmit={submit}>
      {dialog.type === 'add' && <><p className="muted">输入完整的 Session JSON，账号信息将加密保存。</p><label>账号名称<input name="label" required maxLength={120} placeholder="例如：工作账号" autoFocus /></label><label>登录邮箱<input name="email" type="email" required placeholder="chatgpt@example.com" autoComplete="off" /></label><label>Session JSON<textarea name="session_json" required rows={7} maxLength={240000} placeholder={'{\n  "user": { "email": "you@example.com" },\n  "accessToken": "…"\n}'} autoComplete="off" autoCapitalize="off" spellCheck={false} /></label></>}
      {dialog.type === 'date' && <><p className="muted">{dialog.account.label} · {dialog.account.email}</p><label>下次续订日期<input type="date" name="renewal_date" defaultValue={dialog.account.renewal_date || ''} min="2000-01-01" max="9999-12-31" autoFocus /></label><p className="muted">清空日期可取消设置；此操作不扣除钱包代币。</p></>}
      {dialog.type === 'renew' && <><p className="muted">为 {dialog.account.label} 续订 {wallet.renewal_months} 个月</p><div className="renewal-summary"><span>所需代币<strong>{wallet.renewal_token_cost}</strong></span><span>钱包余额<strong>{wallet.balance}</strong></span></div><p className="muted">未过期账号从原续订日期延长，已过期账号从今天起算。续订更新 AiTok 平台会员日期。</p>{wallet.balance < wallet.renewal_token_cost && <p className="notice">余额不足，请先到钱包充值。</p>}</>}
      {dialog.type === 'delete' && <p className="muted">确认删除「{dialog.account.label}」及其保存的 Session？钱包流水会保留。</p>}
      {dialogError && <p className="error" role="alert">{dialogError}</p>}
      <button className="primary full" disabled={busy || (dialog.type === 'renew' && wallet.balance < wallet.renewal_token_cost)}>{busy ? '处理中…' : dialog.type === 'renew' ? `确认支付 ${wallet.renewal_token_cost} 代币` : dialog.type === 'delete' ? '确认删除' : '保存'}</button>
      {dialog.type === 'renew' && wallet.balance < wallet.renewal_token_cost && <button type="button" className="outline" onClick={() => { setDialog(null); navigate('/wallet'); }}>前往充值</button>}
    </form></Dialog>}
  </main>;
}

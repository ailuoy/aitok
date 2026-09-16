import Pagination from './Pagination';
import PaymentExceptions from './PaymentExceptions';
import RechargeManager, {downloadCSV} from './RechargeManager';
import OperationHistory from './OperationHistory';
import React, { useState, useEffect, useCallback } from 'react';
import { Plus, Trash2, CalendarDays, Coins, RefreshCw, KeyRound, Monitor } from 'lucide-react';
import { request } from './api';
import WalletPanel from './WalletPanel';
import SessionFields from './SessionFields';
import BrowserSession from './BrowserSession';
import LocalBrowserSession from './LocalBrowserSession';
import ProxyManager from './ProxyManager';
import { browserEnvironmentID, launcherRequest } from './localBrowser';
import { Link, navigate, canonicalAdminPath } from './router';
import UserManager from './UserManager';
import DataTable from './DataTable';
import Dialog from './Dialog';
import useLocalBrowsers, { browserRunning } from './useLocalBrowsers';
import AddressManager from './AddressManager';
import BankCardManager from './BankCardManager';
import Select from './Select';
import GroupManager from './GroupManager';
import CreateAccountGroup from './CreateAccountGroup';
import { formatUTC8 } from './time';

export default function Dashboard({ user, token, accounts, setAccounts, route }) {
  const [pageSize, setPageSize] = useState(20);
  const admin = ['super_admin', 'admin'].includes(user?.role);
  const tab = canonicalAdminPath(route.pathname).split('/')[2] || 'accounts';
  const [returnNotice, setReturnNotice] = useState('');
  const [wallet, setWallet] = useState(null);
  const [dialog, setDialog] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [dialogError, setDialogError] = useState('');
  const [message, setMessage] = useState('');
  const [proxyConfig, setProxyConfig] = useState(null);
  const [proxyError, setProxyError] = useState('');
  const [bindingAccount, setBindingAccount] = useState(null);
  useEffect(() => {
    let stopped = false, running = false;
    async function sync() {
      if (running || !user?.id || !admin) return;
      running = true;
      try {
        let data = await launcherRequest('/activity-export?cursor=0');
        const key = 'proxy-sync:' + location.origin + ':' + user.id + ':' + data.device_id;
        const cursor = Number(localStorage.getItem(key) || 0);
        if (cursor > 0) data = await launcherRequest('/activity-export?cursor=' + cursor);
        if (stopped) return;
        const events = data.events.filter(e => !e.environment_id || e.environment_id.includes(':user:' + user.id + ':account:'));
        if (events.length) await request('/proxy-activity', token, { method: 'POST', body: { device_id: data.device_id, events } });
        if (!stopped) localStorage.setItem(key, String(data.next_cursor));
      } catch {} finally { running = false; }
    }
    sync(); const timer = setInterval(sync, 30000);
    return () => { stopped = true; clearInterval(timer); };
  }, [token, user?.id, admin]);
  const localBrowsers = useLocalBrowsers(admin ? accounts : [], user?.id, token, setAccounts);
  const [accountQuery,setAccountQuery]=useState(''), [accountPageIndex,setAccountPageIndex]=useState(1),[accountPage,setAccountPage]=useState({accounts:[],total:0});
  const [groups, setGroups] = useState([]);
  const [groupFilter, setGroupFilter] = useState('all');
  useEffect(()=>{if(tab!=='accounts')return;const c=new AbortController();request('/accounts?'+new URLSearchParams({paged:'1',page:accountPageIndex,page_size:pageSize,q:accountQuery,group:groupFilter==='all'?'':groupFilter===''?'none':groupFilter}),token,{signal:c.signal}).then(v=>{if(!c.signal.aborted)setAccountPage(v)}).catch(e=>{if(!c.signal.aborted)setError(e.message)});return()=>c.abort()},[tab,pageSize,accountPageIndex,accountQuery,groupFilter,token,accounts]);
  useEffect(()=>{setAccountPageIndex(1)},[accountQuery,groupFilter]);
  const [groupManager, setGroupManager] = useState(false);
  const [creatingGroup, setCreatingGroup] = useState(null);
  const [groupBusy, setGroupBusy] = useState(null);
  const refreshGroups = useCallback(async () => { const data = await request('/account-groups', token); setGroups(data.groups); }, [token]);
  useEffect(() => { if (!admin) return; refreshGroups().catch(error => setError(error.message)); }, [refreshGroups, admin]);
  useEffect(() => { if (groupFilter && groupFilter !== 'all' && !groups.some(group => String(group.id) === groupFilter)) setGroupFilter('all'); }, [groups, groupFilter]);
  const refreshGrouping = async () => { await refreshGroups(); const data = await request('/accounts', token); setAccounts(data.accounts); };
  async function bindGroup(account, value) {
    setGroupBusy(account.id);
    try {
      const data = await request('/accounts/' + account.id + '/group', token, { method: 'PATCH', body: { group_id: value ? Number(value) : null } });
      setAccounts(current => current.map(item => item.id === account.id ? { ...item, group_id: data.group_id } : item));
      await refreshGroups(); setError(''); setMessage('账号分组已更新');
    } catch (error) { setError(error.message); } finally { setGroupBusy(null); }
  }
  const closeBrowserDialog = useCallback(() => setDialog(current => ['local-browser', 'browser'].includes(current?.type) ? null : current), []);
  async function toggleBrowser(account) {
    if (!browserRunning(localBrowsers.states[account.id])) { open('local-browser', account); return; }
    try { await localBrowsers.close(account.id); setError(''); }
    catch (error) { setError(error.message); }
  }
  const refreshProxies = useCallback(async () => {
    try { setProxyConfig(await launcherRequest('/proxies')); setProxyError(''); }
    catch (error) { setProxyError(error.message); }
  }, []);
  useEffect(() => { if (admin) refreshProxies(); }, [refreshProxies, tab, admin]);
  async function bindProxy(account, proxyID) {
    setBindingAccount(account.id);
    try {
      setProxyConfig(await launcherRequest('/browsers/' + encodeURIComponent(browserEnvironmentID(user.id, account.id)) + '/proxy', { method: 'PATCH', body: { proxy_id: proxyID || null } }));
      setMessage('代理选择已保存，下次打开账号时使用'); setError('');
    } catch (error) { setError(error.message); }
    finally { setBindingAccount(null); }
  }
  const refresh = useCallback(async (syncPending = false) => {
    if (!admin) { const data = await request('/accounts', token); setAccounts(data.accounts); setError(''); return {orders: []}; }
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
  }, [token, setAccounts, admin]);
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
      navigate('/admin/wallet', { replace: true });
    }
  }, [tab, topupStatus, order?.status, notice]);
  function open(type, account) { setDialogError(''); setDialog({ type, account, key: crypto.randomUUID() }); }
  async function submit(event) {
    event.preventDefault(); setBusy(true); setDialogError('');
    const fields = new FormData(event.currentTarget);
    try {
      if (dialog.type === 'add' || dialog.type === 'session') {
        const raw = fields.get('session_json');
        let session;
        try { session = JSON.parse(raw); } catch { throw new Error('Session JSON 格式不正确，请输入完整 JSON 对象'); }
        if (!session || typeof session !== 'object' || Array.isArray(session) || Object.keys(session).length === 0) throw new Error('Session JSON 必须是非空 JSON 对象');
        if (dialog.type === 'session') {
          await request(`/accounts/${dialog.account.id}/session`, token, { method: 'PATCH', body: { session_json: raw } });
          setMessage('Session 已更新，下次打开浏览器时生效');
        } else {
          await request('/accounts', token, { method: 'POST', body: { email: fields.get('email') || '', session_json: raw } });
          setMessage('账号已添加');
        }
      } else if (dialog.type === 'date') {
        await request(`/accounts/${dialog.account.id}/renewal-date`, token, { method: 'PATCH', body: { renewal_date: fields.get('renewal_date') } });
        setMessage('续订日期已更新');
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
  const visibleAccounts = accountPage.accounts;
  return <main className="dashboard">
    {admin && tab === 'accounts' && <div className="stats"><div><span>{admin ? '全部账号' : '我的账号'}</span><b>{accounts.length}</b></div><div><span>有效会员账号</span><b>{activeCount}</b></div><div><span>我的钱包</span><button className="balance-link" onClick={() => navigate('/admin/wallet')}><b>{wallet?.balance ?? '—'}</b> 代币 <Coins size={16} /></button></div></div>}

    {error && <p className="error" role="alert">{error}<button className="text-btn" onClick={() => refresh().catch(e => setError(e.message))}>重试</button></p>}
    {message && <p className="success" role="status">{message}</p>}
    {localBrowsers.loginError && <p className="notice" role="status">{localBrowsers.loginError}</p>}
    {tab==='payment-exceptions' ? <PaymentExceptions token={token}/> : tab === 'orders' || tab==='packages' ? <RechargeManager key={tab} token={token} accounts={accounts} packagesOnly={tab==='packages'}/> : ['notices','audit','proxy-activity'].includes(tab) ? <OperationHistory key={tab} token={token} mode={tab}/> : tab === 'users' ? user.role === 'super_admin' ? <UserManager token={token} /> : <section className="account-section"><h2>无权访问用户列表</h2><p className="muted">只有超级管理员可以管理用户角色。</p><Link to="/admin/accounts" className="outline small">返回账号管理</Link></section> : tab === 'addresses' ? <AddressManager token={token} /> : tab === 'bank-cards' ? <BankCardManager token={token} accounts={accounts} /> : tab === 'proxies' ? <ProxyManager config={proxyConfig} error={proxyError} refresh={refreshProxies} onChange={setProxyConfig} /> : tab === 'wallet' ? <WalletPanel wallet={wallet} token={token} refresh={refresh} user={user || {}} notice={notice} /> : <section className="account-section">
      <div className="section-title account-list-title"><h2>{admin ? '全部 ChatGPT 账号' : '你的 ChatGPT 账号'}</h2><div className="browser-buttons"><button className="text-btn" onClick={() => refresh().catch(e => setError(e.message))}><RefreshCw size={15} />刷新</button><button className="primary small" onClick={() => open('add')}><Plus size={15} />添加账号</button></div></div>
      <div className="address-search"><input aria-label="搜索账号" placeholder="搜索账号邮箱或名称" value={accountQuery} onChange={e=>setAccountQuery(e.target.value)}/>{admin && <><button className="outline small" onClick={()=>downloadCSV('/accounts/export?'+new URLSearchParams({q:accountQuery,group:groupFilter==='all'?'':groupFilter===''?'none':groupFilter}),token,'账号清单.csv').catch(e=>setError(e.message))}>导出清单</button></>}</div>{admin && <div className="account-toolbar"><Select label="筛选账号分组" value={groupFilter} onChange={setGroupFilter} options={[{ value: 'all', label: '全部分组 · ' + accounts.length }, { value: '', label: '未分组 · ' + accounts.filter(account => !account.group_id).length }, ...groups.map(group => ({ value: String(group.id), label: group.name + ' · ' + group.account_count }))]} /><button className="outline small" onClick={() => setGroupManager(true)}>管理分组</button><span className="muted">{accountPage.total} 个账号</span></div>}
      <DataTable searchQuery={accountQuery} label="ChatGPT 账号列表" className={admin ? 'accounts-table' : 'accounts-table user-accounts-table'} columns={['账号', ...(admin ? ['所属用户', '分组', 'SOCKS5', 'Session', '续订日期'] : []), '上次登录（UTC+8）', ...(admin ? ['操作'] : [])]} empty={!visibleAccounts.length && (accounts.length ? '此分组暂无账号。' : <div className="empty"><KeyRound size={24} /><h3>还没有添加账号</h3><p>粘贴 Session JSON，保存你的 ChatGPT 账号。</p><button className="outline" onClick={() => open('add')}>添加第一个账号</button></div>)}>
        {visibleAccounts.map(account => <tr className="account-row" key={account.id}>
          <td className="table-text"><strong>{account.label}</strong>{account.label !== account.email && <small className="cell-secondary">{account.email}</small>}</td>
          {admin && <td className="table-text">{account.owner_email || '—'}</td>}
          {admin && <><td className="table-selector"><div className="account-group"><Select label={'账号 ' + account.email + ' 的分组'} value={account.group_id ?? ''} onChange={value => bindGroup(account, value)} disabled={groupBusy !== null} options={[{ value: '', label: '未分组' }, ...groups.filter(group => group.user_id === account.user_id).map(group => ({ value: String(group.id), label: group.name }))]} searchPlaceholder="输入分组名称过滤…" createLabel="新建分组" onCreate={name => setCreatingGroup({ account, name })} /></div></td>
          <td className="table-selector">{admin && <div className="account-proxy"><Select label={'账号 ' + account.email + ' 的 SOCKS5'} value={proxyConfig?.bindings[browserEnvironmentID(user.id, account.id)] || ''} disabled={!proxyConfig || bindingAccount !== null} onChange={value => bindProxy(account, value)} options={[{ value: '', label: proxyConfig ? '直连（不使用代理）' : '请先启动本机启动器' }, ...(proxyConfig?.proxies || []).map(proxy => ({ value: proxy.id, label: proxy.name + ' · ' + proxy.host }))]} /></div>}</td>
          <td><span className={account.has_session ? 'credit' : 'muted'}>{account.has_session ? 'Session 已保存' : '未保存 Session'}</span></td>
          <td><strong>{account.renewal_date || '未设置'}</strong><small className={'cell-secondary ' + (account.renewal_date && account.renewal_date > today ? 'credit' : 'muted')}>{account.verified_at ? `已开通 ${account.verified_plan} · 到期 ${account.subscription_ends_at || '未知'}` : '人工维护日期'}</small></td></>}
          <td className="last-login">{formatUTC8(account.last_login_at)}</td>
          {admin && <td className="table-actions"><div className="account-actions">{admin && <button className="outline small" onClick={() => toggleBrowser(account)} disabled={localBrowsers.closing[account.id] || localBrowsers.states[account.id]?.state === 'closing' || (!browserRunning(localBrowsers.states[account.id]) && !account.has_session)}><Monitor size={15} />{localBrowsers.closing[account.id] || localBrowsers.states[account.id]?.state === 'closing' ? '正在关闭…' : browserRunning(localBrowsers.states[account.id]) ? '关闭浏览器' : '打开账号'}</button>}{admin && <button className="outline small" onClick={() => open('browser', account)} disabled={!account.has_session}><Monitor size={15} />浏览器管理</button>}{(admin || account.user_id === user?.id) && <button className="outline small" onClick={()=>request('/accounts/'+account.id+'/subscription',token,{method:'PATCH',body:{renewal_enabled:!account.renewal_enabled}}).then(value=>{setMessage(value.message);refresh()}).catch(e=>setError(e.message))}>{account.renewal_enabled ? '关闭续费提醒':'开启续费提醒'}</button>}{(admin || account.user_id === user?.id) && <button className="outline small" onClick={() => open('session', account)}>更新 Session</button>}{admin && <button className="outline small" onClick={() => open('date', account)}><CalendarDays size={15} />设置日期</button>}{(admin || account.user_id === user?.id) && <><button className="icon-btn" aria-label={`删除 ${account.label}`} onClick={() => open('delete', account)}><Trash2 size={17} /></button></>}</div></td>}
        </tr>)}
      </DataTable><Pagination page={accountPageIndex} pageSize={pageSize} total={accountPage.total} onPageChange={setAccountPageIndex} onPageSizeChange={setPageSize} disabled={false} />
    </section>}
    {creatingGroup && <CreateAccountGroup token={token} account={creatingGroup.account} initialName={creatingGroup.name} onCreated={refreshGrouping} onClose={() => setCreatingGroup(null)} />}
    {groupManager && <GroupManager groups={groups} token={token} user={user} accounts={accounts} onChange={refreshGrouping} onClose={() => setGroupManager(false)} />}
    {dialog && <Dialog title={{ add: '导入 ChatGPT 账号', session: '更新账号 Session', browser: '账号浏览器管理', 'local-browser': '在本机打开账号', date: '设置续订日期', delete: '删除账号' }[dialog.type]} onClose={() => { if (!busy) setDialog(null); }}>{dialog.type === 'local-browser' ? <LocalBrowserSession key={dialog.key} account={dialog.account} userID={user.id} token={token} onStatus={localBrowsers.update} onClosed={closeBrowserDialog} /> : dialog.type === 'browser' ? <BrowserSession account={dialog.account} token={token} onClosed={closeBrowserDialog} /> : <form onSubmit={submit}>
      {(dialog.type === 'add' || dialog.type === 'session') && <SessionFields updating={dialog.type === 'session'} />}
      {dialog.type === 'date' && <><p className="muted">{dialog.account.label} · {dialog.account.email}</p><label>下次续订日期<input type="date" name="renewal_date" defaultValue={dialog.account.renewal_date || ''} min="2000-01-01" max="9999-12-31" autoFocus /></label><p className="muted">清空日期可取消设置；此操作不扣除钱包代币。</p></>}
      {dialog.type === 'delete' && <p className="muted">确认删除「{dialog.account.label}」？账号将从列表隐藏，原始记录及钱包流水会保留。</p>}
      {dialogError && <p className="error" role="alert">{dialogError}</p>}
      <button className="primary full" disabled={busy}>{busy ? '处理中…' : dialog.type === 'delete' ? '确认删除' : '保存'}</button>
      {dialog.type === 'delete' && <button type="button" className="outline full" disabled={busy} onClick={() => setDialog(null)}>取消</button>}
    </form>}</Dialog>}
  </main>;
}

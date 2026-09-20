import Pagination from './Pagination';
import React, { useEffect, useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { request } from './api';
import Dialog from './Dialog';
import Select from './Select';
import UserPasswordDialog from './UserPasswordDialog';
import { formatUTC8 } from './time';

const roles = [{ value: 'user', label: '用户' }, { value: 'admin', label: '管理员' }];
const roleName = role => roles.find(item => item.value === role)?.label || '用户';

export default function UserManager({ token }) {
  const [pageSize, setPageSize] = useState(20);
  const [access,setAccess]=useState(null);
  const [passwordUser, setPasswordUser] = useState(null);
  const [message, setMessage] = useState('');

  const [data, setData] = useState({ users: [], total: 0 });
  const [page, setPage] = useState(1);
  const [query, setQuery] = useState('');
  const [draft, setDraft] = useState('');
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [change, setChange] = useState(null);
  const [busy, setBusy] = useState(false);
  const [saveError, setSaveError] = useState('');
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError('');
    request('/users?' + new URLSearchParams({ q: query, page, page_size: pageSize }), token, { signal: controller.signal })
      .then(value => { if (!controller.signal.aborted) setData(value); })
      .catch(error => { if (!controller.signal.aborted) setError(error.message); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [token, query, page, pageSize, revision]);
  async function save() {
    if (busy) return; setBusy(true); setSaveError('');
    try {
      const result = await request('/users/' + change.user.id + '/role', token, { method: 'PATCH', body: { role: change.role } });
      setData(current => ({ ...current, users: current.users.map(user => user.id === change.user.id ? { ...user, role: result.role } : user) }));
      setChange(null);
    } catch (error) { setSaveError(error.message); } finally { setBusy(false); }
  }
  return <section className="account-section user-manager">
    <div className="section-title"><h2>用户列表 <span className="muted">{data.total} 人</span></h2><button className="text-btn" disabled={loading} onClick={() => setRevision(value => value + 1)}><RefreshCw size={15} />刷新</button></div>
    <p className="muted">管理员可管理账号、分组、地址和银行卡；用户仅可查看和添加自己的账号。只有超级管理员可以调整角色。</p>
    <form className="address-search" onSubmit={event => { event.preventDefault(); setQuery(draft.trim()); setPage(1); }}><input aria-label="搜索用户" placeholder="搜索邮箱或超管用户名" value={draft} maxLength={200} onChange={event => setDraft(event.target.value)} /><button className="outline small">搜索</button></form>
    {error && <p className="error" role="alert">{error}</p>}
    {message && <p className="success" role="status">{message}</p>}
    {loading ? <p className="muted">正在加载用户…</p> : <div className="table-wrap"><table><thead><tr><th>用户</th><th>注册时间（UTC+8）</th><th>角色</th><th>登录管理</th></tr></thead><tbody>{data.users.map(user => <tr key={user.id}><td>{user.username || user.email}<small className="order-number">ID：{user.id}</small></td><td>{formatUTC8(user.created_at)}</td><td>{user.role === 'super_admin' ? <span className="muted">超级管理员 · 环境配置</span> : <Select label={'用户 ' + user.email + ' 的角色'} value={user.role || 'user'} options={roles} disabled={busy} onChange={role => { if (role !== (user.role || 'user')) { setChange({ user, role }); setSaveError(''); } }} />}</td><td>{user.role!=='super_admin'&&<div className="user-login-actions"><button className="outline small" disabled={busy} onClick={()=>{setSaveError('');request('/users/'+user.id+'/access',token).then(v=>setAccess({user,disabled:v.access.disabled,permissions:v.access.permissions||v.permissions})).catch(e=>setError(e.message))}}>管理登录</button><button className="outline small" disabled={busy} onClick={() => { setPasswordUser(user); setMessage(''); }}>修改密码</button></div>}</td></tr>)}</tbody></table>{!data.total && <p className="empty muted">没有找到用户。</p>}</div>}
    <Pagination page={page} pageSize={pageSize} total={data.total} onPageChange={setPage} onPageSizeChange={setPageSize} disabled={loading} />
    {passwordUser && <UserPasswordDialog user={passwordUser} token={token} onClose={() => setPasswordUser(null)} onSaved={message => { setPasswordUser(null); setMessage(message); }} />}
    {access && <Dialog title="用户登录状态" onClose={()=>{if(!busy)setAccess(null)}}><p>{access.user.email} · 保存后撤销此用户现有登录。</p><label><input type="checkbox" checked={access.disabled} onChange={e=>setAccess({...access,disabled:e.target.checked})}/>停用用户登录</label><p className="muted">管理员拥有全部业务权限；用户仅可查看和添加账号。</p>{saveError&&<p className="error">{saveError}</p>}<div className="browser-buttons"><button className="primary" disabled={busy} onClick={async()=>{if(busy)return;setBusy(true);try{await request('/users/'+access.user.id+'/access',token,{method:'PATCH',body:{disabled:access.disabled,permissions:null}});setAccess(null);setRevision(v=>v+1)}catch(e){setSaveError(e.message)}finally{setBusy(false)}}}>确认保存</button><button className="outline" disabled={busy} onClick={()=>setAccess(null)}>取消</button></div></Dialog>}
    {change && <Dialog title="确认修改角色" onClose={() => { if (!busy) setChange(null); }}><p>将「{change.user.email}」从{roleName(change.user.role)}改为{roleName(change.role)}？</p>{saveError && <p className="error" role="alert">{saveError}</p>}<div className="browser-buttons"><button className="primary" disabled={busy} onClick={save}>{busy ? '保存中…' : '确认修改'}</button><button className="outline" disabled={busy} onClick={() => setChange(null)}>取消</button></div></Dialog>}
  </section>;
}

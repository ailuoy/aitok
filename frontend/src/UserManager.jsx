import React, { useEffect, useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { request } from './api';
import Dialog from './Dialog';
import Select from './Select';
import { formatUTC8 } from './time';

const roles = [{ value: 'user', label: '用户' }, { value: 'admin', label: '管理员' }];
const roleName = role => roles.find(item => item.value === role)?.label || '用户';

export default function UserManager({ token }) {
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
    request('/users?' + new URLSearchParams({ q: query, page }), token, { signal: controller.signal })
      .then(value => { if (!controller.signal.aborted) setData(value); })
      .catch(error => { if (!controller.signal.aborted) setError(error.message); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [token, query, page, revision]);
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
    <p className="muted">管理员可管理账号、分组、地址和银行卡；用户管理自己的数据。只有超级管理员可以调整角色。</p>
    <form className="address-search" onSubmit={event => { event.preventDefault(); setQuery(draft.trim()); setPage(1); }}><input aria-label="搜索用户" placeholder="搜索邮箱或超管用户名" value={draft} maxLength={200} onChange={event => setDraft(event.target.value)} /><button className="outline small">搜索</button></form>
    {error && <p className="error" role="alert">{error}</p>}
    {loading ? <p className="muted">正在加载用户…</p> : <div className="table-wrap"><table><thead><tr><th>用户</th><th>注册时间（UTC+8）</th><th>角色</th></tr></thead><tbody>{data.users.map(user => <tr key={user.id}><td>{user.username || user.email}<small className="order-number">ID：{user.id}</small></td><td>{formatUTC8(user.created_at)}</td><td>{user.role === 'super_admin' ? <span className="muted">超级管理员 · 环境配置</span> : <Select label={'用户 ' + user.email + ' 的角色'} value={user.role || 'user'} options={roles} disabled={busy} onChange={role => { if (role !== (user.role || 'user')) { setChange({ user, role }); setSaveError(''); } }} />}</td></tr>)}</tbody></table>{!data.total && <p className="empty muted">没有找到用户。</p>}</div>}
    <div className="address-pagination"><span className="muted">第 {page} / {Math.max(1, Math.ceil(data.total / 20))} 页</span><button className="outline small" disabled={loading || page === 1} onClick={() => setPage(page - 1)}>上一页</button><button className="outline small" disabled={loading || page * 20 >= data.total} onClick={() => setPage(page + 1)}>下一页</button></div>
    {change && <Dialog title="确认修改角色" onClose={() => { if (!busy) setChange(null); }}><p>将「{change.user.email}」从{roleName(change.user.role)}改为{roleName(change.role)}？</p>{saveError && <p className="error" role="alert">{saveError}</p>}<div className="browser-buttons"><button className="primary" disabled={busy} onClick={save}>{busy ? '保存中…' : '确认修改'}</button><button className="outline" disabled={busy} onClick={() => setChange(null)}>取消</button></div></Dialog>}
  </section>;
}

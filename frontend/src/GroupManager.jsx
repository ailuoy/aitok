import React, { useState } from 'react';
import { request } from './api';
import Dialog from './Dialog';
import Select from './Select';

export default function GroupManager({ groups, token, user, accounts, onChange, onClose }) {
  const [editing, setEditing] = useState(null);
  const [deleting, setDeleting] = useState(null);
  const [name, setName] = useState('');
  const [owner, setOwner] = useState(String(user.id));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const owners = new Map([[String(user.id), user.email || user.username]]);
  if (user.role === 'super_admin') accounts.forEach(account => owners.set(String(account.user_id), account.owner_email || String(account.user_id)));
  async function save(event) {
    event.preventDefault(); if (busy) return;
    setBusy(true); setError('');
    try {
      await request('/account-groups' + (editing ? '/' + editing.id : ''), token, { method: editing ? 'PATCH' : 'POST', body: { name, user_id: Number(owner) } });
      await onChange(); setEditing(null); setName('');
    } catch (error) { setError(error.message); } finally { setBusy(false); }
  }
  async function remove() {
    if (busy) return; setBusy(true); setError('');
    try { await request('/account-groups/' + deleting.id, token, { method: 'DELETE' }); await onChange(); setDeleting(null); if (editing?.id === deleting.id) { setEditing(null); setName(''); } }
    catch (error) { setError(error.message); } finally { setBusy(false); }
  }
  return <Dialog title="管理账号分组" onClose={() => { if (!busy) onClose(); }}>
    <form onSubmit={save}><label>{editing ? '编辑分组名称' : '新分组名称'}<input name="group_name" value={name} maxLength={80} required onChange={event => setName(event.target.value)} placeholder="例如：九月采购、团队 A" disabled={busy} /></label>
      {!editing && user.role === 'super_admin' && <label>所属用户<Select label="分组所属用户" value={owner} options={[...owners].map(([value, label]) => ({ value, label }))} onChange={setOwner} disabled={busy} /></label>}
      <div className="browser-buttons"><button className="primary small" disabled={busy}>{editing ? '保存分组' : '添加分组'}</button>{editing && <button type="button" className="outline small" onClick={() => { setEditing(null); setName(''); }}>取消编辑</button>}</div>
    </form>
    {error && <p className="error" role="alert">{error}</p>}
    <div className="group-list">{groups.map(group => <div className="group-row" key={group.id}><div><strong>{group.name}</strong><small>{group.account_count} 个账号{user.role === 'super_admin' ? ' · ' + (owners.get(String(group.user_id)) || group.user_id) : ''}</small></div><button className="text-btn" disabled={busy} onClick={() => { setEditing(group); setName(group.name); setOwner(String(group.user_id)); }}>编辑</button><button className="text-btn danger" disabled={busy} onClick={() => { setDeleting(group); setError(''); }}>删除</button></div>)}{!groups.length && <p className="muted">暂无分组，添加后可在账号卡片绑定。</p>}</div>
    {deleting && <Dialog title="删除分组" onClose={() => { if (!busy) setDeleting(null); }}><p>确认删除「{deleting.name}」？组内账号会移至“未分组”，账号不会删除。</p>{error && <p className="error">{error}</p>}<div className="browser-buttons"><button className="primary" disabled={busy} onClick={remove}>确认删除</button><button className="outline" disabled={busy} onClick={() => setDeleting(null)}>取消</button></div></Dialog>}
  </Dialog>;
}

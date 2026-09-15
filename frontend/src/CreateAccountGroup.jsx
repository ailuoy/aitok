import React, { useState } from 'react';
import Dialog from './Dialog';
import { request } from './api';

export default function CreateAccountGroup({ account, initialName, token, onCreated, onClose }) {
  const [name, setName] = useState(initialName);
  const [created, setCreated] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  async function submit(event) {
    event.preventDefault(); if (busy) return;
    setBusy(true); setError('');
    try {
      const group = created || (await request('/account-groups', token, { method: 'POST', body: { name, user_id: account.user_id } })).group;
      setCreated(group);
      await request('/accounts/' + account.id + '/group', token, { method: 'PATCH', body: { group_id: group.id } });
      await onCreated(group); onClose();
    } catch (error) { setError(error.message); } finally { setBusy(false); }
  }
  return <Dialog title="新建账号分组" onClose={() => { if (!busy) onClose(); }}><form onSubmit={submit}><p className="muted">创建后将「{account.label}」加入该分组。</p><label>分组名称<input name="group_name" value={name} onChange={event => setName(event.target.value)} required maxLength={80} autoFocus disabled={busy || Boolean(created)} /></label>{error && <p className="error" role="alert">{created ? '分组已创建，绑定或刷新失败，可重试。' : ''}{error}</p>}<div className="browser-buttons"><button className="primary small" disabled={busy}>{busy ? '处理中…' : created ? '重试绑定' : '创建并绑定'}</button><button className="outline small" type="button" disabled={busy} onClick={onClose}>取消</button></div></form></Dialog>;
}

import React, { useRef, useState } from 'react';
import Dialog from './Dialog';
import { request } from './api';

export default function UserPasswordDialog({ user, token, onClose, onSaved }) {
  const [newPassword, setNewPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const writing = useRef(false);
  async function save(event) {
    event.preventDefault();
    if (writing.current) return;
    const length = new TextEncoder().encode(newPassword).length;
    if (length < 8 || length > 72) { setError('新密码需为 8 至 72 字节'); return; }
    if (newPassword !== confirmation) { setError('两次输入的新密码不一致'); return; }
    writing.current = true; setBusy(true); setError('');
    try {
      const result = await request(`/users/${user.id}/password`, token, { method: 'PATCH', body: { new_password: newPassword, confirm_password: confirmation } });
      onSaved(result.message);
    } catch (error) { setError(error.message); }
    finally { writing.current = false; setBusy(false); }
  }
  return <Dialog title="修改密码" className="user-password-dialog" onClose={() => { if (!writing.current) onClose(); }}>
    <form onSubmit={save}>
      <p>修改「{user.email}」的登录密码，保存后该用户需要重新登录。</p>
      <label>新密码<input name="new_password" type="password" autoComplete="new-password" required autoFocus disabled={busy} value={newPassword} onChange={event => setNewPassword(event.target.value)} /></label>
      <label>确认新密码<input name="confirm_password" type="password" autoComplete="new-password" required disabled={busy} value={confirmation} onChange={event => setConfirmation(event.target.value)} /></label>
      <p className="muted">新密码需为 8 至 72 字节，中文等字符按多个字节计算。</p>
      {error && <p className="error" role="alert">{error}</p>}
      <div className="browser-buttons"><button className="primary" disabled={busy}>{busy ? '保存中…' : '确认修改密码'}</button><button type="button" className="outline" disabled={busy} onClick={onClose}>取消</button></div>
    </form>
  </Dialog>;
}

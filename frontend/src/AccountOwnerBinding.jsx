import React, { useRef, useState } from 'react';
import Dialog from './Dialog';
import { request } from './api';

export default function AccountOwnerBinding({ account, token, onBound, onClose }) {
  const [email, setEmail] = useState('');
  const [matched, setMatched] = useState(null);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const working = useRef(false);
  const endpoint = `/accounts/${account.id}/owner`;
  const sameOwner = matched?.id === account.user_id;

  async function lookup(event) {
    event.preventDefault();
    if (working.current) return;
    working.current = true; setBusy('lookup'); setError(''); setMatched(null);
    try {
      const data = await request(endpoint, token, { method: 'POST', body: { email: email.trim().toLowerCase() } });
      setMatched(data.user);
    } catch (error) { setError(error.message); }
    finally { working.current = false; setBusy(''); }
  }

  async function bind() {
    if (working.current || !matched || sameOwner) return;
    working.current = true; setBusy('bind'); setError('');
    try {
      const data = await request(endpoint, token, { method: 'PATCH', body: { email: matched.email, user_id: matched.id, expected_owner_id: account.user_id } });
      onBound(data); onClose();
    } catch (error) { setMatched(null); setError(error.message); }
    finally { working.current = false; setBusy(''); }
  }

  return <Dialog title="绑定所属用户" className="account-owner-dialog" onClose={() => { if (!working.current) onClose(); }}>
    <form onSubmit={lookup}>
      <p className="muted">账号：{account.label} · {account.email}<br />当前所属用户：{account.owner_email || '—'}</p>
      <label>注册用户邮箱<input name="owner_email" type="email" inputMode="email" autoComplete="off" spellCheck={false} autoFocus required maxLength={254} value={email} disabled={Boolean(busy)} placeholder="请输入完整的注册邮箱" onChange={event => { setEmail(event.target.value); setMatched(null); setError(''); }} /></label>
      <p className="muted">仅按完整邮箱精确查找，不支持模糊搜索。</p>
      <button className="outline" disabled={Boolean(busy) || !email.trim()}>{busy === 'lookup' ? '正在查找…' : '查找用户'}</button>
      {matched && <div className="notice account-owner-match" role="status"><strong>已找到注册用户</strong><span>{matched.email}</span><small>用户 ID：{matched.id}</small>{sameOwner ? <p>该账号已属于此用户，无需重复绑定。</p> : <p>确认后，此账号将归属该用户，原分组绑定会解除。</p>}</div>}
      {error && <p className="error" role="alert">{error}</p>}
      <div className="browser-buttons"><button className="primary" type="button" disabled={Boolean(busy) || !matched || sameOwner} onClick={bind}>{busy === 'bind' ? '正在绑定…' : '确认绑定'}</button><button className="outline" type="button" disabled={Boolean(busy)} onClick={onClose}>取消</button></div>
    </form>
  </Dialog>;
}

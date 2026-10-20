import React, { useEffect, useState } from 'react';
import QRCode from 'qrcode';
import { request } from './api';

export default function TwoFactor({ token, onVerify, onCancel, compact = false, verifyLabel = '验证并继续' }) {
  const [enabled, setEnabled] = useState(null);
  const [setup, setSetup] = useState(null);
  const [qr, setQR] = useState('');
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const c = new AbortController();
    request('/two-factor', token, { signal: c.signal }).then(v => { if (!c.signal.aborted) setEnabled(v.enabled); }).catch(e => { if (!c.signal.aborted) setError(e.message); });
    return () => c.abort();
  }, [token, revision]);
  useEffect(() => {
    if (!enabled || !setup) return;
    const timer = setTimeout(() => {
      setSetup(null); setQR(''); setMessage('二维码已隐藏，需要时请重新验证密码。');
    }, 5 * 60 * 1000);
    return () => clearTimeout(timer);
  }, [enabled, setup]);
  async function submit(event) {
    event.preventDefault(); if (busy || enabled === null || (enabled && !onVerify && setup)) return;
    setBusy(true); setError(''); setMessage('');
    try {
      if (!setup && (!enabled || !onVerify)) {
        const data = await request('/two-factor', token, { method: 'POST', body: { action: enabled ? 'reveal' : 'setup', password } });
        setPassword('');
        const image = await QRCode.toDataURL(data.otpauth_url, { width: 220, margin: 2 });
        setQR(image); setSetup(data);
      } else if (!enabled) {
        await request('/two-factor', token, { method: 'POST', body: { action: 'confirm', code } });
        setSetup(null); setQR(''); setCode(''); setEnabled(true);
        setMessage('验证器已绑定。继续操作时请使用下一组新验证码。');
      } else {
        await onVerify(code);
      }
    } catch (e) { setError(e.message); } finally { setBusy(false); setCode(''); setPassword(''); }
  }
  return <form className={'two-factor' + (compact && enabled ? ' two-factor-compact' : '')} onSubmit={submit}>
    {enabled === null ? <p className="muted">正在读取验证器状态…</p> : enabled ? !compact && <p className="success">两步验证已启用</p> : !setup && <p>请先绑定验证器，敏感操作需要两步验证。</p>}
    {enabled !== null && !setup && (!enabled || !onVerify) && <>
      {enabled && <p>输入当前登录密码，可再次查看二维码并添加到验证器。</p>}
      <label>当前登录密码<input type="password" autoComplete="current-password" value={password} onChange={e => setPassword(e.target.value)} required disabled={busy} /></label>
    </>}
    {setup && <>
      <p>{enabled ? '使用验证器扫描二维码即可添加，原验证器仍可使用。二维码将在 5 分钟后自动隐藏。' : '使用 Google Authenticator、Microsoft Authenticator 等扫描二维码，10 分钟内输入验证码完成绑定。'}</p>
      <img className="totp-qr" src={qr} alt="验证器绑定二维码" />
      <details><summary>无法扫码？手动输入密钥</summary><code className="totp-secret">{setup.secret}</code></details>
    </>}
    {((!enabled && setup) || (enabled && onVerify)) && <label>验证器验证码<input aria-label="验证器验证码" inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" maxLength={6} value={code} onChange={e => setCode(e.target.value.replace(/\D/g, ''))} placeholder="6 位验证码" required disabled={busy} /></label>}
    {message && <p className="success" role="status">{message}</p>}
    {error && <p className="error" role="alert">{error}</p>}
    <div className="browser-buttons">
      {enabled !== null && (!enabled || onVerify || !setup) && <button className="primary" disabled={busy}>{busy ? '处理中…' : !enabled ? setup ? '确认绑定' : '生成绑定二维码' : onVerify ? verifyLabel : '验证密码并显示二维码'}</button>}
      {enabled && setup && !onVerify && <button type="button" className="outline" onClick={() => { setSetup(null); setQR(''); setMessage(''); }}>隐藏二维码</button>}
      {enabled === null && error && <button type="button" className="outline" onClick={() => { setError(''); setRevision(v => v + 1); }}>重试</button>}
      {onCancel && <button type="button" className="outline" disabled={busy} onClick={onCancel}>取消</button>}
    </div>
  </form>;
}

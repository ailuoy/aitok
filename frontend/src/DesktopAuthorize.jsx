import React, { useState } from 'react';
import { request } from './api';

export default function DesktopAuthorize({ token, user, route }) {
  const [busy, setBusy] = useState(false), [done, setDone] = useState(false), [error, setError] = useState('');
  const state = route.searchParams.get('state'), channel = route.searchParams.get('channel');
  let callback;
  try {
    const url = new URL(route.searchParams.get('callback'));
    if (url.protocol === 'http:' && url.hostname === '127.0.0.1' && Number(url.port) >= 1024 && !url.username && !url.password && !url.search && !url.hash && url.pathname === '/callback/' + state && /^[\w-]{43}$/.test(state) && ['test', 'production'].includes(channel)) callback = url.href;
  } catch {}
  async function authorize() {
    if (busy || !callback) return;
    setBusy(true); setError('');
    try {
      const grant = await request('/desktop-auth', token, { method: 'POST', body: { state, channel } });
      const response = await fetch(callback, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(grant), signal: AbortSignal.timeout(15000), redirect: 'error', credentials: 'omit' });
      if (!response.ok) throw new Error('助手未接受授权，请回到助手重新点击登录');
      setDone(true);
    } catch (error) { setError(/^[\u3400-\u9fff]/.test(error.message) ? error.message : '无法连接本机助手，请保持助手打开，并允许浏览器访问本地网络后重试'); }
    finally { setBusy(false); }
  }
  const allowed = ['admin', 'super_admin'].includes(user.role);
  return <main className="auth-wrap"><section className="auth-card">
    <h2>{done ? '授权成功' : '授权登录 AiTok 助手'}</h2>
    {done ? <p role="status">可以返回桌面助手，此页面可以关闭。</p> : <>
      <p>当前账号：{user.username || user.email}</p><p>{channel === 'test' ? '测试版' : '线上版'} · {location.origin}</p>
      <p>确认后将此账号的登录身份交给本机助手，打开账号仍需两步验证。</p>
      {!callback && <p className="error" role="alert">授权链接无效，请从桌面助手重新发起登录。</p>}
      {!allowed && <p className="error" role="alert">桌面助手需要管理员账号授权。</p>}
      {error && <p className="error" role="alert">{error}</p>}
      <button className="primary full" disabled={busy || !callback || !allowed} onClick={authorize}>{busy ? '正在授权…' : '确认授权登录'}</button>
    </>}
  </section></main>;
}

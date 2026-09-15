import React, { useEffect, useRef, useState } from 'react';
import { request } from './api';
import Select from './Select';
import TwoFactor from './TwoFactor';

export const stateLabels = { closed: '未打开', authenticated: '已确认登录', login_required: '需要网页登录', unverified: '登录待确认', starting: '正在启动', checking_ip: '已打开 · 核对 IP 中', ip_check_failed: 'IP 核对未通过', opened: '已打开', api_verified: '账号接口已验证', rejected: '上游未接受凭据', signed_out: '已退出会话适配', closing: '正在关闭', unavailable: '浏览器服务不可用', error: '会话适配失败' };

export default function BrowserSession({ account, token, onClosed }) {
  const [verifying, setVerifying] = useState(false);
  const [proxyMode, setProxyMode] = useState('saved');
  const [proxy, setProxy] = useState('');
  const [settings, setSettings] = useState(null);
  const [status, setStatus] = useState(null);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const mounted = useRef(true);
  const busyRef = useRef(false);
  const hasOpened = useRef(false);
  const endpoint = '/accounts/' + account.id + '/browser';
  useEffect(() => {
    if (!status) return;
    if (!['closed', 'unavailable'].includes(status.state)) hasOpened.current = true;
    if (status.state === 'closed' && hasOpened.current) onClosed?.();
  }, [status, onClosed]);

  useEffect(() => {
    mounted.current = true;
    const controller = new AbortController();
    let timer;
    const poll = async () => {
      if (!busyRef.current) {
        try {
          const data = await request(endpoint, token, { signal: controller.signal });
          if (!controller.signal.aborted && !busyRef.current) { setStatus(data.browser); setSettings(data.settings); }
        } catch (error) { if (!controller.signal.aborted) setError(error.message); }
      }
      if (!controller.signal.aborted) timer = setTimeout(poll, 3000);
    };
    poll();
    return () => { mounted.current = false; controller.abort(); clearTimeout(timer); };
  }, [endpoint, token]);

  async function act(action, totpCode) {
    if (busyRef.current) return;
    busyRef.current = true; setBusy(true); setError(''); setMessage('');
    try {
      const body = proxyMode === 'saved' ? {} : { proxy_url: proxyMode === 'direct' ? '' : proxy.trim() };
      if (action !== 'stop' && proxyMode === 'socks5' && !proxy.trim()) throw new Error('请填写 SOCKS5 代理地址');
      const data = await request(endpoint, token, { totpCode, method: action === 'start' ? 'POST' : action === 'save' ? 'PATCH' : 'DELETE', ...(action === 'stop' ? {} : { body }) });
      if (mounted.current) {
        if (data.browser) setStatus(data.browser);
        if (action === 'start') setVerifying(false);
        setSettings(data.settings); setProxyMode('saved'); setProxy('');
        if (action === 'save') setMessage('代理配置已加密保存，下次打开浏览器时使用。');
      }
    } catch (error) { if (action === 'start') throw error; if (mounted.current) setError(error.message); }
    finally { busyRef.current = false; if (mounted.current) setBusy(false); }
  }

  const running = status && !['closed', 'unavailable'].includes(status.state);
  if (verifying && !running) return <div className="browser-session"><p className="muted">{account.label} · {account.email}</p><TwoFactor token={token} onVerify={code => act('start', code)} onCancel={() => setVerifying(false)} /></div>;
  return <div className="browser-session">
    <p className="muted">{account.label} · {account.email}</p>
    <p className="notice">浏览器窗口将在后台所在电脑上打开，每个账号使用独立环境。恢复网页登录需要登录 Cookie；仅有 accessToken 时请在独立窗口中登录一次。</p>
    <div className="browser-status-row"><span>浏览器状态</span><strong>{status ? stateLabels[status.state] || status.state : '正在读取…'}</strong></div>
    {status?.message && <p className={status.state === 'authenticated' ? 'success' : 'muted'} role="status">{status.message}</p>}
    <label>网络连接<Select label="网络连接" value={proxyMode} onChange={setProxyMode} disabled={busy || running || !settings} options={[{ value: 'saved', label: settings?.has_proxy ? '已保存：' + settings.proxy_address : '当前配置：直连' }, { value: 'direct', label: '改用直连' }, { value: 'socks5', label: '设置 SOCKS5 代理' }]} /></label>
    {proxyMode === 'socks5' && <label>SOCKS5 代理<input type="password" value={proxy} onChange={event => setProxy(event.target.value)} placeholder="socks5://用户名:密码@主机:端口" autoComplete="off" disabled={busy || running} /></label>}
    <p className="muted">代理配置会随账号加密保存。更换代理或更新 Session 后，重新打开浏览器即可生效。</p>
    {message && <p className="success" role="status">{message}</p>}
    {error && <p className="error" role="alert">{error}</p>}
    <div className="browser-buttons"><button className="outline" disabled={busy || running || proxyMode === 'saved'} onClick={() => act('save')}>保存代理</button><button className="primary" disabled={busy || running || !status} onClick={() => setVerifying(true)}>{busy ? '处理中…' : '打开浏览器'}</button><button className="outline" disabled={busy || !running} onClick={() => act('stop')}>关闭浏览器</button></div>
  </div>;
}

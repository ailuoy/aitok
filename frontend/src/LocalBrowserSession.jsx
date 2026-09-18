import React, { useEffect, useRef, useState } from 'react';
import TwoFactor from './TwoFactor';
import { stateLabels } from './BrowserSession';
import { browserEnvironmentID, launcherRequest, openLocalAccount } from './localBrowser';
import LauncherConnection from './LauncherConnection';
import useLauncherPort from './useLauncherPort';
import BrowserStatusRefresh from './BrowserStatusRefresh';
import AccountProxySelect from './AccountProxySelect';
import BrowserFingerprint from './BrowserFingerprint';

export default function LocalBrowserSession({ account, userID, token, managing = false, proxyConfig, proxyBusy, proxyError, onProxyChange, onProxyRefresh, onStatus, onClosed }) {
  const port = useLauncherPort();
  const [status, setStatus] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [verifying, setVerifying] = useState(!managing);
  const [loadAssistant, setLoadAssistant] = useState(true);
  const busyRef = useRef(false);
  const statusVersion = useRef(0);
  const mounted = useRef(true);
  const hasOpened = useRef(false);
  const statusPath = '/browsers/' + encodeURIComponent(browserEnvironmentID(userID, account.id));
  const running = status && !['closed', 'unavailable'].includes(status.state);
  useEffect(() => {
    hasOpened.current = false; setStatus(null); setVerifying(!managing);
    if (!managing) return;
    const version = statusVersion.current;
    const controller = new AbortController();
    launcherRequest(statusPath, { port, signal: controller.signal })
      .then(data => { if (!controller.signal.aborted && statusVersion.current === version) setStatus(data); })
      .catch(error => { if (!controller.signal.aborted) setError(error.message); });
    return () => controller.abort();
  }, [port, statusPath, managing]);
  useEffect(() => {
    if (!status) return;
    onStatus?.(account.id, status);
    if (!['closed', 'unavailable'].includes(status.state)) hasOpened.current = true;
    if (status.state === 'closed' && hasOpened.current) onClosed?.();
  }, [status, account.id, onStatus, onClosed]);

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  async function act(action, totpCode) {
    if (busyRef.current) return;
    if (action === 'start' && proxyBusy) throw new Error('代理正在保存，请稍后再打开账号');
    statusVersion.current++;
    busyRef.current = true; setBusy(true); setError('');
    try {
      const data = action === 'start' ? await openLocalAccount(account, userID, token, totpCode, { loadAssistant }) : action === 'fingerprint' ? await launcherRequest(statusPath + '/fingerprint', { port, method: 'POST', body: {} }) : await launcherRequest(statusPath, { port, method: action === 'refresh' ? 'GET' : 'DELETE' });
      if (mounted.current) { setStatus(data); if (action !== 'refresh') setVerifying(false); }
    } catch (error) { if (mounted.current) setError(error.message); if (action === 'start') throw error; }
    finally { busyRef.current = false; if (mounted.current) setBusy(false); }
  }

  return <div className="browser-session local-browser-session">
    <p className="muted">{account.label} · {account.email}</p>
    <div className="browser-status-row"><span>本机浏览器</span><strong>{busy ? '正在处理…' : status ? stateLabels[status.state] || status.state : '未读取状态'}</strong></div>
    {status?.fingerprint_warning && <p className="notice" role="status">{status.fingerprint_warning}</p>}
    {status?.message && <p className={status.state === 'authenticated' ? 'success' : 'notice'} role="status">{status.message}</p>}
    {error && <p className="error" role="alert">{error}</p>}
    <label>网络连接<AccountProxySelect account={account} userID={userID} config={proxyConfig} label="网络连接" disabled={busy || proxyBusy || running} onChange={onProxyChange} /></label>
    {proxyError && <p className="error" role="alert">{proxyError}</p>}
    {verifying && !running && <TwoFactor compact token={token} onVerify={code => act('start', code)} />}
    <div className="browser-buttons"><BrowserStatusRefresh reminderKey={port} disabled={busy} onClick={() => act('refresh')}>刷新状态</BrowserStatusRefresh>{!verifying && <button className="primary" disabled={busy || proxyBusy || running} onClick={() => setVerifying(true)}>{managing ? '打开浏览器' : '重新打开'}</button>}<button className="outline" disabled={busy || !running} onClick={() => act('stop')}>关闭账号窗口</button><label className="browser-assistant-option" title="打开账号时生效；已打开的窗口需先关闭再重新打开"><input type="checkbox" checked={loadAssistant} onChange={event => setLoadAssistant(event.target.checked)} disabled={busy || running} />加载小助手</label></div>
    <div className="local-browser-settings"><BrowserFingerprint status={status} collapsed>
      <p className="muted">指纹独立保存，重开不变；重新生成保留登录数据。</p>
    <button className="outline" disabled={busy || running} onClick={() => { if (window.confirm('重新随机生成此账号的浏览器指纹？下次打开生效，登录数据保留，但网站可能要求重新验证。')) void act('fingerprint'); }}>重新随机生成指纹</button>
    </BrowserFingerprint><LauncherConnection onConnected={() => { setError(''); onProxyRefresh?.(); }} /></div>
  </div>;
}

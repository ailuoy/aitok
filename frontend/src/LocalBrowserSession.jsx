import React, { useEffect, useRef, useState } from 'react';
import TwoFactor from './TwoFactor';
import { stateLabels } from './BrowserSession';
import { browserEnvironmentID, launcherRequest, openLocalAccount } from './localBrowser';
import LauncherConnection from './LauncherConnection';
import useLauncherPort from './useLauncherPort';
import BrowserStatusRefresh from './BrowserStatusRefresh';

export default function LocalBrowserSession({ account, userID, token, onStatus, onClosed }) {
  const port = useLauncherPort();
  const [status, setStatus] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [verifying, setVerifying] = useState(true);
  const busyRef = useRef(false);
  const mounted = useRef(true);
  const hasOpened = useRef(false);
  const statusPath = '/browsers/' + encodeURIComponent(browserEnvironmentID(userID, account.id));
  const running = status && !['closed', 'unavailable'].includes(status.state);
  useEffect(() => { hasOpened.current = false; setStatus(null); setVerifying(true); }, [port]);
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
    busyRef.current = true; setBusy(true); setError('');
    try {
      const data = action === 'start' ? await openLocalAccount(account, userID, token, totpCode) : await launcherRequest(statusPath, { port, method: action === 'refresh' ? 'GET' : 'DELETE' });
      if (mounted.current) { setStatus(data); if (action !== 'refresh') setVerifying(false); }
    } catch (error) { if (mounted.current) setError(error.message); if (action === 'start') throw error; }
    finally { busyRef.current = false; if (mounted.current) setBusy(false); }
  }

  return <div className="browser-session local-browser-session">
    <p className="muted">{account.label} · {account.email}</p>
    <div className="browser-status-row"><span>本机浏览器</span><strong>{busy ? '正在处理…' : status ? stateLabels[status.state] || status.state : '未读取状态'}</strong></div>
    {status?.message && <p className={status.state === 'authenticated' ? 'success' : 'notice'} role="status">{status.message}</p>}
    {error && <p className="error" role="alert">{error}</p>}
    <LauncherConnection onConnected={() => setError('')} />
    {verifying && !running && <TwoFactor token={token} onVerify={code => act('start', code)} />}
    <p className="muted">在浏览器中登录或手动关闭窗口后，点击刷新状态更新显示。</p>
    <div className="browser-buttons"><BrowserStatusRefresh reminderKey={port} disabled={busy} onClick={() => act('refresh')}>刷新状态</BrowserStatusRefresh><button className="primary" disabled={busy || running} onClick={() => setVerifying(true)}>重新打开</button><button className="outline" disabled={busy || !running} onClick={() => act('stop')}>关闭账号窗口</button></div>
  </div>;
}

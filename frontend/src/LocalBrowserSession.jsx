import React, { useEffect, useRef, useState } from 'react';
import TwoFactor from './TwoFactor';
import { stateLabels } from './BrowserSession';
import { browserEnvironmentID, launcherRequest, openLocalAccount } from './localBrowser';
import LauncherConnection from './LauncherConnection';
import useLauncherPort from './useLauncherPort';

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
    const controller = new AbortController();
    let timer;
    const poll = async () => {
      if (!busyRef.current) {
        try {
          const data = await launcherRequest(statusPath, { signal: controller.signal });
          if (!controller.signal.aborted && !busyRef.current) setStatus(data);
        } catch { /* 启动错误保留在弹窗，轮询失败不覆盖操作提示。 */ }
      }
      if (!controller.signal.aborted) timer = setTimeout(poll, 3000);
    };
    timer = setTimeout(poll, 3000);
    return () => { mounted.current = false; controller.abort(); clearTimeout(timer); };
  }, [account, userID, token, statusPath, port]);

  async function act(action, totpCode) {
    if (busyRef.current) return;
    busyRef.current = true; setBusy(true); setError('');
    try {
      const data = action === 'start' ? await openLocalAccount(account, userID, token, totpCode) : await launcherRequest(statusPath, { method: 'DELETE' });
      if (mounted.current) { setStatus(data); setVerifying(false); }
    } catch (error) { if (mounted.current) setError(error.message); if (action === 'start') throw error; }
    finally { busyRef.current = false; if (mounted.current) setBusy(false); }
  }

  return <div className="browser-session local-browser-session">
    <p className="muted">{account.label} · {account.email}</p>
    <div className="browser-status-row"><span>本机浏览器</span><strong>{busy ? '正在处理…' : status ? stateLabels[status.state] || status.state : '尚未打开'}</strong></div>
    {status?.message && <p className={status.state === 'authenticated' ? 'success' : 'notice'} role="status">{status.message}</p>}
    {error && !verifying && <p className="error" role="alert">{error}</p>}
    <LauncherConnection onConnected={() => setError('')} />
    {verifying && !running && <TwoFactor token={token} onVerify={code => act('start', code)} />}
    <div className="browser-buttons"><button className="primary" disabled={busy || running} onClick={() => setVerifying(true)}>重新打开</button><button className="outline" disabled={busy || !running} onClick={() => act('stop')}>关闭账号窗口</button></div>
  </div>;
}

import React, { useEffect, useRef, useState } from 'react';
import TwoFactor from './TwoFactor';
import LauncherConnection from './LauncherConnection';
import { browserEnvironmentID, launcherRequest, openLocalAccount } from './localBrowser';
import useLauncherPort from './useLauncherPort';

export default function SessionRefresh({ account, userID, token, onStatus, onUpdated, onComplete, onManual }) {
  const port = useLauncherPort();
  const [status, setStatus] = useState(null);
  const [error, setError] = useState('');
  const [task, setTask] = useState(null);
  const [closing, setClosing] = useState(false);
  const callbacks = useRef({ onStatus, onUpdated, onComplete });
  callbacks.current = { onStatus, onUpdated, onComplete };
  const path = '/browsers/' + encodeURIComponent(browserEnvironmentID(userID, account.id));

  useEffect(() => {
    if (!task) return;
    const controller = new AbortController();
    let timer, saved = false;
    const poll = async () => {
      try {
        const next = await launcherRequest(path, { port: task.port, signal: controller.signal });
        if (controller.signal.aborted) return;
        setStatus(next); setError(''); callbacks.current.onStatus(account.id, next);
        if (next.session_update?.state === 'saved' && !saved) {
          saved = true;
          callbacks.current.onUpdated(account.id, { session_updated_at: next.session_update.session_updated_at });
        }
        if (next.state === 'closed') {
          setTask(null);
          if (next.session_update?.state === 'saved') callbacks.current.onComplete();
          else setError(next.session_update?.message || '浏览器已关闭，未完成 Session 更新');
          return;
        }
      } catch (error) {
        if (controller.signal.aborted) return;
        setError(error.message);
      }
      timer = setTimeout(poll, 2000);
    };
    void poll();
    return () => { controller.abort(); clearTimeout(timer); };
  }, [task, path, account.id]);

  async function start(code) {
    setError('');
    const next = await openLocalAccount(account, userID, token, code, { loadAssistant: false, refreshSession: true });
    setStatus(next); callbacks.current.onStatus(account.id, next); setTask({ port });
  }

  async function close() {
    setClosing(true);
    try { setStatus(await launcherRequest(path, { port: task.port, method: 'DELETE' })); }
    catch (error) { setError(error.message); }
    finally { setClosing(false); }
  }

  return <div className="browser-session local-browser-session">
    <p className="muted">{account.email}</p>
    <p>验证后自动打开账号浏览器，更新 Session 并关闭窗口。浏览器不加载小助手；如需登录或网页验证，请在打开的窗口中完成。</p>
    {status?.session_update && <p className={status.session_update.state === 'failed' ? 'error' : 'notice'} role="status">{status.session_update.message}</p>}
    {task && status?.message && <p className="muted">{status.message}</p>}
    {error && <p className="error" role="alert">{error}</p>}
    {!task && <TwoFactor compact token={token} onVerify={start} verifyLabel="验证并自动更新" />}
    {task && <button className="outline" disabled={closing} onClick={close}>{closing ? '正在关闭…' : '关闭浏览器'}</button>}
    {!task && <button className="text-btn" onClick={onManual}>手动导入 Session</button>}
    {!task && <LauncherConnection />}
  </div>;
}

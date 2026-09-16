import React, { useState } from 'react';
import { launcherCommand, launcherPort, launcherRequest, saveLauncherPort } from './localBrowser';

export default function LauncherConnection({ onConnected }) {
  const [port, setPort] = useState(() => String(launcherPort()));
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  async function save(event) {
    event.preventDefault(); setError(''); setMessage('');
    try {
      saveLauncherPort(port);
      setMessage('已保存此站点的连接端口。');
      onConnected?.();
    } catch (error) { setError(error.message); }
  }
  async function test() {
    setBusy(true); setError(''); setMessage('');
    try {
      const health = await launcherRequest('/health', { port, signal: AbortSignal.timeout(5000) });
      if (health.version !== 2 || health.origin && health.origin !== window.location.origin) throw new Error('助手版本或站点不匹配，请检查桌面助手配置');
      setMessage('连接成功，可保存此端口。');
    } catch (error) { setError(error.message); }
    finally { setBusy(false); }
  }
  return <details className="launcher-settings">
    <summary>本机助手连接</summary>
    <p className="muted">打开 AiTok 桌面助手，添加站点 <strong>{window.location.origin}</strong>。线上默认 15683，本地开发默认 15684；助手与这里填写相同端口。</p>
    <form className="browser-buttons" onSubmit={save}>
      <label>本机连接端口<input aria-label="本机连接端口" type="number" min="1024" max="65535" required value={port} onChange={event => { setPort(event.target.value); setError(''); setMessage(''); }} /></label>
      <button className="outline small" disabled={busy}>保存端口</button>
      <button className="outline small" type="button" disabled={busy} onClick={test}>{busy ? '连接中…' : '测试连接'}</button>
    </form>
    {error && <p className="error" role="alert">{error}</p>}
    {message && <p className="success" role="status">{message}</p>}
    <details><summary>命令行启动（开发调试）</summary><p className="muted">安装 Node.js 22+ 后，在项目目录执行。桌面助手已连接时无需重复启动。</p><pre className="launcher-command"><code>{launcherCommand()}</code></pre></details>
  </details>;
}

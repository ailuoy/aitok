import React, { useState } from 'react';
import { launcherRequest } from './localBrowser';
import Dialog from './Dialog';

export default function ProxyImport({ onClose, onChange }) {
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [results, setResults] = useState([]);
  const [error, setError] = useState('');
  async function submit(event) {
    event.preventDefault(); if (busy) return;
    setBusy(true); setError(''); setResults([]);
    try {
      const { items } = await launcherRequest('/proxies/parse', { method: 'POST', body: { text } });
      const failedLines = [];
      for (const item of items) {
        let message = item.error;
        if (!message) {
          try {
            const tested = await launcherRequest('/proxies/test', { method: 'POST', body: item.proxy });
            if (!tested.result.ok) throw new Error(tested.result.error || '测试失败');
            onChange(await launcherRequest('/proxies', { method: 'POST', body: { ...item.proxy, test_token: tested.test_token } }));
          } catch (error) { message = error.message; }
        }
        if (message) failedLines.push(text.split(/\r?\n/).map(line => line.trim()).filter(Boolean)[item.line - 1]);
        setResults(current => [...current, { line: item.line, ok: !message, message: message || '测试通过，已导入' }]);
      }
      setText(failedLines.join('\n'));
    } catch (error) { setError(error.message); } finally { setBusy(false); }
  }
  return <Dialog title="导入 SOCKS5" onClose={() => { if (!busy) onClose(); }}><form onSubmit={submit}><p className="muted">每行一条，支持 socks5://主机:端口:用户名:密码 和 socks5://用户名:密码@主机:端口。逐条测试通过后保存，失败项保留以便重试。</p><label>代理列表<textarea aria-label="代理导入内容" value={text} onChange={event => setText(event.target.value)} rows={6} required disabled={busy} autoComplete="off" spellCheck={false} /></label>{error && <p className="error" role="alert">{error}</p>}<div className="browser-buttons"><button className="primary" disabled={busy || !text.trim()}>{busy ? '正在测试并导入…' : '测试并导入'}</button><button className="outline" type="button" disabled={busy} onClick={onClose}>关闭</button></div></form><div className="import-results" aria-live="polite">{results.map(result => <p key={result.line} className={result.ok ? 'credit' : 'danger'}>第 {result.line} 行：{result.message}</p>)}</div></Dialog>;
}

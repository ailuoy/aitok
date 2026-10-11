import React, { useState } from 'react';
import { request } from './api';
import { readLocalProxyConfig } from './sharedProxies';
import { proxyURL } from '../../shared/proxy-url.mjs';
import Dialog from './Dialog';
import Select from './Select';

export default function ProxyExport({ token, hasSharedProxies, onClose }) {
  const [source, setSource] = useState(hasSharedProxies ? 'shared' : 'local');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  async function download(event) {
    event.preventDefault();
    if (busy) return;
    setBusy(true); setError('');
    try {
      let proxies;
      if (source === 'local') {
        ({ proxies } = await readLocalProxyConfig());
      } else {
        const config = await request('/proxies', token);
        proxies = [];
        for (const proxy of config.proxies) proxies.push(await request('/proxies/' + proxy.id, token));
      }
      if (!proxies.length) throw new Error(source === 'local' ? '当前电脑没有可导出的代理' : '暂无可导出的共享代理');
      // 任意一条读取失败或缺少凭据时不下载残缺文件。
      if (proxies.some(proxy => typeof proxy.password !== 'string' || (Boolean(proxy.username) !== Boolean(proxy.password)))) throw new Error('代理认证信息不完整，请刷新后重试');
      const text = proxies.map(proxyURL).join('\n') + '\n';
      const url = URL.createObjectURL(new Blob([text], { type: 'text/plain;charset=utf-8' }));
      const link = document.createElement('a');
      link.href = url;
      link.download = `socks5-${source}-${Date.now()}.txt`;
      document.body.appendChild(link);
      try { link.click(); }
      finally { link.remove(); setTimeout(() => URL.revokeObjectURL(url), 1000); }
      onClose();
    } catch (error) { setError(error.message); }
    finally { setBusy(false); }
  }

  return <Dialog title="导出 SOCKS5" onClose={() => { if (!busy) onClose(); }}>
    <form onSubmit={download}>
      <label>导出来源<Select label="导出代理来源" value={source} onChange={value => { setSource(value); setError(''); }} disabled={busy} options={[{ value: 'shared', label: '共享代理' }, { value: 'local', label: '本机已有代理' }]} /></label>
      <p className="muted">下载 TXT 文件，每行一条 SOCKS5 地址，可粘贴到“导入代理”中使用。文件包含用户名和密码，请妥善保管；不包含代理名称、账号绑定及使用记录。</p>
      {source === 'local' && <p className="muted">请先启动保存这些代理的本机助手。</p>}
      {error && <p className="error" role="alert">{error}</p>}
      <div className="browser-buttons"><button className="primary" disabled={busy}>{busy ? '正在导出…' : '下载 TXT'}</button><button type="button" className="outline" disabled={busy} onClick={onClose}>取消</button></div>
    </form>
  </Dialog>;
}

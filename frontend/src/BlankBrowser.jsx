import React, { useState } from 'react';
import { Monitor } from 'lucide-react';
import Dialog from './Dialog';
import Select from './Select';
import { launcherPort, launcherRequest } from './localBrowser';

export default function BlankBrowser({ port }) {
  const [open, setOpen] = useState(false), [proxies, setProxies] = useState([]), [proxyID, setProxyID] = useState('');
  const [busy, setBusy] = useState(false), [loading, setLoading] = useState(false), [error, setError] = useState('');
  async function load() {
    setLoading(true); setError('');
    try {
      const health = await launcherRequest('/health', { port });
      if (!health.blank_browser) throw new Error('请安装并重启支持空白浏览器的新版桌面助手');
      const config = await launcherRequest('/proxies', { port });
      setProxies(config.proxies);
      setProxyID(current => config.proxies.some(proxy => proxy.id === current) ? current : '');
    } catch (error) { setProxies([]); setError(error.message); }
    finally { setLoading(false); }
  }
  async function start(event) {
    event.preventDefault();
    if (busy || loading || !proxyID) return;
    setBusy(true); setError('');
    try {
      if (port !== launcherPort()) throw new Error('本机端口已更改，请关闭弹窗后重试');
      await launcherRequest('/blank-browsers', { port, method: 'POST', body: { proxy_id: proxyID } });
      setOpen(false);
    } catch (error) { setError(error.message); }
    finally { setBusy(false); }
  }
  return <>
    <button className="outline small" onClick={() => { setOpen(true); setProxyID(''); void load(); }}><Monitor size={15} />打开空白浏览器</button>
    {open && <Dialog title="打开空白浏览器" onClose={() => { if (!busy) setOpen(false); }}>
      <form onSubmit={start}><p className="muted">选择代理后打开独立空白窗口，在地址栏访问 chatgpt.com 登录账号。</p>
        <label>SOCKS5 代理<Select label="空白浏览器代理" value={proxyID} onChange={setProxyID} disabled={busy || loading} options={[{ value: '', label: loading ? '正在加载代理…' : '请选择代理' }, ...proxies.map(proxy => ({ value: proxy.id, label: `${proxy.name} · ${proxy.host}:${proxy.port}` }))]} /></label>
        {!loading && !error && !proxies.length && <p className="muted">暂无代理，请先在 SOCKS5 管理中添加并测试代理。</p>}
        {error && <p className="error" role="alert">{error}</p>}
        <div className="browser-buttons"><button className="primary" disabled={busy || loading || !proxyID}>{busy ? '正在打开…' : '打开浏览器'}</button><button className="outline" type="button" disabled={busy || loading} onClick={load}>刷新代理</button></div>
      </form>
    </Dialog>}
  </>;
}

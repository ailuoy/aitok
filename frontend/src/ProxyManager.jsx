import React, { useState } from 'react';
import { Plus, RefreshCw } from 'lucide-react';
import { launcherCommand, launcherRequest } from './localBrowser';
import Dialog from './Dialog';
import ProxyHistory from './ProxyHistory';
import { formatUTC8 } from './time';
import ProxyImport from './ProxyImport';
import DataTable from './DataTable';

export default function ProxyManager({ config, error: connectionError, refresh, onChange }) {
  const [editing, setEditing] = useState(null);
  const [deleting, setDeleting] = useState(null);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [tested, setTested] = useState(null);
  const [history, setHistory] = useState(null);
  const [importing, setImporting] = useState(false);

  async function action(key, path, options) {
    if (busy) return;
    setBusy(key); setError('');
    try { onChange(await launcherRequest(path, options)); setEditing(null); setDeleting(null); }
    catch (error) { setError(error.message); }
    finally { setBusy(''); }
  }

  async function edit(proxy) {
    setBusy('edit'); setError(''); setTested(null);
    try { setEditing(await launcherRequest('/proxies/' + proxy.id)); }
    catch (error) { setError(error.message); }
    finally { setBusy(''); }
  }

  async function save(event) {
    event.preventDefault();
    if (busy) return;
    const fields = new FormData(event.currentTarget);
    const body = Object.fromEntries(fields);
    if (event.nativeEvent.submitter?.value === 'test') {
      setBusy('draft-test'); setError(''); setTested(null);
      try { setTested(await launcherRequest('/proxies/test', { method: 'POST', body: { ...body, id: editing.id } })); }
      catch (error) { setError(error.message); }
      finally { setBusy(''); }
      return;
    }
    if (!tested?.result.ok || !tested.test_token) return;
    await action('save', '/proxies' + (editing.id ? '/' + editing.id : ''), { method: editing.id ? 'PATCH' : 'POST', body: { ...body, test_token: tested.test_token } });
    setTested(null);
  }

  return <section className="account-section proxy-manager">
    <div className="section-title"><h2>SOCKS5 管理</h2><div className="browser-buttons"><button className="outline small" disabled={!config} onClick={() => setImporting(true)}>导入代理</button><button className="outline small" disabled={!config} onClick={() => setHistory({})}>全部使用记录</button><button className="text-btn" onClick={refresh}><RefreshCw size={15} />刷新</button><button className="primary small" disabled={!config || Boolean(busy)} onClick={() => { setEditing({}); setError(''); setTested(null); }}><Plus size={15} />添加代理</button></div></div>
    <p className="muted">代理与账号绑定保存在本机。测试通过所选 SOCKS5 访问 api.ipify.org；出口 IP 与代理主机 IP 一致显示绿色，不一致显示红色。</p>
    {connectionError && <div className="notice"><p>{connectionError}</p><p>在本机项目目录运行启动器后点击刷新：</p><pre className="launcher-command"><code>{launcherCommand()}</code></pre></div>}
    {error && <p className="error" role="alert">{error}</p>}
    {editing && <form className="proxy-form" onSubmit={save} onChange={() => setTested(null)} key={editing.id || 'new'}>
      <h3>{editing.id ? '编辑代理' : '添加代理'}</h3>
      <fieldset className="proxy-fields" disabled={Boolean(busy)}><label>名称<input name="name" required maxLength={80} defaultValue={editing.name} placeholder="例如：美国住宅 IP" autoFocus /></label><label>主机 IP / 域名<input name="host" required maxLength={253} defaultValue={editing.host} placeholder="203.0.113.10" /></label><label>端口<input name="port" type="number" required min={1} max={65535} defaultValue={editing.port || 1080} /></label><label>用户名（可选）<input name="username" defaultValue={editing.username} autoComplete="off" /></label><label>密码（可选）<input name="password" type="text" defaultValue={editing.password || ''} autoComplete="off" placeholder="与用户名同时填写" /></label></fieldset>
      {tested ? <p role="status" className={tested.result.ok && tested.result.matches ? 'proxy-result match' : 'proxy-result mismatch'}>{tested.result.ok ? `测试通过，出口 IP：${tested.result.exit_ip}（${tested.result.matches ? '与代理 IP 一致' : '与代理 IP 不一致'}），可以保存。` : tested.result.error}</p> : <p className="muted">请先测试当前配置，测试通过后才能保存。修改配置后需重新测试。</p>}
      <div className="browser-buttons"><button className="primary small" disabled={Boolean(busy) || !tested?.result.ok || !tested.test_token}>{busy === 'save' ? '保存中…' : '保存代理'}</button><button type="submit" value="test" className="outline small" disabled={Boolean(busy)}>{busy === 'draft-test' ? '测试中…' : '测试'}</button><button type="button" className="outline small" disabled={Boolean(busy)} onClick={() => { setEditing(null); setTested(null); }}>取消</button></div>
    </form>}
    {config && <DataTable label="SOCKS5 列表" className="proxy-table" columns={['名称', '主机 / 端口', '用户名', '出口 IP / 状态', '延迟', '上次测试（UTC+8）', '操作']} empty={!config.proxies.length && '还没有 SOCKS5 代理，添加后可在账号表格中选择。'}>
      {config.proxies.map(proxy => {
        const result = proxy.last_test;
        return <tr className="proxy-row" key={proxy.id}>
          <td className="table-text"><strong>{proxy.name}</strong></td><td className="table-mono">{proxy.host}:{proxy.port}</td><td className="table-text">{proxy.username || '无认证'}</td>
          <td className="table-text">{result ? <div className={result.ok && result.matches ? 'proxy-result match' : 'proxy-result mismatch'} role="status">{result.ok ? <><b>{result.exit_ip}</b><span>{result.matches ? '与代理 IP 一致' : '与代理 IP 不一致'}</span><small>代理 IP：{result.proxy_ips.join(' / ')}</small></> : <b>{result.error}</b>}</div> : <span className="muted">尚未测试</span>}</td>
          <td>{result?.ok ? result.latency_ms + ' ms' : '—'}</td><td>{formatUTC8(result?.tested_at)}</td>
          <td className="table-actions"><div className="row-actions"><button className="outline small" onClick={() => setHistory(proxy)}>使用记录</button><button className="outline small" disabled={Boolean(busy)} onClick={() => action(proxy.id + ':test', '/proxies/' + proxy.id + '/test', { method: 'POST', body: {} })}>{busy === proxy.id + ':test' ? '测试中…' : '测试'}</button><button className="outline small" disabled={Boolean(busy)} onClick={() => action(proxy.id + ':ip', '/proxies/' + proxy.id + '/test', { method: 'POST', body: { action: 'get_ip' } })}>{busy === proxy.id + ':ip' ? '获取中…' : '获取 IP'}</button><button className="outline small" disabled={Boolean(busy)} onClick={() => edit(proxy)}>编辑</button><button className="text-btn danger" disabled={Boolean(busy)} onClick={() => { setDeleting(proxy); setError(''); }}>删除</button></div></td>
        </tr>;
      })}
    </DataTable>}
    {importing && <ProxyImport onClose={() => setImporting(false)} onChange={onChange} />}
    {history && <ProxyHistory proxy={history.id ? history : null} onClose={() => setHistory(null)} />}
    {deleting && <Dialog title="删除代理" onClose={() => { if (!busy) setDeleting(null); }}><p>确认删除「{deleting.name}」？此操作无法撤销。</p>{error && <p className="error" role="alert">{error}</p>}<div className="browser-buttons"><button className="outline small danger" disabled={Boolean(busy)} onClick={() => action('delete', '/proxies/' + deleting.id, { method: 'DELETE' })}>{busy === 'delete' ? '删除中…' : '确认删除'}</button><button className="text-btn" disabled={Boolean(busy)} onClick={() => setDeleting(null)}>取消</button></div></Dialog>}
  </section>;
}

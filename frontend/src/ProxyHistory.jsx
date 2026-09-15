import Pagination from './Pagination';
import React, { useEffect, useState } from 'react';
import { launcherRequest } from './localBrowser';
import { formatUTC8 } from './time';
import Dialog from './Dialog';

const actions = { test: '测试代理', get_ip: '获取 IP', open: '打开账号', ip_check: '浏览器 IP 核对', login: '确认账号登录', close: '关闭浏览器' };
async function readHistory(proxyID, page, pageSize, signal) {
  const read = p => launcherRequest('/proxy-history?' + new URLSearchParams({ proxy_id: proxyID, page: p, page_size: pageSize }), { signal });
  const result = await read(page);
  if (result.page_size === pageSize) return result;
  // 兼容仍在运行、每页固定 20 条的旧启动器，无需关闭正在使用的浏览器。
  const size = result.page_size || 20, offset = (page - 1) * pageSize;
  const first = Math.floor(offset / size) + 1;
  const last = Math.ceil(Math.min(result.total, offset + pageSize) / size);
  const chunks = await Promise.all(Array.from({ length: Math.max(0, last - first + 1) }, (_, i) => first + i === page ? result : read(first + i)));
  return { ...result, page, page_size: pageSize, records: chunks.flatMap(chunk => chunk.records).slice(offset % size, offset % size + pageSize) };
}
export default function ProxyHistory({ proxy, onClose }) {
  const [pageSize, setPageSize] = useState(20);
  const [page, setPage] = useState(1);
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(true);
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError('');
    readHistory(proxy?.id || '', page, pageSize, controller.signal)
      .then(data => { if (!controller.signal.aborted) setData(data); })
      .catch(error => { if (!controller.signal.aborted) setError(error.message); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [proxy?.id, page, pageSize, revision]);
  return <Dialog title={proxy ? proxy.name + ' · 使用记录' : '全部代理使用记录'} onClose={onClose}><p className="muted">时间为 UTC+8。记录保存在本机，包含账号使用、测试和 IP 核对；代理删除后历史仍保留。</p><button className="text-btn" disabled={loading} onClick={() => setRevision(value => value + 1)}>刷新记录</button>{error && <p className="error" role="alert">{error}</p>}
    {loading ? <p className="muted">正在读取记录…</p> : <div className="usage-list">{data?.records.map(row => <article className="usage-row" key={row.id}><div><strong>{actions[row.action] || row.action}</strong><span className={row.ok ? 'credit' : 'danger'}>{row.ok ? '成功' : '失败'}</span></div><time>{formatUTC8(row.created_at)}</time><span>{row.email || row.environment_id || '代理检测'}</span>{!proxy && <span>{row.proxy_name} · {row.proxy_address}</span>}{row.exit_ip && <span className={row.matches === false ? 'danger' : 'credit'}>出口 IP：{row.exit_ip}{row.matches === null ? '' : row.matches ? ' · IP 一致' : ' · IP 不一致'}</span>}</article>)}{!data?.records.length && <p className="empty muted">暂无使用记录，新操作会自动记录。</p>}</div>}
    <Pagination page={page} pageSize={pageSize} total={data?.total || 0} onPageChange={setPage} onPageSizeChange={setPageSize} disabled={loading} />
  </Dialog>;
}

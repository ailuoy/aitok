import Highlight from './Highlight';
import Pagination from './Pagination';
import React, { useEffect, useState } from 'react';
import { request } from './api';
import DataTable from './DataTable';
import { Link } from './router';
import { formatUTC8 } from './time';

const auditPages = { accounts: '账号', proxies: 'SOCKS5', addresses: '地址', 'bank-cards': '银行卡', users: '用户', wallet: '钱包', orders: '充值订单', packages: '充值套餐', notices: '到期与异常', audit: '操作审计', 'proxy-activity': '设备使用记录', 'payment-exceptions': '支付退款与异常' };
function AuditRow({ event: e, query }) {
  const data = e.after_data?._request || e.after_data || {};
  const result = data.result || (data.status >= 400 ? 'failure' : data.status >= 200 ? 'success' : '');
  const label = { visited: '已访问', triggered: '已触发', success: '成功', failure: '失败', cancelled: '已取消' }[result] || '已记录';
  return <Highlight query={query}><tr><td>{formatUTC8(e.created_at)}</td><td>{e.actor}<small className="cell-secondary">用户 #{e.actor_id}</small></td><td>{auditPages[data.page?.split('/')[2]] || '—'}{data.page && <small className="cell-secondary">{data.page}</small>}</td><td className="table-text">{e.action}</td><td className={result === 'failure' ? 'danger' : ''}>{label}{data.status > 0 && <small className="cell-secondary">HTTP {data.status}</small>}</td><td>{data.source === 'browser' ? '前端上报' : data.source === 'server' ? '后端执行' : '业务记录'}</td><td className="table-text">{data.resource || `${e.entity_type} / ${e.entity_id}`}</td><td><details><summary>查看记录</summary><pre className="operation-json">{JSON.stringify({ before: e.before_data, after: e.after_data }, null, 2)}</pre></details></td></tr></Highlight>;
}


export default function OperationHistory({ token, mode }) {
  const [pageSize, setPageSize] = useState(20);
  const [data, setData] = useState({}), [page, setPage] = useState(1), [error, setError] = useState(''), [query, setQuery] = useState(''), [revision, refresh] = useState(0);
  useEffect(() => { const c = new AbortController(); setError(''); request('/' + mode + '?' + new URLSearchParams({ page, page_size: pageSize, q: query }), token, { signal: c.signal }).then(v => { if (!c.signal.aborted) setData(v); }).catch(e => { if (!c.signal.aborted) setError(e.message); }); return () => c.abort(); }, [mode, token, page, pageSize, query, revision]);
  const rows = data.events || data.notices || data.activities || [];
  return <section className="account-section"><div className="section-title"><h2>{{ audit: '操作审计', notices: '到期与异常提醒', 'proxy-activity': '设备代理使用记录' }[mode]}</h2><button className="outline small" onClick={() => refresh(v => v + 1)}>刷新</button></div>{error && <p className="error" role="alert">{error}</p>}
    {mode === 'audit' && <input aria-label="搜索审计操作" placeholder="搜索用户、页面路径或操作" value={query} onChange={e => { setQuery(e.target.value); setPage(1); }} />}
    {mode === 'notices' ? <DataTable label="业务提醒" columns={['事项', '关联记录', '说明', '操作']} empty={!rows.length && '当前没有到期或异常提醒。'}>{rows.map(row => <tr key={row.kind + row.id}><td>{{ renewal: '订阅到期', order: '订单待处理', card: '低余额', payment: '支付异常' }[row.kind]}</td><td>{row.label}</td><td>{row.detail}</td><td><Link to={row.path}>前往处理</Link></td></tr>)}</DataTable> : mode === 'audit' ? <DataTable label="操作审计" columns={['时间（UTC+8）', '操作者', '页面', '操作', '结果', '记录来源', '关联资源', '详情']} stickyActions={false} empty={!rows.length && '暂无审计记录。'}>{rows.map(e => <AuditRow key={e.id} event={e} query={query} />)}</DataTable> : <DataTable label="设备代理记录" columns={['时间', '设备', '账号', '代理', '操作', '结果']} stickyActions={false} empty={!rows.length && '暂无同步记录。使用本机启动器后将自动同步。'}>{rows.map(e => <tr key={e.id}><td>{formatUTC8(e.data.created_at)}</td><td>{e.device_id}</td><td>{e.data.email || '—'}</td><td>{e.data.proxy_name || '—'}</td><td>{e.data.action}</td><td>{e.data.ok ? '成功' : '失败'} · {e.data.exit_ip || '—'}</td></tr>)}</DataTable>}
    {mode !== 'notices' && <Pagination page={page} pageSize={pageSize} total={data.total || 0} onPageChange={setPage} onPageSizeChange={setPageSize} disabled={false} />}
  </section>;
}

import React, { useEffect, useState } from 'react';
import { request } from './api';
import DataTable from './DataTable';
import Pagination from './Pagination';
import { formatCardUSD } from './BankCardLedger';
import { formatUTC8 } from './time';

export default function ConsumptionOrders({ token }) {
  const [page, setPage] = useState(1), [pageSize, setPageSize] = useState(20), [revision, refresh] = useState(0);
  const [data, setData] = useState(null), [error, setError] = useState(''), [loading, setLoading] = useState(true);
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError('');
    request('/consumption-orders?' + new URLSearchParams({ page, page_size: pageSize }), token, { signal: controller.signal })
      .then(value => { if (!controller.signal.aborted) setData(value); })
      .catch(e => { if (!controller.signal.aborted) { setError(e.message); setData(null); } })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [token, page, pageSize, revision]);
  return <section className="account-section consumption-orders">
    <div className="section-title"><h2>消费订单</h2><button className="outline small" disabled={loading} onClick={() => refresh(v => v + 1)}>刷新</button></div>
    <div className="consumption-balance"><span className="muted">当前钱包余额</span><div><strong>{data ? data.balance.toLocaleString('zh-CN', { maximumFractionDigits: 2 }) : '—'}</strong><span>代币</span></div></div>
    {error && <p className="error" role="alert">{error}</p>}
    {loading ? <p role="status">正在加载…</p> : data && <>
      <DataTable label="消费订单" columns={['账号', '消费 USD', '扣款后余额（代币）', '时间（UTC+8）']} empty={!data.orders.length && '暂无消费订单。'}>
        {data.orders.map(item => <tr key={item.id}><td className="table-text"><strong>{item.account_label || item.account_email}</strong>{item.account_label && item.account_label !== item.account_email && <small className="cell-secondary">{item.account_email}</small>}</td><td><strong>{formatCardUSD(item.amount_usd_minor)}</strong></td><td>{(item.balance_after_minor / 100).toLocaleString('zh-CN', { maximumFractionDigits: 2 })}</td><td>{formatUTC8(item.created_at)}</td></tr>)}
      </DataTable>
      <Pagination page={page} pageSize={pageSize} total={data.total} onPageChange={setPage} onPageSizeChange={setPageSize} disabled={loading} />
    </>}
  </section>;
}

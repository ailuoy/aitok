import React, { useEffect, useState } from 'react';
import { request } from './api';
import Dialog from './Dialog';
import DataTable from './DataTable';
import Pagination from './Pagination';
import { formatCardUSD } from './BankCardLedger';
import { formatAccountPlan, formatPackageName } from './packageLabels';

export function CardRenewalDifference({ summary }) {
  if (!summary) return <span className="muted">—</span>;
  const difference = summary.difference_usd_minor;
  return <div className="card-renewal-difference" title={`${summary.month}，可用 USD 减去本月预计续费金额`}>
    {difference == null ? <strong className="muted">待核算</strong> : <strong className={difference < 0 ? 'danger' : 'credit'}>{difference < 0 ? '−' : '+'}{formatCardUSD(Math.abs(difference))}</strong>}
    <small className="cell-secondary">{summary.renewal_count} 个续费 · {summary.unknown_count ? '已知需' : '需'} {formatCardUSD(summary.required_usd_minor)}</small>
    {summary.unknown_count > 0 && <small className="cell-secondary">{summary.unknown_count} 个账号待核算</small>}
  </div>;
}

export default function BankCardAccounts({ card, token, onClose }) {
  const [data, setData] = useState(null);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError('');
    request(`/bank-cards/${card.id}/accounts?` + new URLSearchParams({ page, page_size: pageSize }), token, { signal: controller.signal })
      .then(value => { if (!controller.signal.aborted) setData(value); })
      .catch(error => { if (!controller.signal.aborted) setError(error.message); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [card.id, token, page, pageSize, revision]);
  return <Dialog className="card-accounts-dialog" title={`${card.label} · 尾号 ${card.last4} 绑定账号`} onClose={onClose}>
    <p className="muted">本月按北京时间自然月统计，包含已开启续订且续费日期在本月的账号（含本月已逾期）。按当前套餐价格预估一次续费，可用余额已扣除预授权占用。</p>
    {error && <p className="error" role="alert">{error}<button className="text-btn" onClick={() => setRevision(value => value + 1)}>重试</button></p>}
    {loading ? <p className="muted" role="status">正在加载绑定账号…</p> : !error && data && <>
      <div className="card-ledger-summary"><div><span>绑定账号</span><strong>{data.total} 个</strong></div><div><span>可用余额 · USD</span><strong>{formatCardUSD(data.available_usd_minor)}</strong></div><div><span>{data.renewal.month} 续费余额差</span><CardRenewalDifference summary={data.renewal} /></div></div>
      <DataTable label="银行卡绑定账号" className="card-accounts-table" stickyActions={false} columns={['账号', '套餐类型', '续费金额 · USD', '续费日期', '本月续费']} empty={!data.accounts.length && '此卡暂无绑定账号。'}>
        {data.accounts.map(account => <tr key={account.id}>
          <td className="table-text"><strong>{account.label}</strong>{account.label !== account.email && <small className="cell-secondary">{account.email}</small>}</td>
          <td className="table-text">{account.subscription_plan ? formatAccountPlan(account.subscription_plan) : account.subscription_package_id ? '套餐已删除' : '未设置'}{account.package_name && <small className="cell-secondary">{formatPackageName(account.package_name)}</small>}</td>
          <td>{account.renewal_usd_minor == null ? '待核算' : formatCardUSD(account.renewal_usd_minor)}</td>
          <td>{account.renewal_date || '未设置'}</td>
          <td>{account.in_month ? '计入本月' : !account.renewal_enabled ? '未开启续订' : !account.renewal_date ? '未设置日期' : '非本月'}</td>
        </tr>)}
      </DataTable>
    </>}
    <Pagination page={page} pageSize={pageSize} total={data?.total || 0} onPageChange={setPage} onPageSizeChange={size => { setPageSize(size); setPage(1); }} disabled={loading || !!error} />
  </Dialog>;
}

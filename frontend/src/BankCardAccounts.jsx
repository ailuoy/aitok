import React, { useEffect, useState } from 'react';
import { request } from './api';
import Dialog from './Dialog';
import DataTable from './DataTable';
import Pagination from './Pagination';
import { formatCardUSD } from './BankCardLedger';
import { formatAccountPlan, formatPackageName } from './packageLabels';
import RenewalCountdown from './RenewalCountdown';

export function CardUpcomingRenewals({ summary, onClick, label }) {
  if (!summary) return <span className="muted">—</span>;
  const content = <span>
    <strong>{summary.renewal_count} 个续费</strong>
    <small className="cell-secondary">{summary.unknown_count ? '已知需' : '需'} {formatCardUSD(summary.required_usd_minor)}</small>
    {summary.unknown_count > 0 && <small className="cell-secondary">{summary.unknown_count} 个账号待核算</small>}
  </span>;
  const title = '北京时间，含今天在内未来 10 天已开启续订的账号，按当前套餐价格预估';
  return onClick ? <button type="button" className="text-btn" style={{ textAlign: 'left' }} title={title} aria-label={label} onClick={onClick}>{content}</button> : <div title={title}>{content}</div>;
}

export function CardRenewalDifference({ summary }) {
  if (!summary) return <span className="muted">—</span>;
  const difference = summary.difference_usd_minor;
  return <div className="card-renewal-difference" title={`${summary.month}，可用 USD 减去本月预计续费金额`}>
    {difference == null ? <strong className="muted">待核算</strong> : <strong className={difference < 0 ? 'danger' : 'credit'}>{difference < 0 ? '−' : '+'}{formatCardUSD(Math.abs(difference))}</strong>}
    <small className="cell-secondary">{summary.renewal_count} 个续费 · {summary.unknown_count ? '已知需' : '需'} {formatCardUSD(summary.required_usd_minor)}</small>
    {summary.unknown_count > 0 && <small className="cell-secondary">{summary.unknown_count} 个账号待核算</small>}
  </div>;
}

export default function BankCardAccounts({ card, token, onClose, upcoming = false, onOpenBrowser }) {
  const [data, setData] = useState(null);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError('');
    request(`/bank-cards/${card.id}/accounts?` + new URLSearchParams({ page, page_size: pageSize, ...(upcoming ? { renewal_window: 'next_10_days' } : {}) }), token, { signal: controller.signal })
      .then(value => { if (!controller.signal.aborted) setData(value); })
      .catch(error => { if (!controller.signal.aborted) setError(error.message); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [card.id, token, page, pageSize, revision, upcoming]);
  return <Dialog className="card-accounts-dialog" title={`${card.label} · 尾号 ${card.last4} ${upcoming ? '近 10 天续费账号' : '绑定账号'}`} onClose={onClose}>
    <p className="muted">{upcoming ? '按北京时间统计含今天在内未来 10 天已开启续订的账号，按续费日期由近到远排列。金额按当前套餐预估；打开浏览器需要本机助手。' : '本月按北京时间自然月统计，包含已开启续订且续费日期在本月的账号（含本月已逾期）。按当前套餐价格预估一次续费，可用余额已扣除预授权占用。'}</p>
    {error && <p className="error" role="alert">{error}<button className="text-btn" onClick={() => setRevision(value => value + 1)}>重试</button></p>}
    {loading ? <p className="muted" role="status">{upcoming ? '正在加载续费账号…' : '正在加载绑定账号…'}</p> : !error && data && <>
      <div className="card-ledger-summary"><div><span>{upcoming ? '近 10 天续费账号' : '绑定账号'}</span><strong>{data.total} 个</strong></div><div><span>可用余额 · USD</span><strong>{formatCardUSD(data.available_usd_minor)}</strong></div><div><span>{upcoming ? '预计续费 · USD' : `${data.renewal.month} 续费余额差`}</span>{upcoming ? <><strong>{formatCardUSD(data.renewal.next_10_days.required_usd_minor)}</strong>{data.renewal.next_10_days.unknown_count > 0 && <small className="muted">已知金额 · {data.renewal.next_10_days.unknown_count} 个账号待核算</small>}</> : <CardRenewalDifference summary={data.renewal} />}</div></div>
      <DataTable label={upcoming ? '近 10 天续费账号' : '银行卡绑定账号'} className="card-accounts-table" stickyActions={false} columns={['账号', '套餐类型', '续费金额 · USD', '续费日期', ...(upcoming ? ['续费倒计时', '操作'] : ['本月续费'])]} empty={!data.accounts.length && (upcoming ? '此卡近 10 天暂无需要续费的账号。' : '此卡暂无绑定账号。')}>
        {data.accounts.map(account => <tr data-audit-account-id={account.id} key={account.id}>
          <td className="table-text"><strong>{account.label}</strong>{account.label !== account.email && <small className="cell-secondary">{account.email}</small>}</td>
          <td className="table-text">{account.subscription_plan ? formatAccountPlan(account.subscription_plan) : account.subscription_package_id ? '套餐已删除' : '未设置'}{account.package_name && <small className="cell-secondary">{formatPackageName(account.package_name)}</small>}</td>
          <td>{account.renewal_usd_minor == null ? '待核算' : formatCardUSD(account.renewal_usd_minor)}</td>
          <td>{account.renewal_date || '未设置'}</td>
          {upcoming ? <>
            <td><RenewalCountdown date={account.renewal_date} today={data.today} /></td>
            <td><button type="button" className="outline small" disabled={!account.has_session || !onOpenBrowser} onClick={() => onOpenBrowser(account)}>打开浏览器</button>{!account.has_session && <small className="cell-secondary">请先在账号管理中更新 Session</small>}</td>
          </> : <td>{account.in_month ? '计入本月' : !account.renewal_enabled ? '未开启续订' : !account.renewal_date ? '未设置日期' : '非本月'}</td>}
        </tr>)}
      </DataTable>
    </>}
    <Pagination page={page} pageSize={pageSize} total={data?.total || 0} onPageChange={setPage} onPageSizeChange={size => { setPageSize(size); setPage(1); }} disabled={loading || !!error} />
  </Dialog>;
}

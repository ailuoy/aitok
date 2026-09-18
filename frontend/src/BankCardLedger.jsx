import Pagination from './Pagination';
import React, { useEffect, useRef, useState } from 'react';
import { request } from './api';
import Dialog from './Dialog';
import DataTable from './DataTable';
import Select from './Select';
import { formatUTC8 } from './time';

export const formatCardUSD = value => new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }).format((value ?? 0) / 100);
const kindLabels = { opening: '初始余额', deposit: '存入', subscription: '账号开通',refund:'退款入卡',reversal:'冲正',fee:'手续费',adjustment:'余额调整' };

export default function BankCardLedger({ card, token, accounts, onChange, onClose }) {
  const [pageSize, setPageSize] = useState(20);
  const [data, setData] = useState(null);
  const [page, setPage] = useState(1);
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [composing, setComposing] = useState(false);
  const [kind, setKind] = useState('deposit');
  const [amount, setAmount] = useState('');
  const [packages, setPackages] = useState([]);
  const [packagesLoading, setPackagesLoading] = useState(false);
  const [packageError, setPackageError] = useState('');
  const [packageID, setPackageID] = useState('');
  const [chargeMode, setChargeMode] = useState('package');
  const [accountID, setAccountID] = useState('');
  const [notes, setNotes] = useState('');
  const [confirmation, setConfirmation] = useState(null);
  const pending = useRef(null);
  const writing = useRef(false);
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError('');
    request(`/bank-cards/${card.id}/ledger?page=${page}&page_size=${pageSize}`, token, { signal: controller.signal })
      .then(value => { if (!controller.signal.aborted) setData(value); })
      .catch(error => { if (!controller.signal.aborted) setError(error.message); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [card.id, token, page, pageSize, revision]);
  useEffect(() => {
    if (!composing || kind !== 'subscription') return;
    const controller = new AbortController(); setPackagesLoading(true); setPackageError('');
    request('/packages', token, { signal: controller.signal })
      .then(value => { if (!controller.signal.aborted) setPackages(value.packages.filter(item => item.enabled)); })
      .catch(error => { if (!controller.signal.aborted) { setPackages([]); setPackageError(error.message); } })
      .finally(() => { if (!controller.signal.aborted) setPackagesLoading(false); });
    return () => controller.abort();
  }, [token, composing, kind]);
  async function prepare(event) {
    event.preventDefault(); setError('');
    if (writing.current) return;
    if ((kind === 'deposit' || chargeMode !== 'package') && (!/^(0|[1-9]\d{0,10})(\.\d{1,2})?$/.test(amount) || Number(amount) <= 0 || Number(amount) > 10000000000)) { setError('请输入大于 0、最多两位小数的金额'); return; }
    if (kind === 'subscription' && !accountID) { setError('请选择已开通成功的账号'); return; }
    if (kind === 'subscription' && (!packageID || packagesLoading || packageError)) { setError('请选择可用套餐'); return; }
    const fields=Object.fromEntries(new FormData(event.currentTarget));
    const payload = { ...fields, kind, account_id: kind === 'subscription' ? Number(accountID) : 0, notes: notes.trim() };
    writing.current = true; setBusy(true);
    try {
      let pricing;
      if (kind === 'subscription') {
        const charge = { package_id: packageID, charge_mode: chargeMode, charge_amount: chargeMode === 'package' ? '' : amount };
        pricing = await request(`/bank-cards/${card.id}/ledger?` + new URLSearchParams({ quote: '1', ...charge }), token);
        Object.assign(payload, charge, { package_id: Number(packageID), expected_amount_usd_minor: pricing.amount_usd_minor, rate_id: pricing.exchange_rate?.batch?.id || 0 });
      } else payload.amount_usd = amount;
      const fingerprint = JSON.stringify(payload);
      // 请求失败后重试沿用相同键，避免响应丢失时重复入账。
      if (pending.current?.fingerprint !== fingerprint) pending.current = { fingerprint, body: { ...payload, request_key: crypto.randomUUID() } };
      setConfirmation({ ...pending.current.body, pricing });
    } catch (error) { setError(error.message); }
    finally { writing.current = false; setBusy(false); }
  }
  async function save() {
    if (writing.current) return;
    writing.current = true; setBusy(true); setError('');
    try {
      await request(`/bank-cards/${card.id}/ledger`, token, { method: 'POST', body: pending.current.body });
      pending.current = null; setConfirmation(null); setComposing(false); setAmount(''); setNotes(''); setAccountID(''); setPackageID(''); setChargeMode('package'); setPage(1); setRevision(value => value + 1); onChange();
    } catch (error) { setError(error.message); }
    finally { writing.current = false; setBusy(false); }
  }
  const selectedAccount = accounts.find(account => String(account.id) === String(confirmation?.account_id));
  const selectedPackage = packages.find(item => String(item.id) === packageID);
  const confirmedAmount = confirmation?.pricing?.amount_usd_minor ?? Math.round(Number(confirmation?.amount_usd || 0) * 100);
  const balanceAfter = (data?.balance_usd_minor || 0) + (confirmation?.kind === 'deposit' ? confirmedAmount : -confirmedAmount);
  return <Dialog title={`${card.label} · ${card.last4} 对账单`} onClose={() => { if (!busy) onClose(); }}>
    <p className="muted">USD 账本用于记录已发生的存入和消费，不会向银行卡实际转账或发起付款。</p>
    {data && <div className="card-ledger-summary"><div><span>当前余额 · USD</span><strong>{formatCardUSD(data.balance_usd_minor)}</strong></div><div><span>累计入账 · USD</span><strong>{formatCardUSD(data.deposited_usd_minor)}</strong></div><div><span>累计支出 · USD</span><strong>{formatCardUSD(data.spent_usd_minor)}</strong></div></div>}
    {error && <p className="error" role="alert">{error}</p>}
    {!composing && <div className="browser-buttons"><button className="primary small" disabled={loading || !data || !!card.deleted_at} onClick={() => { setKind('deposit'); setComposing(true); }}>记录存入</button><button className="outline small" disabled={loading || !data || !!card.deleted_at} onClick={() => { setKind('subscription'); setComposing(true); }}>记录开通扣款</button></div>}
    {confirmation ? <div className="card-ledger-confirm"><h3>{confirmation.kind === 'deposit' ? '确认记录存入' : '确认开通扣款'}</h3>
      <p>{confirmation.kind === 'deposit' ? '存入' : '扣除'} {formatCardUSD(confirmedAmount)}{confirmation.kind === 'subscription' && <>，关联账号 {selectedAccount?.email || selectedAccount?.label}，套餐「{confirmation.pricing.package.name}」，原价 {confirmation.pricing.package.currency} {(confirmation.pricing.package.original_amount_minor/100).toFixed(2)}。请确认账号已付款开通成功</>}。</p>
      {confirmation.pricing?.charge_currency === 'CNY' && <p>实际扣款 CNY {(confirmation.pricing.charge_amount_minor / 100).toFixed(2)}，按 {formatUTC8(confirmation.pricing.exchange_rate.batch.synced_at)} 的汇率折算并保存。</p>}
      <p className={balanceAfter < 0 ? 'notice' : 'muted'}>记账后预计余额：{formatCardUSD(balanceAfter)}{balanceAfter < 0 && '。历史补录允许负余额，请后续核对并补录存入。'}</p>
      <div className="browser-buttons"><button className="primary small" disabled={busy} onClick={save}>{busy ? '记账中…' : '确认记账'}</button><button className="outline small" disabled={busy} onClick={() => setConfirmation(null)}>返回修改</button></div>
    </div> : composing && <form className="card-ledger-form" onSubmit={prepare}>
      <div className="proxy-fields"><label>记账类型<Select label="银行卡记账类型" value={kind} onChange={setKind} disabled={busy} options={[{ value: 'deposit', label: data?.total === 0 ? '存入（初始余额）' : '存入' }, { value: 'subscription', label: '历史开通扣款补录' }]} /></label>{kind === 'deposit' && <label>存入金额 · USD<input aria-label="记账金额 USD" value={amount} onChange={event => setAmount(event.target.value)} inputMode="decimal" placeholder="0.00" required maxLength={14} disabled={busy} /></label>}</div>
      {kind === 'subscription' && <>
        <label>已开通成功的账号<Select label="扣款关联账号" value={accountID} onChange={setAccountID} disabled={busy} options={[{ value: '', label: '选择账号' }, ...accounts.map(account => ({ value: String(account.id), label: `${account.label} · ${account.email}` }))]} searchPlaceholder="搜索账号名称或邮箱…" /></label>
        <label>开通套餐<Select label="补录套餐" value={packageID} onChange={setPackageID} disabled={busy || packagesLoading} options={[{ value: '', label: packagesLoading ? '正在加载套餐…' : '请选择套餐' }, ...packages.map(item => ({ value: String(item.id), label: `${item.name} · ${item.currency} ${(item.original_amount_minor / 100).toFixed(2)} / ${item.months}个月` }))]} searchPlaceholder="搜索套餐…" /></label>
        {packageError && <p className="error" role="alert">{packageError}</p>}
        <div className="proxy-fields"><label>实际扣款方式<Select label="实际扣款方式" value={chargeMode} onChange={value => { setChargeMode(value); setAmount(''); }} disabled={busy} options={[{ value: 'package', label: '与套餐一致' }, { value: 'CNY', label: '输入人民币金额' }, { value: 'USD', label: '输入美元金额' }]} /></label>
          {chargeMode === 'package' ? <label>实际扣款 · USD<input aria-label="套餐扣款 USD" readOnly value={selectedPackage ? selectedPackage.price_ready === false ? '等待汇率同步' : (selectedPackage.sale_usd_minor / 100).toFixed(2) : ''} placeholder="选择套餐后自动带入" /></label> : <label>实际扣款 · {chargeMode}<input aria-label={`记账金额 ${chargeMode}`} value={amount} onChange={event => setAmount(event.target.value)} inputMode="decimal" placeholder="0.00" required maxLength={14} disabled={busy} /></label>}
          <label>周期开始<input type="date" name="period_start" defaultValue={pending.current?.body.period_start || ''} required disabled={busy}/></label><label>周期结束<input type="date" name="period_end" defaultValue={pending.current?.body.period_end || ''} required disabled={busy}/></label>
        </div>
        <p className="muted">套餐自动带入原币价格；人民币按核对时的当日汇率折算为 USD。此处仅补录已发生的扣款，同一账号同一周期只记录一次，余额不足也可补录为负数。</p>
      </>}
      <label>实际交易号<input name="reference" defaultValue={pending.current?.body.reference || ''} required maxLength={200} disabled={busy}/></label><label>核对依据 / 备注（选填）<input aria-label="记账备注" value={notes} onChange={event => setNotes(event.target.value)} maxLength={1000} placeholder="例如：入金凭据、订单号，可留空" disabled={busy} /></label>
      <div className="browser-buttons"><button className="primary small" disabled={busy || loading || !data || (kind === 'subscription' && (packagesLoading || !!packageError || !packageID))}>{busy ? '正在核对…' : '核对并记账'}</button><button type="button" className="outline small" disabled={busy} onClick={() => setComposing(false)}>收起</button></div>
    </form>}
    {loading ? <p className="muted" role="status">正在加载对账单…</p> : data && <DataTable label="银行卡收支流水" className="card-ledger-table" stickyActions={false} columns={['时间（UTC+8）', '类型', '收入 / 支出 · USD', '交易后余额 · USD', '关联账号', '原币价格', '流水 / 操作者', '交易号', '备注']} empty={!data.entries.length && '暂无流水，首次存入会记为初始余额。'}>
      {data.entries.map(entry => <tr key={entry.id}><td>{formatUTC8(entry.created_at)}</td><td>{kindLabels[entry.kind]}</td><td className={entry.amount_usd_minor > 0 ? 'credit' : 'danger'}>{entry.amount_usd_minor > 0 ? '+' : '−'}{formatCardUSD(Math.abs(entry.amount_usd_minor))}{entry.pricing_snapshot?.charge_currency === 'CNY' && <small className="cell-secondary">CNY {(entry.pricing_snapshot.charge_amount_minor / 100).toFixed(2)}</small>}</td><td>{formatCardUSD(entry.balance_after_usd_minor)}</td><td className="table-text">{entry.account_label || '—'}{entry.account_email && <small className="cell-secondary">{entry.account_email}</small>}</td><td>{entry.original_amount_minor ? `${entry.currency} ${(entry.original_amount_minor/100).toFixed(2)}` : entry.original_php_minor ? `PHP ${(entry.original_php_minor/100).toFixed(2)}` : '—'}{entry.pricing_snapshot && <small className="cell-secondary">{entry.pricing_snapshot.package.name}</small>}</td><td>{entry.id} / {entry.actor_id}</td><td>{entry.external_reference||'—'}</td><td className="table-text">{entry.notes || '—'}</td></tr>)}
    </DataTable>}
    <Pagination page={page} pageSize={pageSize} total={data?.total || 0} onPageChange={setPage} onPageSizeChange={setPageSize} disabled={loading} />
  </Dialog>;
}

import React, { useEffect, useRef, useState } from 'react';
import Dialog from './Dialog';
import { request } from './api';
import { formatCardUSD } from './BankCardLedger';

const tokens = minor => (minor / 100).toLocaleString('zh-CN', { maximumFractionDigits: 2 });

export default function OrderWalletDebit({ order, token, onClose, onComplete }) {
  const [quote, setQuote] = useState(null), [error, setError] = useState(''), [busy, setBusy] = useState(false);
  const writing = useRef(false), requestKey = useRef(crypto.randomUUID());
  useEffect(() => {
    const controller = new AbortController();
    request(`/orders/${order.id}/wallet-quote`, token, { signal: controller.signal })
      .then(value => { if (!controller.signal.aborted) setQuote(value); })
      .catch(e => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [order.id, token]);
  async function submit(event) {
    event.preventDefault();
    if (writing.current || !quote || quote.balance_after_minor < 0) return;
    writing.current = true; setBusy(true); setError('');
    try {
      await request(`/orders/${order.id}`, token, { method: 'POST', body: {
        action: 'wallet_debit', request_key: requestKey.current, version: quote.version,
        expected_user_id: quote.user_id, expected_received_usd_minor: quote.amount_usd_minor,
        expected_tokens_per_usd: quote.tokens_per_usd,
      } });
      onComplete();
    } catch (e) { setError(e.message); }
    finally { writing.current = false; setBusy(false); }
  }
  return <Dialog className="wallet-debit-dialog" title="钱包扣款" onClose={() => { if (!busy) onClose(); }}>
    <form onSubmit={submit}>
      {quote ? <>
        <div className="wallet-debit-account"><strong>{quote.account_email}</strong><span className="muted">扣款用户：{quote.user_email}</span></div>
        <dl className="wallet-debit-summary">
          <div><dt>实收折合 USD</dt><dd>{formatCardUSD(quote.amount_usd_minor)}</dd></div>
          <div><dt>扣除代币</dt><dd>{tokens(quote.tokens_minor)}</dd></div>
          <div><dt>当前余额</dt><dd>{tokens(quote.balance_minor)}</dd></div>
          <div><dt>扣款后余额</dt><dd>{quote.balance_after_minor < 0 ? '余额不足' : tokens(quote.balance_after_minor)}</dd></div>
        </dl>
        <p className="muted">按收款当天保存的汇率结算 · $1 = {quote.tokens_per_usd} 代币</p>
        {quote.balance_after_minor < 0 && <p className="error" role="alert">该用户钱包余额不足，请充值后重新打开。</p>}
      </> : !error && <p role="status">正在读取扣款信息…</p>}
      {error && <p className="error" role="alert">{error}</p>}
      <div className="browser-buttons"><button type="button" className="outline" disabled={busy} onClick={onClose}>取消</button><button className="primary" disabled={busy || !quote || quote.balance_after_minor < 0}>{busy ? '正在扣款…' : '确认扣款'}</button></div>
    </form>
  </Dialog>;
}

import React, { useRef, useState } from 'react';
import Select from './Select';
import Dialog from './Dialog';
import { request } from './api';

const options = [{ value: 'in_use', label: '使用中' }, { value: 'discarded', label: '废弃' }];

export default function AccountUsageStatus({ account, token, onChange, onError }) {
  const status = account.usage_status || 'in_use';
  const [editing, setEditing] = useState(false), [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false), [error, setError] = useState('');
  const writing = useRef(false);
  function editReason() {
    setReason(account.discard_reason || ''); setError(''); setEditing(true);
  }
  async function save(nextStatus, discardReason) {
    if (writing.current) return;
    writing.current = true; setBusy(true); setError('');
    try {
      const data = await request(`/accounts/${account.id}/usage-status`, token, { method: 'PATCH', body: { usage_status: nextStatus, ...(discardReason === undefined ? {} : { discard_reason: discardReason }) } });
      onChange(account.id, data); setEditing(false);
    } catch (error) {
      if (editing) setError(error.message);
      else onError(error.message);
    } finally { writing.current = false; setBusy(false); }
  }
  return <div className="account-usage-status" data-status={status}>
    <Select label={`账号 ${account.email} 的使用状态`} value={status} options={options} disabled={busy} onChange={value => { if (value !== status) { if (value === 'discarded') editReason(); else save(value); } }} />
    {status === 'discarded' && <>
      {account.discard_reason && <p className="account-discard-reason" title={account.discard_reason}>{account.discard_reason}</p>}
      <button type="button" className="text-btn" disabled={busy} onClick={editReason}>{account.discard_reason ? '编辑废弃理由' : '添加废弃理由'}</button>
    </>}
    {editing && <Dialog accountID={account.id} title={status === 'discarded' ? '废弃理由' : '废弃账号'} className="account-status-dialog" onClose={() => { if (!busy) setEditing(false); }}>
      <form onSubmit={event => { event.preventDefault(); save('discarded', reason); }}>
        <p className="muted">{account.label} · {account.email}</p>
        <label>废弃理由（选填）<textarea name="discard_reason" rows={5} maxLength={2000} value={reason} onChange={event => setReason(event.target.value)} placeholder="填写废弃原因，支持多行文本" autoFocus disabled={busy} /></label>
        {error && <p className="error" role="alert">{error}</p>}
        <div className="browser-buttons"><button className="primary" disabled={busy}>{busy ? '保存中…' : status === 'discarded' ? '保存理由' : '确认废弃'}</button><button type="button" className="outline" disabled={busy} onClick={() => setEditing(false)}>取消</button></div>
      </form>
    </Dialog>}
  </div>;
}

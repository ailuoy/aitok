import React, { useState } from 'react';
import Dialog from './Dialog';
import { request } from './api';

export default function AccountNotes({ account, token, onChange }) {
  const [editing, setEditing] = useState(false), [notes, setNotes] = useState('');
  const [busy, setBusy] = useState(false), [error, setError] = useState('');
  async function save(event) {
    event.preventDefault();
    if (busy) return;
    setBusy(true); setError('');
    try {
      const result = await request(`/accounts/${account.id}/notes`, token, { method: 'PATCH', body: { notes } });
      onChange(account.id, { notes: result.notes }); setEditing(false);
    } catch (error) { setError(error.message); }
    finally { setBusy(false); }
  }
  return <div className="account-notes">
    {account.notes && <p className="account-notes-preview">{account.notes}</p>}
    <button className="text-btn" aria-label={`编辑 ${account.email} 的备注`} onClick={() => { setNotes(account.notes || ''); setError(''); setEditing(true); }}>{account.notes ? '编辑备注' : '添加备注'}</button>
    {editing && <Dialog title="账号备注" className="account-notes-dialog" onClose={() => { if (!busy) setEditing(false); }}>
      <form onSubmit={save}><p className="muted">{account.label} · {account.email}</p>
        <label>备注<textarea name="notes" rows={12} maxLength={20000} value={notes} onChange={event => setNotes(event.target.value)} placeholder="记录账号用途、操作记录及其他信息，支持多行文本" autoFocus disabled={busy} /></label>
        <small className="muted">{Array.from(notes).length} / 20000 字，可留空</small>
        {error && <p className="error" role="alert">{error}</p>}
        <div className="browser-buttons"><button className="primary" disabled={busy}>{busy ? '保存中…' : '保存备注'}</button><button type="button" className="outline" disabled={busy} onClick={() => setEditing(false)}>取消</button></div>
      </form>
    </Dialog>}
  </div>;
}

import React, { useRef, useState } from 'react';
import { request } from './api';

export default function AccountRenewal({ account, token, onChange, onError }) {
  const [busy, setBusy] = useState(false);
  const writing = useRef(false);
  async function toggle() {
    if (writing.current) return;
    writing.current = true; setBusy(true);
    try {
      const data = await request(`/accounts/${account.id}/subscription`, token, { method: 'PATCH', body: { renewal_enabled: !account.renewal_enabled } });
      onChange(account.id, { renewal_enabled: data.renewal_enabled });
    } catch (error) { onError(error.message); }
    finally { writing.current = false; setBusy(false); }
  }
  return <button type="button" className="account-renewal-switch" role="switch" aria-checked={Boolean(account.renewal_enabled)} aria-label={`账号 ${account.email} 是否续订`} data-audit-control="subscription" disabled={busy} onClick={toggle} title="记录本系统续订安排与提醒；官网订阅需在官网管理"><span className="switch-track" aria-hidden="true"><span /></span>{busy ? '保存中…' : account.renewal_enabled ? '续订' : '不续订'}</button>;
}

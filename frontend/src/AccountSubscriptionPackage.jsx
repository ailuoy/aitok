import React, { useRef, useState } from 'react';
import Select from './Select';
import { request } from './api';

export default function AccountSubscriptionPackage({ account, packages, loading, token, onChange, onError }) {
  const [busy, setBusy] = useState(false);
  const writing = useRef(false);
  const options = [{ value: '', label: '未设置' }, ...packages.map(pkg => ({ value: String(pkg.id), label: `${pkg.name} · ${pkg.region} · ${pkg.months}个月${pkg.enabled ? '' : '（已下架）'}` }))];
  if (account.subscription_package_id && !packages.some(pkg => pkg.id === account.subscription_package_id)) {
    options.push({ value: String(account.subscription_package_id), label: '原套餐（不可用）', disabled: true });
  }
  async function save(value) {
    if (writing.current || String(account.subscription_package_id ?? '') === value) return;
    writing.current = true; setBusy(true);
    try {
      const result = await request(`/accounts/${account.id}/subscription-package`, token, { method: 'PATCH', body: { subscription_package_id: value ? Number(value) : null } });
      onChange(account.id, result);
    } catch (error) { onError(error.message); }
    finally { writing.current = false; setBusy(false); }
  }
  return <Select label={`账号 ${account.email} 的产品选型`} value={account.subscription_package_id ?? ''} options={options} disabled={loading || busy} onChange={save} searchPlaceholder="搜索套餐名称、地区或周期…" />;
}

import React from 'react';
import Select from './Select';

export default function AccountProxySelect({ account, config, disabled, onChange, label }) {
  return <Select label={label || '账号 ' + account.email + ' 的 SOCKS5'} value={config?.bindings[account.id] || ''} disabled={!config || disabled} onChange={onChange} options={[{ value: '', label: config ? '直连（不使用代理）' : '共享代理未加载' }, ...(config?.proxies || []).map(proxy => ({ value: proxy.id, label: proxy.name + ' · ' + proxy.host }))]} />;
}

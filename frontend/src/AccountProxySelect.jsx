import React from 'react';
import Select from './Select';
import { browserEnvironmentID } from './localBrowser';

export default function AccountProxySelect({ account, userID, config, disabled, onChange, label }) {
  return <Select label={label || '账号 ' + account.email + ' 的 SOCKS5'} value={config?.bindings[browserEnvironmentID(userID, account.id)] || ''} disabled={!config || disabled} onChange={onChange} options={[{ value: '', label: config ? '直连（不使用代理）' : '请先连接本机助手' }, ...(config?.proxies || []).map(proxy => ({ value: proxy.id, label: proxy.name + ' · ' + proxy.host }))]} />;
}

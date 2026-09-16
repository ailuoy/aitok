// SOCKS5 绑定属于当前电脑；对完整账号集合排序后再切页，不向服务端上传代理配置。
export function accountProxyPage(accounts, { config, environmentID, query, group, direction, page, pageSize }) {
  const proxies = new Map((config?.proxies || []).map(proxy => [proxy.id, `${proxy.name} · ${proxy.host}`]));
  const name = account => proxies.get(config?.bindings?.[environmentID(account.id)]) || '';
  const collator = new Intl.Collator('zh-CN', { numeric: true, sensitivity: 'base' });
  const filtered = accounts.filter(account => `${account.label} ${account.email}`.toLowerCase().includes(query.toLowerCase()) && (group === 'all' || String(account.group_id ?? '') === group));
  filtered.sort((a, b) => (direction === 'desc' ? -1 : 1) * collator.compare(name(a), name(b)) || b.id - a.id);
  return { accounts: filtered.slice((page - 1) * pageSize, page * pageSize), total: filtered.length };
}

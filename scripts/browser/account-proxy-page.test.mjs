import test from 'node:test';
import assert from 'node:assert/strict';
import { accountProxyPage } from '../../frontend/src/accountProxyPage.mjs';

test('本机 SOCKS5 对完整筛选结果排序后分页，同代理排序稳定且不修改原数据', () => {
  const accounts = [1, 2, 3, 4, 5].map(id => ({ id, label: `账号${id}`, email: `user${id}@example.com`, group_id: id < 5 ? 1 : null }));
  const options = { config: { proxies: [{ id: 'a', name: '代理2', host: 'a.example' }, { id: 'b', name: '代理10', host: 'b.example' }], bindings: { 'env:1': 'b', 'env:2': 'a', 'env:4': 'a', 'env:5': 'b' } }, environmentID: id => `env:${id}`, query: '', group: 'all', direction: 'asc', pageSize: 2 };
  const pages = [1, 2, 3].map(page => accountProxyPage(accounts, { ...options, page }));
  assert.deepEqual(pages.flatMap(page => page.accounts.map(account => account.id)), [3, 4, 2, 5, 1]);
  assert.ok(pages.every(page => page.total === 5));
  assert.deepEqual(accountProxyPage(accounts, { ...options, direction: 'desc', page: 1 }).accounts.map(account => account.id), [5, 1]);
  const filtered = accountProxyPage(accounts, { ...options, group: '1', query: 'USER4', page: 1 });
  assert.equal(filtered.total, 1);
  assert.equal(filtered.accounts[0].id, 4);
  assert.deepEqual(accounts.map(account => account.id), [1, 2, 3, 4, 5]);
});

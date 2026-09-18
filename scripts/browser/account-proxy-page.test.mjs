import test from 'node:test';
import assert from 'node:assert/strict';
import { accountProxyPage } from '../../frontend/src/accountProxyPage.mjs';
import { matchesRenewalStatus, renewalDays } from '../../frontend/src/accountRenewalStatus.mjs';

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

test('续费筛选覆盖自然日边界、空日期及 SOCKS5 跨页结果', () => {
  const today = '2026-09-19';
  const dates = ['2026-09-30', '2026-09-29', '2026-09-20', '2026-09-19', '2026-09-18', null];
  const accounts = dates.map((renewal_date, i) => ({ id: i + 1, label: '测试账号', email: `user${i + 1}@test.local`, group_id: 1, renewal_date }));
  assert.deepEqual(dates.slice(0, 5).map(date => renewalDays(date, today)), [11, 10, 1, 0, -1]);
  assert.equal(renewalDays('2027-01-01', '2026-12-31'), 1);
  assert.equal(renewalDays('2028-03-01', '2028-02-28'), 2);
  for (const [status, ids] of [['safe', [1]], ['soon', [4, 3, 2]], ['overdue', [5]], ['', [6, 5, 4, 3, 2, 1]]]) {
    const options = { environmentID: String, query: '', group: '1', direction: 'asc', renewalStatus: status, today, pageSize: 1 };
    const pages = ids.map((_, i) => accountProxyPage(accounts, { ...options, page: i + 1 }));
    assert.deepEqual(pages.flatMap(page => page.accounts.map(account => account.id)), ids);
    assert.ok(pages.every(page => page.total === ids.length));
  }
  assert.equal(matchesRenewalStatus(null, 'soon', today), false);
  assert.equal(matchesRenewalStatus('invalid', 'overdue', today), false);
  const filtered = accountProxyPage(accounts, { environmentID: String, query: 'user3', group: '1', direction: 'asc', renewalStatus: 'soon', today, pageSize: 20, page: 1 });
  assert.equal(filtered.total, 1);
  assert.equal(filtered.accounts[0].id, 3);
});

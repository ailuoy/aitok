import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { once, EventEmitter } from 'node:events';
import net from 'node:net';
import { ProxyStore } from './proxy-store.mjs';
import { testProxy, proxyFailure } from './proxy-test.mjs';
import { ProxyError } from './proxy.mjs';
import { parseProxy } from './proxy.mjs';
import { createLauncher } from '../session-browser.mjs';

test('代理加密保存、编辑保留密码、绑定重载及删除约束', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'aitok-proxies-'));
  const store = await new ProxyStore(directory).load();
  const input = { name: '测试代理', host: '203.0.113.10', port: 1080, username: 'user', password: 'test-secret' };
  const { id } = await store.save(null, input);
  assert.ok(!JSON.stringify(store.list()).includes('test-secret'));
  assert.ok(!(await readFile(join(directory, 'proxies.enc'))).includes(Buffer.from('test-secret')));
  await store.save(id, { ...input, name: '已编辑', password: undefined });
  assert.equal(store.get(id).password, 'test-secret');
  await store.bind('account:1', id);
  const reloaded = await new ProxyStore(directory).load();
  assert.equal(reloaded.list().bindings['account:1'], id);
  assert.equal(reloaded.get(id).name, '已编辑');
  await assert.rejects(reloaded.remove(id), /仍有账号使用/);
  await reloaded.bind('account:1', null);
  await reloaded.remove(id);
  assert.equal(reloaded.list().proxies.length, 0);
  await assert.rejects(store.save(null, { ...input, port: 70000 }));
  await assert.rejects(store.save(null, { ...input, host: 'host/path' }));
  await assert.rejects(store.save(null, { ...input, password: '' }));
});

test('代理导入支持冒号凭据格式、标准格式、IPv6 与特殊密码', () => {
  assert.deepEqual(parseProxy('socks5://203.0.113.1:42456:user:pass'), { host: '203.0.113.1', port: 42456, username: 'user', password: 'pass' });
  assert.deepEqual(parseProxy('socks5://user:pass@203.0.113.1:42456'), parseProxy('socks5://203.0.113.1:42456:user:pass'));
  assert.equal(parseProxy('socks5://[::1]:1080:user:p:a@ss%').password, 'p:a@ss%');
  for (const raw of ['socks5://host:70000:u:p', 'socks5://host:1080:u:', 'socks5://host:1080::p']) assert.throws(() => parseProxy(raw));
});

test('出口 IP 比对支持相同、不同、域名解析及失败，测试失败不返回凭据', async () => {
  const raw = 'socks5://secret:password@proxy.example:1080';
  const resolve = async () => [{ address: '203.0.113.1' }, { address: '203.0.113.2' }];
  const same = await testProxy(raw, { resolve, fetchIP: async () => '203.0.113.2' });
  assert.equal(same.ok, true); assert.equal(same.matches, true);
  const different = await testProxy(raw, { resolve, fetchIP: async () => '198.51.100.1' });
  assert.equal(different.ok, true); assert.equal(different.matches, false);
  const failed = await testProxy(raw, { resolve, fetchIP: async () => { throw new Error(raw); } });
  assert.equal(failed.ok, false); assert.ok(!JSON.stringify(failed).includes('password'));
  const ipv6 = await testProxy('socks5://[2001:db8::1]:1080', { fetchIP: async () => '2001:db8:0:0:0:0:0:1' });
  assert.equal(ipv6.matches, true);
});

test('代理失败提示区分认证、目标连接和 TLS，不输出底层凭据', async () => {
  assert.match(proxyFailure(new ProxyError('SOCKS_AUTH_REJECTED', 'authentication')).error, /认证失败/);
  assert.match(proxyFailure(new ProxyError('SOCKS_TIMEOUT', 'authentication')).error, /认证超时/);
  assert.match(proxyFailure(new ProxyError('SOCKS_TARGET_4', 'target')).error, /目标主机不可达/);
  assert.match(proxyFailure(new ProxyError('TLS_TIMEOUT', 'tls')).error, /TLS 握手超时/);
  const result = await testProxy('socks5://user:secret@127.0.0.1:1080', { fetchIP: async () => { throw new ProxyError('TLS_CLOSED', 'tls'); } });
  assert.equal(result.stage, 'tls'); assert.equal(result.error_code, 'TLS_CLOSED');
  assert.ok(!JSON.stringify(result).includes('secret'));
  assert.equal(proxyFailure(new Error('socks5://user:secret@host')).error_code, 'UNKNOWN');
});

test('真实 SOCKS5 认证拒绝可完整传递到页面测试结果', async t => {
  const server = net.createServer(socket => {
    let stage = 0;
    socket.on('error', () => {});
    socket.on('data', () => {
      if (stage++ === 0) socket.write(Buffer.from([5, 2]));
      else socket.end(Buffer.from([1, 1]));
    });
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  t.after(() => server.close());
  const result = await testProxy(`socks5://test:secret@127.0.0.1:${server.address().port}`);
  assert.equal(result.ok, false);
  assert.equal(result.stage, 'authentication');
  assert.equal(result.error_code, 'SOCKS_AUTH_REJECTED');
  assert.match(result.error, /代理拒绝了用户名或密码/);
});

test('无密钥启动器管理代理并把账号绑定的认证代理交给浏览器', async t => {
  const store = await new ProxyStore(await mkdtemp(join(tmpdir(), 'aitok-launcher-'))).load();
  const launches = [], probes = [];
  const browser = Object.assign(new EventEmitter(), { start: async input => { launches.push(input); return { state: 'opened' }; }, status: () => ({ state: 'closed' }) });
  const server = createLauncher({ origin: 'http://localhost:15680', store, browser, probeProxy: async raw => { probes.push(raw); return raw.includes('failed.example') ? { ok: false, error: '代理测试失败' } : { ok: true, matches: true, exit_ip: '203.0.113.1' }; } });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  t.after(() => { server.closeAllConnections(); server.close(); });
  const base = `http://127.0.0.1:${server.address().port}`;
  const headers = { Origin: 'http://localhost:15680', 'X-AiTok-Client': 'browser', 'Content-Type': 'application/json' };
  const call = (path, method = 'GET', body) => fetch(base + path, { headers, method, ...(body ? { body: JSON.stringify(body) } : {}) });
  assert.equal((await (await call('/health')).json()).version, 2);
  const input = { name: '美国', host: '203.0.113.1', port: 1080, username: 'u', password: 'p' };
  assert.equal((await call('/proxies', 'POST', input)).status, 400);
  const approval = await (await call('/proxies/test', 'POST', input)).json();
  assert.equal(approval.result.ok, true);
  assert.equal((await call('/proxies', 'POST', { ...input, password: 'changed', test_token: approval.test_token })).status, 400);
  const added = await (await call('/proxies', 'POST', { ...input, test_token: approval.test_token })).json();
  assert.equal((await call('/proxies', 'POST', { ...input, test_token: approval.test_token })).status, 400);
  const id = added.proxies[0].id;
  assert.equal(added.proxies[0].password, undefined);
  assert.equal((await (await call('/proxies/' + id)).json()).password, 'p');
  assert.equal((await call('/proxies/' + id, 'PATCH', input)).status, 400);
  const editApproval = await (await call('/proxies/test', 'POST', { ...input, id, name: '已编辑' })).json();
  assert.equal((await call('/proxies/' + id, 'PATCH', { ...input, name: '已编辑', test_token: editApproval.test_token })).status, 200);
  await call('/browsers/account%3A1/proxy', 'PATCH', { proxy_id: id });
  await call('/browsers', 'POST', { environment_id: 'account:1', session: { accessToken: 'test' }, proxy_url: 'socks5://untrusted:1080' });
  assert.equal(launches[0].proxy_url, 'socks5://u:p@203.0.113.1:1080');
  const tested = await (await call('/proxies/' + id + '/test', 'POST', {})).json();
  assert.equal(probes.length, 3); assert.equal(tested.proxies[0].last_test.matches, true);
  assert.equal((await call('/proxies/' + id, 'DELETE')).status, 400);
  await call('/browsers/account%3A1/proxy', 'PATCH', { proxy_id: null });
  browser.emit('activity', { environment_id: 'account:1', action: 'login', ok: true, logged_in_at: '2026-09-15T00:30:00Z' });
  browser.emit('activity', { environment_id: 'account:1', action: 'close', ok: true });
  assert.equal((await call('/proxies/' + id, 'DELETE')).status, 200);
  assert.equal((await (await call('/proxies')).json()).proxies.length, 0);
  const history = await (await call('/proxy-history?proxy_id=' + id)).json();
  assert.ok(history.records.some(record => record.action === 'open' && record.ok));
  assert.ok(history.records.some(record => record.action === 'test'));
  assert.equal(history.records.filter(record => record.action === 'close').length, 1);
  assert.ok(history.records.some(record => record.action === 'login'));
  const closed = await (await call('/browsers/account%3A1')).json();
  assert.equal(closed.state, 'closed');
  assert.equal(closed.authenticated_at, '2026-09-15T00:30:00Z');
  assert.ok(!history.records.some(record => 'password' in record || 'session' in record));
  const reloaded = await new ProxyStore(store.directory).load();
  assert.equal(reloaded.history(id).total, history.total);
  assert.equal((await call('/proxy-history?page=0')).status, 400);
  assert.equal((await call('/proxy-history?page_size=101')).status, 400);
  assert.equal((await (await call('/proxy-history?page_size=50')).json()).page_size, 50);
  const failedInput = { ...input, host: 'failed.example' };
  const failed = await (await call('/proxies/test', 'POST', failedInput)).json();
  assert.equal(failed.result.ok, false);
  assert.equal(failed.test_token, null);
  assert.equal((await call('/proxies', 'POST', { ...failedInput, test_token: failed.test_token })).status, 400);
  assert.equal((await fetch(base + '/proxies', { headers: { ...headers, Origin: 'https://evil.test' } })).status, 403);
});

test('代理使用历史分页、并发写入和代理筛选不丢记录', async () => {
  const store = await new ProxyStore(await mkdtemp(join(tmpdir(), 'aitok-usage-'))).load();
  const proxy = { id: 'proxy-a', name: '代理 A', host: '203.0.113.1', port: 1080, password: 'never-log' };
  await Promise.all(Array.from({ length: 25 }, (_, index) => store.recordUsage(proxy, { action: index % 2 ? 'open' : 'close', ok: true, email: 'account@example.com', session: 'never-log' })));
  await store.recordUsage({ ...proxy, id: 'proxy-b' }, { action: 'get_ip', ok: false });
  assert.equal(store.history('proxy-a').records.length, 20);
  assert.equal(store.history('proxy-a', 2).records.length, 5);
  assert.equal(store.history('proxy-a', 1, 50).records.length, 25);
  assert.equal(store.history('proxy-a', 2, 50).records.length, 0);
  assert.equal(store.history('proxy-b').records.length, 1);
  assert.equal(store.history('').total, 26);
  assert.ok(!JSON.stringify(store.history('')).includes('never-log'));
  await store.recordLogin('direct-account', '2026-09-15T01:00:00Z');
  await store.recordLogin('direct-account', '2026-09-15T00:00:00Z');
  const reloaded = await new ProxyStore(store.directory).load();
  assert.equal(reloaded.data.logins['direct-account'], '2026-09-15T01:00:00Z');
});

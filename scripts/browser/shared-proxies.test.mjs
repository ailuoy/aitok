import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { once, EventEmitter } from 'node:events';
import { ProxyStore } from './proxy-store.mjs';
import { createLauncher } from './launcher-server.mjs';

test('不同电脑及登录用户使用相同共享代理，旧本机绑定不会覆盖共享选择', async t => {
  const proxy = { id: '23', name: '共享代理', host: '203.0.113.23', port: 1080, username: 'shared', password: 'shared-secret' };
  for (const user of [1, 2]) {
    const store = await new ProxyStore(await mkdtemp(join(tmpdir(), 'aitok-shared-proxy-'))).load();
    const environment = `https://aitok.test:user:${user}:account:7`;
    const old = await store.save(null, { name: '旧代理', host: '203.0.113.99', port: 1080, username: '', password: '' });
    await store.bind(environment, old.id);
    const starts = [], probes = [];
    const browser = new EventEmitter();
    browser.start = async input => { starts.push(input); return { state: 'opened' }; };
    browser.startBlank = browser.start;
    const server = createLauncher({ origin: 'https://aitok.test', browser, store, enforceOriginScope: true, authorize: async () => ({ id: user }), probeProxy: async url => { probes.push(url); return { ok: true, exit_ip: '203.0.113.23', proxy_ips: ['203.0.113.23'], matches: true }; } });
    server.listen(0, '127.0.0.1'); await once(server, 'listening');
    t.after(() => { server.closeAllConnections(); server.close(); });
    const call = (path, body) => fetch(`http://127.0.0.1:${server.address().port}${path}`, { method: body ? 'POST' : 'GET', headers: { Origin: 'https://aitok.test', 'X-AiTok-Client': 'browser', 'Content-Type': 'application/json' }, ...(body ? { body: JSON.stringify(body) } : {}) });
    assert.equal((await (await call('/health')).json()).shared_proxies, true);
    assert.equal((await call('/browsers', { environment_id: environment, shared_proxy: proxy })).status, 200);
    assert.equal(starts[0].proxy_url, 'socks5://shared:shared-secret@203.0.113.23:1080');
    assert.ok(!Object.hasOwn(starts[0], 'shared_proxy'));
    browser.emit('activity', { environment_id: environment, action: 'close', ok: true });
    assert.equal((await call('/browsers', { environment_id: environment, shared_proxy: null })).status, 200);
    assert.equal(starts[1].proxy_url, '', '明确直连必须覆盖本机旧绑定');
    assert.equal((await call('/browsers', { environment_id: environment, shared_proxy: {} })).status, 400);
    assert.equal(starts.length, 2, '无效配置不得回退直连');
    assert.equal((await call('/blank-browsers', { shared_proxy: proxy })).status, 200);
    assert.equal(starts[2].proxy_url, starts[0].proxy_url);
    assert.equal((await call('/shared-proxies/test', { proxy })).status, 200);
    assert.deepEqual(probes, [starts[0].proxy_url]);
    await store.queue;
    assert.ok(store.history('23').records.some(record => record.action === 'open'));
    assert.ok(!JSON.stringify(store.history('23')).includes('shared-secret'));
    assert.equal(store.list().proxies.length, 1, '共享凭据不落入本机旧代理配置');
  }
});

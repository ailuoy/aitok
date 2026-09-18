import test from 'node:test';
import assert from 'node:assert/strict';
import { EventEmitter, once } from 'node:events';
import { mkdtemp } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import net from 'node:net';
import { startLauncher } from './launcher-runtime.mjs';
import { defaultLauncherPort, normalizeOrigin, validateLauncherPort } from '../../shared/local-launcher.mjs';

async function freePort() {
  const server = net.createServer(); server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const port = server.address().port;
  await new Promise(resolve => server.close(resolve));
  return port;
}

test('端口默认按环境区分，非法来源和端口不能进入启动器', () => {
  assert.equal(defaultLauncherPort('https://toktopup.com'), 15683);
  for (const origin of ['http://localhost:15680', 'http://127.0.0.1:5173', 'http://[::1]:5173']) assert.equal(defaultLauncherPort(origin), 15684);
  assert.equal(normalizeOrigin('https://toktopup.com/'), 'https://toktopup.com');
  for (const value of ['file:///tmp/test', 'https://user:pass@toktopup.com', 'https://toktopup.com/a', 'https://toktopup.com/?a=1']) assert.throws(() => normalizeOrigin(value));
  for (const port of [0, 1, 65536, '1234.5', 'NaN', '12;test']) assert.throws(() => validateLauncherPort(port));
});

test('双环境隔离代理、账号和 CORS，端口冲突不会结束已有助手', async t => {
  const directory = await mkdtemp(join(tmpdir(), 'aitok-launcher-test-'));
  const calls = [], closed = [];
  const factory = async ({ origin }) => Object.assign(new EventEmitter(), {
    environments: new Map(),
    start: async input => { calls.push({ origin, input }); return { state: 'opened' }; },
    status: () => ({ state: 'closed' }), stop: async () => ({ state: 'closing' }), close: () => closed.push(origin),
  });
  const prod = await startLauncher({ directory, origin: 'https://toktopup.com', port: await freePort(), browserFactory: factory });
  const dev = await startLauncher({ directory, origin: 'http://localhost:15680', port: await freePort(), browserFactory: factory });
  t.after(async () => { await prod.close(); await dev.close(); });
  // 同一端口会在测试中重启，每次建立新连接，避免复用已关闭服务的连接池套接字。
  const call = (runtime, path, method = 'GET', body, origin = runtime.origin) => fetch(`http://127.0.0.1:${runtime.port}${path}`, {
    method, headers: { Origin: origin, 'X-AiTok-Client': 'browser', 'Content-Type': 'application/json', Connection: 'close' }, body: body && JSON.stringify(body),
  });
  const config = { name: '仅在线上', host: '203.0.113.1', port: 1080, username: '', password: '' };
  await prod.store.save(null, prod.store.prepare(null, config), { ok: true });
  assert.equal((await (await call(prod, '/proxies')).json()).proxies.length, 1);
  assert.equal((await (await call(dev, '/proxies')).json()).proxies.length, 0);
  assert.equal((await call(prod, '/proxies', 'GET', null, dev.origin)).status, 403);
  assert.equal((await call(prod, '/browsers', 'POST', { environment_id: dev.origin + ':user:1:account:1', session: {} })).status, 403);
  assert.equal((await call(dev, '/browsers/' + encodeURIComponent(prod.origin + ':user:1:account:1'))).status, 403);
  assert.equal((await call(dev, '/browsers', 'POST', { environment_id: dev.origin + ':user:1:account:1', session: {} })).status, 200);
  assert.equal(calls[0].input.assistant_endpoint, dev.origin + '/api/browser-assistant');
  await assert.rejects(startLauncher({ directory, origin: dev.origin, port: prod.port, browserFactory: factory }), /已被占用/);
  assert.equal((await call(prod, '/health')).status, 200);
  await dev.close();
  assert.equal((await call(prod, '/health')).status, 200);
  assert.ok(!closed.includes(prod.origin));
  // 重启沿用同一站点的加密代理配置。
  await prod.close();
  const reopened = await startLauncher({ directory, origin: prod.origin, port: prod.port, browserFactory: factory });
  t.after(() => reopened.close());
  assert.equal((await (await call(reopened, '/proxies')).json()).proxies[0].name, '仅在线上');
});

test('桌面授权失效后拒绝请求，账号环境必须属于当前登录用户', async t => {
  const directory = await mkdtemp(join(tmpdir(), 'aitok-authorized-launcher-'));
  let loggedIn = false, opened = 0;
  const runtime = await startLauncher({ directory, origin: 'http://localhost:15680', port: await freePort(),
    authorize: async () => { if (!loggedIn) throw new Error('请先授权登录'); return { id: 7 }; },
    browserFactory: async () => Object.assign(new EventEmitter(), { environments: new Map(), start: async () => { opened++; return { state: 'opened' }; }, close: async () => {} }),
  });
  t.after(() => runtime.close());
  const call = (path, body) => fetch(`http://127.0.0.1:${runtime.port}${path}`, { method: body ? 'POST' : 'GET', headers: { Origin: runtime.origin, 'X-AiTok-Client': 'browser', 'Content-Type': 'application/json' }, body: body && JSON.stringify(body) });
  assert.equal((await call('/health')).status, 200);
  assert.notEqual((await call('/proxies')).status, 200);
  loggedIn = true;
  assert.equal((await call('/browsers', { environment_id: runtime.origin + ':user:8:account:1', session: {} })).status, 403);
  assert.equal(opened, 0);
  assert.equal((await call('/browsers', { environment_id: runtime.origin + ':user:7:account:1', session: {} })).status, 200);
  assert.equal(opened, 1);
  loggedIn = false;
  assert.notEqual((await call('/browsers', { environment_id: runtime.origin + ':user:7:account:1', session: {} })).status, 200);
  assert.equal(opened, 1);
});

test('空白浏览器只使用已保存代理，隔离用户资料且不接受外部 Session', async t => {
  const directory = await mkdtemp(join(tmpdir(), 'aitok-blank-launcher-'));
  const calls = [];
  let signedIn = true;
  const runtime = await startLauncher({ directory, origin: 'http://localhost:15680', port: await freePort(),
    authorize: async () => { if (!signedIn) throw new Error('请先授权登录'); return { id: 7 }; },
    browserFactory: async () => Object.assign(new EventEmitter(), {
      environments: new Map(), startBlank: async input => { calls.push(input); return { state: 'opened' }; },
      status: () => ({ state: 'opened' }), close: async () => {},
    }),
  });
  t.after(() => runtime.close());
  const call = (path, body, origin = runtime.origin) => fetch(`http://127.0.0.1:${runtime.port}${path}`, {
    method: body ? 'POST' : 'GET', headers: { Origin: origin, 'X-AiTok-Client': 'browser', 'Content-Type': 'application/json' }, body: body && JSON.stringify(body),
  });
  const proxy = await runtime.store.save(null, runtime.store.prepare(null, { name: '空白窗口代理', host: '203.0.113.4', port: 1080, username: 'test', password: 'private' }), { ok: true });
  assert.equal((await (await call('/health')).json()).blank_browser, true);
  assert.equal((await call('/blank-browsers', {})).status, 400);
  assert.equal((await call('/blank-browsers', { proxy_id: 'missing' })).status, 400);
  assert.equal((await call('/blank-browsers', { proxy_id: proxy.id }, 'https://evil.invalid')).status, 403);
  signedIn = false;
  assert.equal((await call('/blank-browsers', { proxy_id: proxy.id })).status, 400);
  assert.equal(calls.length, 0);
  signedIn = true;
  const response = await call('/blank-browsers', { proxy_id: proxy.id, environment_id: 'other-user', session: { accessToken: 'injected' } });
  assert.equal(response.status, 200);
  const body = await response.json();
  assert.ok(body.environment_id.startsWith(runtime.origin + ':user:7:blank:'));
  assert.ok(!JSON.stringify(body).includes('private'));
  assert.deepEqual(Object.keys(calls[0]).sort(), ['environment_id', 'proxy_url']);
  assert.equal(calls[0].proxy_url, 'socks5://test:private@203.0.113.4:1080');
  await call('/blank-browsers', { proxy_id: proxy.id });
  assert.notEqual(calls[0].environment_id, calls[1].environment_id);
  assert.equal((await call('/browsers/' + encodeURIComponent(body.environment_id.replace(':user:7:', ':user:8:')))).status, 403);
});

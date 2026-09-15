import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import { once } from 'node:events';
import { mkdtemp } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { checkBrowserIP, IPIFY_URL, BILLING_URL, CLEANIP_URL } from './browser-ip.mjs';
import { SessionBrowser, findChrome } from './session.mjs';

test('浏览器出口 IP 比对覆盖一致、不一致、IPv6、直连和非法响应', async () => {
  const calls = [];
  let body = '203.0.113.1';
  const cdp = { send: async (method, params, sessionID) => {
    calls.push({ method, params, sessionID });
    return method === 'Page.navigate' ? {} : { result: { value: body } };
  } };
  assert.equal((await checkBrowserIP(cdp, 'page', 'socks5://203.0.113.1:1080')).matches, true);
  assert.equal(calls.find(call => call.method === 'Page.navigate').params.url, IPIFY_URL);
  assert.ok(calls.every(call => call.sessionID === 'page'));
  body = '198.51.100.1';
  assert.equal((await checkBrowserIP(cdp, 'page', 'socks5://203.0.113.1:1080')).matches, false);
  body = '2001:db8:0:0:0:0:0:1';
  assert.equal((await checkBrowserIP(cdp, 'page', 'socks5://[2001:db8::1]:1080')).matches, true);
  assert.equal((await checkBrowserIP(cdp, 'page', '')).matches, null);
  body = '<h1>Bad Gateway</h1>';
  assert.equal((await checkBrowserIP(cdp, 'page', 'socks5://203.0.113.1:1080')).ok, false);
  assert.equal((await checkBrowserIP(cdp, 'page', '')).ok, false);
  assert.equal((await checkBrowserIP({ send: async () => ({ errorText: 'net::ERR_PROXY_CONNECTION_FAILED' }) }, 'page', '')).ok, false);
});

test('IP 检查完成且一致才新开 Billing 标签，失败或关闭中不打开', async () => {
  for (const outcome of ['match', 'mismatch', 'failed', 'closing']) {
    let finish;
    const calls = [];
    const browser = new SessionBrowser({ chrome: '', directory: '', checkIP: () => new Promise(resolve => { finish = resolve; }) });
    browser.verifyLogin = async () => {};
    const environment = { state: 'checking_ip', session: {}, hasLoginCookie: true, cdp: { send: async (method, params) => { calls.push({ method, params }); return {}; } } };
    const opening = browser.openBilling(environment, 'page', 'socks5://203.0.113.1:1080');
    assert.deepEqual(calls, [{ method: 'Target.createTarget', params: { url: CLEANIP_URL } }]);
    if (outcome === 'closing') environment.state = 'closing';
    finish({ ok: outcome !== 'failed', matches: outcome !== 'mismatch', exit_ip: '203.0.113.1', proxy_ips: ['203.0.113.1'], error: '测试连接失败' });
    await opening;
    clearInterval(environment.verifyTimer);
    assert.deepEqual(calls, [{ method: 'Target.createTarget', params: { url: CLEANIP_URL } }, ...(outcome === 'match' ? [{ method: 'Target.createTarget', params: { url: BILLING_URL } }] : [])]);
    assert.equal(environment.state, outcome === 'match' ? 'opened' : outcome === 'closing' ? 'closing' : 'ip_check_failed');
  }
});

test('Chromium 先展示 IP 页面，成功后保留原标签并新开账单页；失败保留窗口', { skip: !process.env.AITOK_BROWSER_SMOKE, timeout: 30000 }, async t => {
  const requests = [];
  const server = http.createServer((request, response) => {
    requests.push(request.url);
    response.setHeader('Content-Type', 'text/plain');
    response.end(request.url === '/ip' ? '203.0.113.1' : request.url === '/bad' ? 'Bad Gateway' : 'Billing fixture');
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  t.after(() => { server.closeAllConnections(); server.close(); });
  const base = `http://127.0.0.1:${server.address().port}`;
  for (const path of ['/ip', '/bad']) {
    const browser = new SessionBrowser({ chrome: await findChrome(), directory: await mkdtemp(join(tmpdir(), 'aitok-ip-smoke-')), headless: true, startURL: base + path, billingURL: base + '/billing', cleanipURL: base + '/cleanip' });
    t.after(() => browser.close());
    const initial = await browser.start({ environment_id: 'ip-flow', session: { accessToken: 'test-only', sessionToken: 'test-cookie' } });
    assert.equal(initial.state, 'checking_ip');
    const environment = browser.environments.get('ip-flow');
    await environment.opening;
    const { targetInfos } = await environment.cdp.send('Target.getTargets');
    assert.equal(targetInfos.filter(target => target.type === 'page').length, path === '/ip' ? 3 : 2);
    assert.ok(!targetInfos.some(target => target.type === 'page' && target.url === 'about:blank'));
    assert.ok(targetInfos.some(target => target.url === base + path));
    assert.ok(targetInfos.some(target => target.url === base + '/cleanip'));
    assert.equal(targetInfos.some(target => target.url === base + '/billing'), path === '/ip');
    assert.equal(browser.status('ip-flow').state, path === '/ip' ? 'opened' : 'ip_check_failed');
    assert.equal(environment.child.exitCode, null);
    await browser.stop('ip-flow');
  }
  assert.ok(requests.indexOf('/ip') < requests.indexOf('/billing'));
});

import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import { once } from 'node:events';
import { mkdtemp } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { SessionRefresh } from './session-refresh.mjs';
import { SessionBrowser, validateSession, findChrome } from './session.mjs';

const fresh = { accessToken: 'fresh-test-token', user: { email: 'test@example.com' }, expires: '2099-01-01T00:00:00Z' };
function environment(email = fresh.user.email) {
  return { expectedEmail: 'test@example.com', controller: new AbortController(), cdp: { async send(method) {
    if (method === 'Target.getTargets') return { targetInfos: [{ type: 'page', targetId: 'page', url: 'https://chatgpt.com/' }] };
    if (method === 'Target.attachToTarget') return { sessionId: 'session' };
    if (method === 'Page.getFrameTree') return { frameTree: { frame: { id: 'frame' } } };
    if (method === 'Page.createIsolatedWorld') return { executionContextId: 1 };
    if (method === 'Runtime.evaluate') return { result: { value: { session: { ...fresh, user: { email } } } } };
    if (method === 'Network.getCookies') return { cookies: [{ name: '__Secure-next-auth.session-token', value: 'cookie-test', domain: '.chatgpt.com' }] };
    return {};
  } } };
}

test('自动更新只在后台保存成功后关闭，返回时间且不泄露凭据，并防止重复提交', async t => {
  let writes = 0, closed = 0;
  const updatedAt = new Date().toISOString();
  const server = http.createServer(async (request, response) => {
    assert.equal(request.url, '/api/browser-assistant/session');
    assert.equal(request.headers.authorization, 'Bearer refresh-test');
    let body = ''; for await (const chunk of request) body += chunk;
    const session = JSON.parse(JSON.parse(body).session_json);
    assert.equal(session.accessToken, fresh.accessToken);
    assert.equal(session.cookies[0].value, 'cookie-test');
    writes++;
    response.end(JSON.stringify({ session_updated_at: updatedAt }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  t.after(() => { server.closeAllConnections(); server.close(); });
  const job = new SessionRefresh(environment(), `http://127.0.0.1:${server.address().port}/api/browser-assistant`, 'refresh-test', async () => { assert.equal(writes, 1); closed++; });
  t.after(() => job.close());
  await Promise.all([job.run(), job.run()]);
  await job.run();
  assert.equal(writes, 1); assert.equal(closed, 1);
  assert.equal(job.status.state, 'saved'); assert.equal(job.status.session_updated_at, updatedAt);
  assert.equal(job.token, null);
  assert.ok(!JSON.stringify(job.status).includes('cookie-test'));
  job.close(); assert.equal(job.status.state, 'saved');
});

test('邮箱不匹配、保存失败、取消、超时均不关闭窗口或误报更新成功', async t => {
  let writes = 0;
  const server = http.createServer((request, response) => { writes++; response.writeHead(401); response.end(JSON.stringify({ error: '授权已过期' })); });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  t.after(() => { server.closeAllConnections(); server.close(); });
  for (const scenario of ['wrong-email', 'save-failure', 'cancel', 'timeout']) {
    const job = new SessionRefresh(environment(scenario === 'wrong-email' ? 'other@example.com' : fresh.user.email), `http://127.0.0.1:${server.address().port}`, 'refresh-test', () => assert.fail('失败不能关闭浏览器'));
    t.after(() => job.close());
    if (scenario === 'cancel') job.close();
    if (scenario === 'timeout') job.fail('等待登录超时');
    await job.run();
    assert.equal(job.status.state, scenario === 'cancel' ? 'cancelled' : 'failed');
    assert.equal(job.status.session_updated_at, undefined);
    assert.equal(job.token, null);
  }
  assert.equal(writes, 1);
});

test('仅更新模式允许过期 Session，登录验证未通过时不启动采集', async () => {
  const expired = { ...fresh, expires: '2020-01-01T00:00:00Z' };
  assert.throws(() => validateSession(expired), /过期/);
  assert.equal(validateSession(expired, { allowExpired: true }).accessToken, fresh.accessToken);
  assert.throws(() => validateSession({ accessToken: 'bad\nvalue' }, { allowExpired: true }));
  const browser = new SessionBrowser({ chrome: 'unused', directory: 'unused' });
  let saves = 0, value = { status: 200, email: 'other@example.com' };
  const env = { session: fresh, expectedEmail: fresh.user.email, pages: new Map(), refresh: { run: async () => saves++ }, cdp: { async send(method) {
    if (method === 'Target.getTargets') return { targetInfos: [{ type: 'page', targetId: 'page', url: 'https://chatgpt.com/' }] };
    if (method === 'Target.attachToTarget') return { sessionId: 'page' };
    return { result: { value } };
  } } };
  for (const state of [{ status: 200, email: 'other@example.com' }, { status: 401 }, { challenge: true }]) {
    value = state; await browser.verifyLogin(env); assert.equal(saves, 0);
  }
  value = { status: 200, email: fresh.user.email };
  await browser.verifyLogin(env); assert.equal(saves, 1);
});

test('真实 Chromium 自动更新不加载小助手，保存成功后关闭并保留结果', { skip: !process.env.AITOK_BROWSER_SMOKE, timeout: 30000 }, async t => {
  const updatedAt = new Date().toISOString();
  let writes = 0;
  const server = http.createServer(async (request, response) => {
    let raw = ''; for await (const chunk of request) raw += chunk;
    const session = JSON.parse(JSON.parse(raw).session_json);
    assert.equal(session.cookies[0].value, 'restored-test-cookie');
    writes++;
    response.end(JSON.stringify({ session_updated_at: updatedAt }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  t.after(() => { server.closeAllConnections(); server.close(); });
  const browser = new SessionBrowser({ chrome: await findChrome(), directory: await mkdtemp(join(tmpdir(), 'aitok-refresh-smoke-')), headless: true, startURL: 'about:blank', billingURL: 'about:blank', cleanipURL: '', checkIP: async () => ({ ok: true, exit_ip: '127.0.0.1' }) });
  t.after(() => browser.close());
  await browser.start({ environment_id: 'refresh-test', session: { ...fresh, expires: '2020-01-01T00:00:00Z', sessionToken: 'restored-test-cookie' }, expected_email: fresh.user.email, assistant_token: 'refresh-test', assistant_endpoint: `http://127.0.0.1:${server.address().port}`, refresh_session: true });
  const env = browser.environments.get('refresh-test');
  await env.opening;
  assert.equal(env.assistant, undefined, '自动更新不能创建助手面板或填充功能');
  // 所有页面保持 about:blank，仅模拟官网响应，不访问真实 ChatGPT 或用户账号。
  const send = env.cdp.send.bind(env.cdp);
  env.cdp.send = async (method, params, sessionID) => {
    if (method === 'Target.getTargets') {
      const result = await send(method, params, sessionID);
      return { targetInfos: result.targetInfos.map(target => ({ ...target, url: 'https://chatgpt.com/' })) };
    }
    if (method === 'Runtime.evaluate' && params.expression.includes('async function readSession')) return { result: { value: { session: fresh } } };
    if (method === 'Runtime.evaluate' && params.awaitPromise && !params.contextId) return { result: { value: { status: 200, email: fresh.user.email } } };
    return send(method, params, sessionID);
  };
  while (env.verifying) await delay(20);
  await browser.verifyLogin(env);
  for (let i = 0; i < 100 && browser.status('refresh-test').state !== 'closed'; i++) await delay(50);
  assert.equal(writes, 1);
  const status = browser.status('refresh-test');
  assert.equal(status.state, 'closed');
  assert.equal(status.session_update.state, 'saved');
  assert.equal(status.session_update.session_updated_at, updatedAt);
});

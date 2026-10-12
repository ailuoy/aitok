import test from 'node:test';
import assert from 'node:assert/strict';
import { runInNewContext } from 'node:vm';
import { loginCheckSource } from './page-verification.mjs';
import { spawn } from 'node:child_process';
import { mkdtemp } from 'node:fs/promises';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { setTimeout as delay } from 'node:timers/promises';
import { CDP } from './cdp.mjs';
import { findChrome } from './session.mjs';
import { captureBrowserSession } from './session-capture.mjs';

function page() {
  let requests = 0;
  const context = {
    location: { origin: 'https://chatgpt.com' },
    document: { readyState: 'complete', querySelector: () => null, querySelectorAll: () => [] },
    getComputedStyle: frame => frame.style,
    AbortSignal, atob,
    fetch: async () => {
      requests++;
      return new Response(JSON.stringify({ user: { email: 'test@example.com' }, account: { planType: 'plus' } }));
    },
  };
  return { context, check: () => runInNewContext(loginCheckSource(), context), requests: () => requests };
}

test('页面加载或显示验证表单时不请求登录接口，验证结束后恢复真实身份检查', async () => {
  const { context, check, requests } = page();
  context.document.readyState = 'loading';
  assert.equal(await check(), null);
  context.document.readyState = 'complete';
  context.document.querySelector = () => ({});
  assert.equal((await check()).challenge, true);
  assert.equal(requests(), 0);
  context.document.querySelector = () => null;
  const result = await check();
  assert.equal(result.email, 'test@example.com');
  assert.equal(result.plan, 'plus');
  assert.equal(requests(), 1);
});

test('可见 Cloudflare 验证框暂停登录请求，隐藏框不阻止正常登录检查', async () => {
  const { context, check, requests } = page();
  const frame = { getBoundingClientRect: () => ({ width: 300, height: 65 }), style: { visibility: 'visible', display: 'block' } };
  context.document.querySelectorAll = () => [frame];
  assert.equal((await check()).challenge, true);
  assert.equal(requests(), 0);
  frame.style.display = 'none';
  assert.equal((await check()).status, 200);
  frame.style.display = 'block';
  frame.getBoundingClientRect = () => ({ width: 0, height: 0 });
  assert.equal((await check()).status, 200);
});

test('页面资源尚未加载完成时即可检查 Session，仍暂停可见验证页的请求', async () => {
  const { context, check, requests } = page();
  context.document.readyState = 'interactive';
  assert.equal((await check()).email, 'test@example.com');
  assert.equal(requests(), 1);
  context.document.querySelector = () => ({});
  assert.equal((await check()).challenge, true);
  assert.equal(requests(), 1);
  context.location.origin = 'https://example.com';
  assert.equal(await check(), null);
  assert.equal(requests(), 1);
});

test('登录接口的 Cloudflare 挑战响应不会误报 Cookie 失效或登录成功', async () => {
  const { context, check } = page();
  context.fetch = async () => new Response('<html>verification</html>', { status: 403, headers: { 'cf-mitigated': 'challenge' } });
  const result = await check();
  assert.equal(result.challenge, true);
  assert.equal(result.status, 403);
  assert.equal(result.email, undefined);
});

test('真实 Chromium 图片请求挂起时仍能确认登录并提取 Session', { skip: !process.env.AITOK_BROWSER_SMOKE, timeout: 20000 }, async t => {
  const directory = await mkdtemp(join(tmpdir(), 'aitok-slow-page-'));
  const child = spawn(await findChrome(), [`--user-data-dir=${directory}`, '--headless=new', '--remote-debugging-pipe', '--no-first-run', '--disable-background-networking', 'about:blank'], { stdio: ['ignore', 'ignore', 'ignore', 'pipe', 'pipe'] });
  t.after(() => child.kill());
  const cdp = new CDP(child);
  const { targetInfos } = await cdp.send('Target.getTargets');
  const { sessionId } = await cdp.send('Target.attachToTarget', { targetId: targetInfos.find(target => target.type === 'page').targetId, flatten: true });
  await cdp.send('Page.enable', {}, sessionId);
  await cdp.send('Fetch.enable', { patterns: [{ urlPattern: '*' }] }, sessionId);
  let slowResource = false, sessionRequests = 0;
  const errors = [];
  // 拦截全部页面请求，仅返回本地夹具；图片保持挂起，不访问真实账号。
  cdp.on('message', message => {
    if (message.method !== 'Fetch.requestPaused') return;
    const url = new URL(message.params.request.url);
    if (url.pathname === '/slow.png') { slowResource = true; return; }
    const session = url.pathname === '/api/auth/session';
    if (session) sessionRequests++;
    const body = session ? JSON.stringify({ accessToken: 'fixture-token', user: { email: 'test@example.com' } }) : '<html><body>ChatGPT fixture<img src="/slow.png"></body></html>';
    void cdp.send('Fetch.fulfillRequest', { requestId: message.params.requestId, responseCode: 200, responseHeaders: [{ name: 'Content-Type', value: session ? 'application/json' : 'text/html' }], body: Buffer.from(body).toString('base64') }, message.sessionId).catch(error => errors.push(error.message));
  });
  const evaluate = async expression => (await cdp.send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true }, sessionId)).result?.value;
  await cdp.send('Page.navigate', { url: 'https://chatgpt.com/' }, sessionId);
  for (let i = 0; i < 100; i++) {
    if (slowResource && await evaluate('document.readyState') === 'interactive') break;
    await delay(30);
  }
  assert.equal(slowResource, true);
  assert.equal(await evaluate('document.readyState'), 'interactive');
  const status = await evaluate(loginCheckSource());
  assert.equal(status.email, 'test@example.com');
  const captured = await captureBrowserSession({ cdp, expectedEmail: 'test@example.com' });
  assert.equal(captured.accessToken, 'fixture-token');
  assert.equal(sessionRequests, 2);
  assert.equal(await evaluate('document.readyState'), 'interactive', '无需等待慢图片完成');
  assert.deepEqual(errors, []);
});

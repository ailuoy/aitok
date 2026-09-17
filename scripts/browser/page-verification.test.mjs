import test from 'node:test';
import assert from 'node:assert/strict';
import { runInNewContext } from 'node:vm';
import { loginCheckSource } from './page-verification.mjs';

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

test('登录接口的 Cloudflare 挑战响应不会误报 Cookie 失效或登录成功', async () => {
  const { context, check } = page();
  context.fetch = async () => new Response('<html>verification</html>', { status: 403, headers: { 'cf-mitigated': 'challenge' } });
  const result = await check();
  assert.equal(result.challenge, true);
  assert.equal(result.status, 403);
  assert.equal(result.email, undefined);
});

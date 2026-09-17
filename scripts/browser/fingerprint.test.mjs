import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import http from 'node:http';
import { EventEmitter, once } from 'node:events';
import { setTimeout as delay } from 'node:timers/promises';
import { SessionBrowser, findChrome } from './session.mjs';
import { profileDirectory, readFingerprint, fingerprintSummary } from './fingerprint-store.mjs';
import { FingerprintRuntime } from './fingerprint-runtime.mjs';
import { createLauncher } from './launcher-server.mjs';

test('账号独立指纹持久化，重随机不修改资料，失败保留配置且更新互斥', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'aitok-fingerprint-store-'));
  const browser = new SessionBrowser({ chrome: 'must-not-run', directory });
  const first = await browser.resetFingerprint('one');
  const second = await browser.resetFingerprint('two');
  assert.notEqual(first.fingerprint.id, second.fingerprint.id);
  assert.equal(first.fingerprint.generation, 1);
  assert.equal(first.fingerprint.seed, undefined);
  const profile = profileDirectory(directory, 'one');
  await writeFile(join(profile, 'keep-login-data'), 'login-fixture');
  const loaded = new SessionBrowser({ chrome: '', directory });
  assert.deepEqual(await loaded.fingerprintInfo('one'), first.fingerprint);
  const resetting = loaded.resetFingerprint('one');
  await assert.rejects(loaded.resetFingerprint('one'), /正在更新/);
  await assert.rejects(loaded.start({ environment_id: 'one', session: { accessToken: 'test' } }), /正在更新/);
  const changed = await resetting;
  assert.notEqual(changed.fingerprint.id, first.fingerprint.id);
  assert.equal(changed.fingerprint.generation, 2);
  assert.equal(await readFile(join(profile, 'keep-login-data'), 'utf8'), 'login-fixture');
  loaded.environments.set('one', { state: 'opened' });
  await assert.rejects(loaded.resetFingerprint('one'), /关闭账号窗口/);
  loaded.environments.clear();
  await writeFile(join(profile, 'fingerprint.json'), 'broken');
  await assert.rejects(loaded.resetFingerprint('one'), /保留原文件/);
  assert.equal(await readFile(join(profile, 'fingerprint.json'), 'utf8'), 'broken');
  await loaded.close();
  await assert.rejects(loaded.resetFingerprint('one'), /已停止/);
});

test('新页面初始化失败仍恢复执行，不接管不相关目标，关闭时移除监听器', async () => {
  const calls = [];
  const cdp = Object.assign(new EventEmitter(), { send: async (method, params, sessionId) => {
    calls.push({ method, params, sessionId });
    if (method === 'Target.setAutoAttach' && sessionId) throw new Error('标签已关闭');
    return {};
  } });
  const runtime = new FingerprintRuntime(cdp, { seed: 'a'.repeat(64) });
  await runtime.start();
  const config = calls.find(call => call.method === 'Target.setAutoAttach').params;
  assert.equal(config.waitForDebuggerOnStart, true);
  assert.deepEqual(config.filter.at(-1), { exclude: true });
  cdp.emit('message', { method: 'Target.attachedToTarget', params: { sessionId: 'child', targetInfo: { type: 'page', targetId: 'new' }, waitingForDebugger: true } });
  await Promise.all([...runtime.pending]);
  assert.equal(calls.at(-1).method, 'Runtime.runIfWaitingForDebugger');
  assert.equal(runtime.errors, 1);
  runtime.close();
  assert.equal(cdp.listenerCount('message'), 0);
});

test('指纹接口只接受本站账号，返回摘要且支持助手重启后读取', async t => {
  const directory = await mkdtemp(join(tmpdir(), 'aitok-fingerprint-api-'));
  const browser = new SessionBrowser({ chrome: '', directory });
  const origin = 'http://localhost:15680';
  const server = createLauncher({ origin, browser, enforceOriginScope: true });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  t.after(() => { server.closeAllConnections(); server.close(); });
  const call = (id, method, suffix = '') => fetch(`http://127.0.0.1:${server.address().port}/browsers/${encodeURIComponent(id)}${suffix}`, {
    method, headers: { Origin: origin, 'X-AiTok-Client': 'browser', 'Content-Type': 'application/json' }, ...(method === 'POST' ? { body: '{}' } : {}),
  });
  assert.equal((await call('https://other:user:1:account:1', 'POST', '/fingerprint')).status, 403);
  const id = origin + ':user:1:account:1';
  const reset = await (await call(id, 'POST', '/fingerprint')).json();
  browser.fingerprints.clear();
  const status = await (await call(id, 'GET')).json();
  assert.deepEqual(status.fingerprint, reset.fingerprint);
  const saved = await readFingerprint(profileDirectory(directory, id));
  assert.ok(!JSON.stringify(status).includes(saved.seed));
});

function probe() {
  const canvas = typeof document === 'object' ? document.createElement('canvas') : new OffscreenCanvas(64, 64);
  canvas.width = canvas.height = 64;
  const context = canvas.getContext('2d');
  context.fillStyle = '#4488cc'; context.fillRect(0, 0, 64, 64);
  const hash = pixels => { let value = 2166136261; for (const pixel of pixels) value = Math.imul(value ^ pixel, 16777619) >>> 0; return value; };
  const first = hash(context.getImageData(0, 0, 64, 64).data), second = hash(context.getImageData(0, 0, 64, 64).data);
  const image = canvas.toDataURL?.();
  const afterExport = hash(context.getImageData(0, 0, 64, 64).data);
  const graphics = typeof document === 'object' ? document.createElement('canvas') : new OffscreenCanvas(64, 64);
  graphics.width = graphics.height = 64;
  const gl = graphics.getContext('webgl');
  let webgl = null;
  if (gl) {
    gl.clearColor(0.25, 0.5, 0.75, 1); gl.clear(gl.COLOR_BUFFER_BIT);
    const pixels = new Uint8Array(64 * 64 * 4);
    gl.readPixels(0, 0, 64, 64, gl.RGBA, gl.UNSIGNED_BYTE, pixels); webgl = hash(pixels);
  }
  return { first, second, afterExport, image, webgl };
}

test('真实 Chromium 首屏、刷新、iframe 和 Worker 使用稳定独立指纹，重开不变且重随机保留 Cookie', { skip: !process.env.AITOK_BROWSER_SMOKE, timeout: 60000 }, async t => {
  const source = `(${probe.toString()})()`;
  const server = http.createServer((request, response) => {
    response.setHeader('Content-Type', 'text/html');
    response.end(`<script>window.initialProbe=${source};if(window.parent!==window)parent.postMessage({fingerprintProbe:window.initialProbe},'*');</script><div id="box" style="width:100px;height:50px">box</div>`);
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  t.after(() => { server.closeAllConnections(); server.close(); });
  const directory = await mkdtemp(join(tmpdir(), 'aitok-fingerprint-smoke-'));
  const options = { chrome: await findChrome(), directory, headless: true, startURL: 'about:blank', billingURL: 'about:blank', cleanipURL: null, checkIP: async () => ({ ok: true, exit_ip: '203.0.113.1', matches: null }) };
  let browser = new SessionBrowser(options);
  t.after(() => browser.close());
  const session = { accessToken: 'test-only' };
  const open = async id => {
    await browser.start({ environment_id: id, session });
    const environment = browser.environments.get(id); await environment.opening;
    const cdp = environment.cdp;
    const send = cdp.send.bind(cdp);
    cdp.send = async (method, params, sessionId) => {
      try { return await send(method, params, sessionId); }
      catch (error) { throw new Error(method + ': ' + error.message); }
    };
    const { targetId } = await cdp.send('Target.createTarget', { url: `http://127.0.0.1:${server.address().port}/` });
    const sessionId = await environment.fingerprintRuntime.pageSession(targetId);
    const evaluate = async expression => {
      const result = await cdp.send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true }, sessionId);
      if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description || '脚本执行失败');
      return result.result?.value;
    };
    let initial;
    for (let attempt = 0; attempt < 100; attempt++) { initial = await evaluate('window.initialProbe'); if (initial) break; await delay(50); }
    assert.ok(initial, '页面首个脚本应完成');
    return { environment, cdp, sessionId, evaluate, initial };
  };
  const one = await open('one');
  assert.equal(one.initial.first, one.initial.second);
  assert.equal(one.initial.first, one.initial.afterExport, '导出和读取不能累积修改原画布');
  assert.deepEqual(await one.evaluate(source), one.initial);
  assert.equal(await one.evaluate('navigator.webdriver'), false);
  const frameResult = await one.evaluate(`new Promise(resolve => { const frame=document.createElement('iframe'); frame.src='/frame'; frame.onload=()=>resolve(frame.contentWindow.initialProbe); document.body.append(frame); })`);
  assert.equal(frameResult.first, one.initial.first);
  const crossFrame = await one.evaluate(`new Promise(resolve => { const frame=document.createElement('iframe'); const listener=event=>{if(event.source===frame.contentWindow&&event.data?.fingerprintProbe){removeEventListener('message',listener);resolve(event.data.fingerprintProbe)}};addEventListener('message',listener);frame.src='http://localhost:${server.address().port}/cross-frame';document.body.append(frame); })`);
  assert.equal(crossFrame.first, one.initial.first, '跨站 iframe 也在首屏脚本前加载');
  const worker = await one.evaluate(`new Promise((resolve,reject) => { const worker=new Worker(URL.createObjectURL(new Blob([${JSON.stringify(`postMessage((${probe.toString()})())`)}],{type:'text/javascript'}))); const timer=setTimeout(()=>{worker.terminate();reject(new Error('Worker timeout'));},5000); worker.onmessage=event=>{clearTimeout(timer);worker.terminate();resolve(event.data)}; worker.onerror=()=>{clearTimeout(timer);reject(new Error('Worker failed'))}; })`);
  assert.equal(worker.first, one.initial.first, 'Worker 与主页面采用相同种子');
  assert.equal(one.environment.fingerprintRuntime.errors, 0);
  await one.evaluate('window.beforeReload = true');
  await one.cdp.send('Page.reload', {}, one.sessionId);
  let reloaded;
  for (let attempt = 0; attempt < 100; attempt++) { reloaded = await one.evaluate('!window.beforeReload && window.initialProbe'); if (reloaded) break; await delay(50); }
  assert.deepEqual(reloaded, one.initial);
  const box = await one.evaluate('(()=>{const element=document.querySelector("#box"),a=element.getBoundingClientRect(),b=element.getClientRects().item(0);return {x:a.x,second:b.x,width:a.width,height:a.height,empty:document.createElement("div").getBoundingClientRect().x}})()');
  assert.equal(box.x, box.second); assert.equal(box.width, 100); assert.equal(box.height, 50); assert.equal(box.empty, 0);
  await one.cdp.send('Storage.setCookies', { cookies: [{ name: 'keep', value: 'cookie-fixture', domain: '127.0.0.1', path: '/', expires: Date.now() / 1000 + 3600 }] });
  const two = await open('two');
  assert.notEqual(two.initial.first, one.initial.first);
  if (two.initial.webgl !== null && one.initial.webgl !== null) assert.notEqual(two.initial.webgl, one.initial.webgl);
  const originalFingerprint = browser.status('one').fingerprint;
  await browser.close();
  browser = new SessionBrowser(options);
  const reopened = await open('one');
  assert.deepEqual(reopened.initial, one.initial);
  assert.deepEqual(browser.status('one').fingerprint, originalFingerprint);
  await browser.close();
  browser = new SessionBrowser(options);
  await browser.resetFingerprint('one');
  const regenerated = await open('one');
  assert.notEqual(regenerated.initial.first, one.initial.first);
  assert.equal(browser.status('one').fingerprint.generation, 2);
  assert.ok((await regenerated.cdp.send('Storage.getCookies')).cookies.some(cookie => cookie.name === 'keep' && cookie.value === 'cookie-fixture'));
});

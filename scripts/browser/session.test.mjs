import test from 'node:test';
import assert from 'node:assert/strict';
import net from 'node:net';
import http from 'node:http';
import { once } from 'node:events';
import { mkdtemp } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { validateSession, SessionBrowser, findChrome } from './session.mjs';
import { parseProxy, createProxyBridge } from './proxy.mjs';
import { createLauncher } from '../session-browser.mjs';
import { loginCookies, restoreLoginCookies } from './cookies.mjs';

const session = { accessToken: 'test-access-only', user: { email: 'test@example.com' }, refreshToken: 'never-export' };

test('停止站点时中止尚未启动的账号，退出后不能再生成浏览器进程', async () => {
  const browser = new SessionBrowser({ chrome: 'must-not-run', directory: await mkdtemp(join(tmpdir(), 'aitok-close-test-')) });
  const starting = browser.start({ environment_id: 'pending', session });
  browser.close();
  await assert.rejects(starting, /服务已停止/);
  assert.equal(browser.environments.size, 0);
  await assert.rejects(browser.start({ environment_id: 'after-close', session }), /服务已停止/);
});

test('Session 拒绝过期和头部注入，过滤长期凭据', () => {
  assert.equal(validateSession(session).refreshToken, undefined);
  for (const value of [null, [], {}, { accessToken: 'x\r\nAuthorization: x' }, { accessToken: 'x\0' }, { ...session, expires: '2020-01-01T00:00:00Z' }]) {
    assert.throws(() => validateSession(value));
  }
  const expired = 'x.' + Buffer.from(JSON.stringify({ exp: 1 })).toString('base64url') + '.x';
  assert.throws(() => validateSession({ accessToken: expired, expires: '2099-01-01T00:00:00Z' }));
});

test('登录 Cookie 仅限 ChatGPT 并在导入前清理旧的分段 Cookie', async () => {
  const clean = loginCookies({ sessionToken: 'cookie-secret', cookies: [
    { name: '__Secure-next-auth.session-token.0', value: 'chunk', domain: '.chatgpt.com' },
    { name: 'other', value: 'unrelated' },
    { name: '__Secure-next-auth.session-token', value: 'foreign', domain: 'example.com' },
  ] });
  assert.equal(clean.length, 2);
  assert.ok(clean.every(cookie => cookie.domain === '.chatgpt.com' && cookie.httpOnly && cookie.secure));
  assert.throws(() => loginCookies({ sessionToken: 'bad;cookie' }));
  const calls = [];
  const cdp = { send: async (method, params) => {
    calls.push({ method, params });
    return { cookies: [{ name: '__Secure-next-auth.session-token.1', value: 'old', domain: '.chatgpt.com', path: '/' }] };
  } };
  await restoreLoginCookies(cdp, { sessionToken: 'new' });
  assert.deepEqual(calls.map(call => call.method), ['Storage.getCookies', 'Storage.setCookies', 'Storage.setCookies']);
  assert.equal(calls[1].params.cookies[0].expires, 1);
  assert.equal(calls[2].params.cookies[0].value, 'new');
});

test('完整登录 Token 按认证库规则分段，保留内容且重复校验不改变分段', () => {
  for (const name of ['__Secure-next-auth.session-token', '__Secure-authjs.session-token']) {
    for (const size of [3936, 3937, 7872, 16000]) {
      const value = 'abcd'.repeat(4000).slice(0, size);
      const cookies = loginCookies({ cookies: [{ name, value }] });
      assert.equal(cookies.map(cookie => cookie.value).join(''), value);
      assert.deepEqual(cookies.map(cookie => cookie.name), size <= 3936 ? [name] : Array.from({ length: Math.ceil(size / 3936) }, (_, index) => `${name}.${index}`));
      assert.ok(cookies.every(cookie => cookie.name.length + cookie.value.length <= 4096));
      assert.deepEqual(loginCookies({ cookies }), cookies);
    }
  }
  const cookies = loginCookies({ sessionToken: 'a'.repeat(5000), cookies: [
    { name: '__Secure-next-auth.session-token.9', value: 'stale' },
    { name: '__Secure-authjs.session-token.0', value: 'preserved' },
  ] });
  assert.ok(!cookies.some(cookie => cookie.name.endsWith('.9')));
  assert.ok(cookies.some(cookie => cookie.value === 'preserved'));
  assert.deepEqual(loginCookies({ cookies: [{ name: ['__Secure-next-auth.session-token'], value: 'invalid-name-type' }] }), []);
});

test('已分段 Cookie 超限时在修改浏览器存储前给出明确错误', async () => {
  await assert.rejects(restoreLoginCookies({ send: () => assert.fail('无效分段不能修改浏览器') }, {
    cookies: [{ name: '__Secure-next-auth.session-token.0', value: 'a'.repeat(5000) }],
  }), /Cookie 分段过长/);
});

test('无密钥启动器仍拒绝错误来源、Host 及普通表单请求', async t => {
  const calls = [];
  const browser = { start: async input => { calls.push(input); return { state: 'opened' }; }, status: () => ({ state: 'closed' }), stop: async () => ({ state: 'closing' }) };
  const server = createLauncher({ origin: 'http://localhost:15680', browser });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  t.after(() => { server.closeAllConnections(); server.close(); });
  const base = `http://127.0.0.1:${server.address().port}`;
  const headers = { Origin: 'http://localhost:15680', 'X-AiTok-Client': 'browser', 'Content-Type': 'application/json' };
  assert.equal((await fetch(base + '/health')).status, 403);
  assert.equal((await fetch(base + '/health', { headers: { ...headers, Origin: 'https://evil.test' } })).status, 403);
  const wrongHostStatus = await new Promise((resolve, reject) => {
    http.get(base + '/health', { headers: { ...headers, Host: 'evil.test' } }, response => { response.resume(); resolve(response.statusCode); }).on('error', reject);
  });
  assert.equal(wrongHostStatus, 403);
  assert.equal((await fetch(base + '/health', { headers: { ...headers, 'X-AiTok-Client': '' } })).status, 403);
  const preflight = await fetch(base + '/browsers', { method: 'OPTIONS', headers: { Origin: headers.Origin } });
  assert.equal(preflight.status, 204);
  assert.equal(preflight.headers.get('access-control-allow-origin'), headers.Origin);
  assert.equal(preflight.headers.get('access-control-allow-private-network'), 'true');
  const result = await fetch(base + '/browsers', { method: 'POST', headers, body: JSON.stringify({ environment_id: 'test', session }) });
  assert.equal(result.status, 200); assert.equal(calls.length, 1);
  assert.equal(result.headers.get('cache-control'), 'no-store');
  assert.equal((await fetch(base + '/browsers', { method: 'POST', headers, body: 'null' })).status, 400);
  assert.equal((await fetch(base + '/browsers', { method: 'POST', headers, body: 'x'.repeat(256 * 1024 + 1) })).status, 413);
});

test('SOCKS5 代理配置解析', () => {
  assert.equal(parseProxy(''), null);
  assert.deepEqual(parseProxy('socks5://user:pass%40word@[::1]:1080'), { host: '::1', port: 1080, username: 'user', password: 'pass@word' });
  for (const url of ['https://host:1080', 'socks5://host', 'socks5://host:0', 'socks5://host:65536', 'socks5://host:1080/path', 'socks5://user@host:1080']) assert.throws(() => parseProxy(url));
});

test('SOCKS5 桥接认证、远端域名解析与双向数据传输', { timeout: 10000 }, async t => {
  const upstream = net.createServer(socket => {
    t.after(() => socket.destroy());
    let stage = 0;
    socket.on('data', data => {
      if (stage === 0) {
        assert.deepEqual(data, Buffer.from([5, 1, 2])); socket.write(Buffer.from([5, 2]));
      } else if (stage === 1) {
        assert.deepEqual(data, Buffer.from([1, 1, 117, 1, 112])); socket.write(Buffer.from([1, 0]));
      } else if (stage === 2) {
        assert.equal(data[3], 3);
        assert.equal(data.subarray(5, 5 + data[4]).toString(), 'unresolved.example');
        // 与握手同包的数据也必须完整送给客户端。
        socket.write(Buffer.concat([Buffer.from([5, 0, 0, 1, 127, 0, 0, 1, 1, 187]), Buffer.from('hello')]));
      } else { socket.write(data); }
      stage++;
    });
  });
  upstream.listen(0, '127.0.0.1'); await once(upstream, 'listening');
  t.after(() => upstream.close());
  const bridge = await createProxyBridge({ host: '127.0.0.1', port: upstream.address().port, username: 'u', password: 'p' });
  t.after(() => bridge.close());
  const client = net.createConnection({ host: '127.0.0.1', port: Number(new URL(bridge.url).port) });
  t.after(() => client.destroy());
  await once(client, 'connect');
  client.write(Buffer.from([5, 1, 0]));
  assert.deepEqual((await once(client, 'data'))[0], Buffer.from([5, 0]));
  const domain = Buffer.from('unresolved.example');
  client.write(Buffer.concat([Buffer.from([5, 1, 0, 3, domain.length]), domain, Buffer.from([1, 187])]));
  let received = Buffer.alloc(0);
  while (received.length < 15) received = Buffer.concat([received, (await once(client, 'data'))[0]]);
  assert.equal(received.subarray(10).toString(), 'hello');
  client.write('echo');
  assert.equal((await once(client, 'data'))[0].toString(), 'echo');
});

test('只根据真实网页登录响应确认身份，退出和账号不匹配不会误报成功', async () => {
  const browser = new SessionBrowser({ chrome: '', directory: '' });
  let value = { status: 200, email: null };
  const environment = { state: 'opened', apiStatus: null, session, pages: new Map([['chat', 'page']]), cdp: { send: async method => method === 'Target.getTargets' ? { targetInfos: [{ type: 'page', targetId: 'chat', url: 'https://chatgpt.com/' }] } : { result: { value } } } };
  await browser.verifyLogin(environment);
  assert.equal(environment.state, 'login_required');
  value = { status: 200, email: 'other@example.com' };
  await browser.verifyLogin(environment);
  assert.equal(environment.state, 'rejected');
  value = { status: 200, email: 'test@example.com' };
  await browser.verifyLogin(environment);
  assert.equal(environment.state, 'authenticated');
  const loggedInAt = environment.authenticatedAt;
  assert.ok(Number.isFinite(Date.parse(loggedInAt)));
  await browser.verifyLogin(environment);
  assert.equal(environment.authenticatedAt, loggedInAt);
  value = { status: 401 };
  await browser.verifyLogin(environment);
  assert.equal(environment.state, 'login_required');
  value = { status: 403 };
  await browser.verifyLogin(environment);
  assert.equal(environment.state, 'unverified');
  assert.ok(!JSON.stringify(browser.status('unknown')).includes('test-access-only'));
});

test('Cloudflare 接口挑战期间退避，验证恢复后继续确认账号', async () => {
  const browser = new SessionBrowser({ chrome: '', directory: '' });
  let requests = 0, value = { challenge: true, status: 403 };
  const environment = { state: 'opened', session, pages: new Map([['chat', 'page']]), cdp: { send: async method => {
    requests++;
    return method === 'Target.getTargets' ? { targetInfos: [{ type: 'page', targetId: 'chat', url: 'https://chatgpt.com/' }] } : { result: { value } };
  } } };
  await browser.verifyLogin(environment);
  assert.equal(environment.state, 'unverified');
  assert.match(environment.message, /Cloudflare/);
  assert.ok(environment.loginCheckAfter > Date.now());
  await browser.verifyLogin(environment);
  assert.equal(requests, 2);
  environment.loginCheckAfter = 0;
  value = { status: 200, email: session.user.email };
  await browser.verifyLogin(environment);
  assert.equal(environment.state, 'authenticated');
});

// 只打开 about:blank；检查真实 Chromium Cookie 存储，不访问用户账号或 ChatGPT 上游。
test('Chromium 正确恢复 HttpOnly 登录 Cookie；单独 accessToken 不声称已登录', { skip: !process.env.AITOK_BROWSER_SMOKE, timeout: 30000 }, async t => {
  const directory = await mkdtemp(join(tmpdir(), 'aitok-browser-smoke-'));
  const browser = new SessionBrowser({ chrome: await findChrome(), directory, headless: true, startURL: 'about:blank', billingURL: 'about:blank', cleanipURL: null, checkIP: async () => ({ ok: true, exit_ip: '203.0.113.1', matches: null }) });
  t.after(() => browser.close());
  const longToken = 'abcd'.repeat(4000);
  const assistantEndpoint = 'http://127.0.0.1:1/api/browser-assistant';
  await browser.start({ environment_id: 'cookie-account', session: { ...session, sessionToken: longToken }, assistant_token: 'test-assistant', assistant_endpoint: assistantEndpoint });
  const environment = browser.environments.get('cookie-account');
  await environment.opening;
  assert.ok(environment.assistant);
  const restored = await environment.cdp.send('Storage.getCookies');
  const chunks = restored.cookies.filter(cookie => cookie.name.startsWith('__Secure-next-auth.session-token.'))
    .sort((left, right) => Number(left.name.split('.').at(-1)) - Number(right.name.split('.').at(-1)));
  assert.equal(chunks.length, 5);
  assert.equal(chunks.map(cookie => cookie.value).join(''), longToken);
  assert.ok(chunks.every(cookie => cookie.httpOnly && cookie.secure));
  await environment.cdp.send('Storage.setCookies', { cookies: [
    { name: '__Secure-next-auth.session-token.1', value: 'old-chunk', domain: '.chatgpt.com', path: '/', secure: true },
    { name: 'unrelated', value: 'keep', domain: '.chatgpt.com', path: '/' },
    { name: '__Secure-next-auth.session-token', value: 'foreign', domain: '.example.com', path: '/', secure: true },
  ] });
  await restoreLoginCookies(environment.cdp, { ...session, sessionToken: 'test-login-cookie' });
  const result = await environment.cdp.send('Storage.getCookies');
  assert.ok(!result.cookies.some(cookie => /^__Secure-next-auth\.session-token\.\d+$/.test(cookie.name)));
  assert.ok(result.cookies.some(cookie => cookie.name === 'unrelated' && cookie.value === 'keep'));
  assert.ok(result.cookies.some(cookie => cookie.domain === '.example.com' && cookie.value === 'foreign'));
  const cookie = result.cookies.find(cookie => cookie.name === '__Secure-next-auth.session-token' && cookie.domain === '.chatgpt.com');
  assert.equal(cookie.value, 'test-login-cookie');
  assert.equal(cookie.httpOnly, true);
  assert.equal(cookie.secure, true);
  assert.equal(browser.status('cookie-account').state, 'opened');
  await assert.rejects(browser.start({ environment_id: 'cookie-account', session }), /已经打开/);
  await browser.start({ environment_id: 'token-only', session, assistant_endpoint: assistantEndpoint });
  await browser.environments.get('token-only').opening;
  assert.equal(browser.environments.get('token-only').assistant, undefined, '不提供助手授权时不创建面板或注入助手脚本');
  assert.equal(browser.status('token-only').state, 'opened');
  assert.match(browser.status('token-only').message, /未提供登录 Cookie/);
  await browser.stop('cookie-account');
  await browser.stop('token-only');
});

test('只检查 ChatGPT 标签，关闭标签或调试失败不会关闭浏览器或误报登录失效', async () => {
  const browser = new SessionBrowser({ chrome: '', directory: '' });
  let targets = [{ targetId: 'home', type: 'page', url: 'https://cleanip.io/' }];
  const calls = [];
  const environment = { state: 'opened', session, pages: new Map(), child: { kill: () => assert.fail('不能因页面检查失败结束浏览器') }, cdp: { send: async (method, params, sessionID) => {
    calls.push({ method, params, sessionID });
    if (method === 'Target.getTargets') return { targetInfos: targets };
    if (method === 'Target.attachToTarget') {
      if (params.targetId === 'gone') throw new Error('No target with given id found');
      return { sessionId: 'chat-session' };
    }
    return { result: { value: { status: 200, email: session.user.email } } };
  } } };
  await browser.verifyLogin(environment);
  assert.equal(environment.state, 'opened');
  assert.deepEqual(calls.map(call => call.method), ['Target.getTargets']);
  targets.push({ targetId: 'iframe', type: 'iframe', url: 'https://chatgpt.com/' }, { targetId: 'foreign', type: 'page', url: 'https://chatgpt.com.evil.test/' }, { targetId: 'gone', type: 'page', url: 'https://chatgpt.com/' }, { targetId: 'new-chat', type: 'page', url: 'https://chatgpt.com/' });
  await browser.verifyLogin(environment);
  assert.equal(environment.state, 'authenticated');
  assert.deepEqual(calls.filter(call => call.method === 'Target.attachToTarget').map(call => call.params.targetId), ['gone', 'new-chat']);
  assert.equal(calls.at(-1).sessionID, 'chat-session');
  targets = [];
  await browser.verifyLogin(environment);
  assert.equal(environment.pages.size, 0);
  environment.cdp.send = async () => { throw new Error('连接中断'); };
  await browser.verifyLogin(environment);
  assert.equal(environment.state, 'authenticated');
  assert.equal(environment.verifying, false);
});

test('Chromium 新标签输入、导航、刷新和快速关闭不会结束整个浏览器', { skip: !process.env.AITOK_BROWSER_SMOKE, timeout: 30000 }, async t => {
  const server = http.createServer((request, response) => {
    response.setHeader('Content-Type', 'text/html');
    response.end('<input id="query"><iframe src="about:blank"></iframe><script>new Worker(URL.createObjectURL(new Blob(["self.onmessage = () => postMessage(1)"])))</script>');
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  t.after(() => { server.closeAllConnections(); server.close(); });
  const browser = new SessionBrowser({ chrome: await findChrome(), directory: await mkdtemp(join(tmpdir(), 'aitok-tabs-smoke-')), headless: true, startURL: 'about:blank', billingURL: 'about:blank', cleanipURL: null, checkIP: async () => ({ ok: true, exit_ip: '203.0.113.1', matches: null }) });
  t.after(() => browser.close());
  await browser.start({ environment_id: 'tabs', session: { ...session, sessionToken: 'test-cookie' } });
  const environment = browser.environments.get('tabs');
  await environment.opening;
  const { cdp } = environment;
  for (let i = 0; i < 10; i++) {
    const { targetId } = await cdp.send('Target.createTarget', { url: 'about:blank' });
    await cdp.send('Target.closeTarget', { targetId });
  }
  const { targetId } = await cdp.send('Target.createTarget', { url: 'chrome://newtab/' });
  const { sessionId } = await cdp.send('Target.attachToTarget', { targetId, flatten: true });
  await cdp.send('Page.navigate', { url: `http://127.0.0.1:${server.address().port}/search?q=test` }, sessionId);
  for (let attempt = 0; attempt < 50; attempt++) {
    const result = await cdp.send('Runtime.evaluate', { expression: 'Boolean(document.querySelector("#query"))', returnByValue: true }, sessionId);
    if (result.result.value) break;
    await delay(100);
  }
  await cdp.send('Runtime.evaluate', { expression: 'document.querySelector("#query").focus()' }, sessionId);
  await browser.syncPages(environment);
  const label = await cdp.send('Runtime.evaluate', { expression: 'Boolean(document.querySelector("#aitok-account-label"))', returnByValue: true }, sessionId);
  assert.equal(label.result.value, false);
  await cdp.send('Input.insertText', { text: '新标签输入内容' }, sessionId);
  const entered = await cdp.send('Runtime.evaluate', { expression: 'document.querySelector("#query").value', returnByValue: true }, sessionId);
  assert.equal(entered.result.value, '新标签输入内容');
  await cdp.send('Page.reload', {}, sessionId);
  for (let attempt = 0; attempt < 50; attempt++) {
    const page = await cdp.send('Runtime.evaluate', { expression: 'Boolean(document.querySelector("#query"))', returnByValue: true }, sessionId);
    if (page.result.value) break;
    await delay(100);
  }
  await browser.syncPages(environment);
  const restoredLabel = await cdp.send('Runtime.evaluate', { expression: 'Boolean(document.querySelector("#aitok-account-label"))', returnByValue: true }, sessionId);
  assert.equal(restoredLabel.result.value, false);
  await browser.verifyLogin(environment);
  await cdp.send('Target.closeTarget', { targetId });
  await delay(500);
  assert.equal(browser.status('tabs').state, 'opened');
  assert.equal(environment.child.exitCode, null);
  assert.equal(environment.child.signalCode, null);
  assert.ok((await cdp.send('Browser.getVersion')).product);
  for (const target of (await cdp.send('Target.getTargets')).targetInfos.filter(target => target.type === 'page')) {
    await cdp.send('Target.closeTarget', { targetId: target.targetId });
  }
  for (let attempt = 0; attempt < 40 && browser.status('tabs').state !== 'closed'; attempt++) await delay(100);
  assert.equal(browser.status('tabs').state, 'closed');
});

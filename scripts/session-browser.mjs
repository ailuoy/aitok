import http from 'node:http';
import { createHash, randomUUID } from 'node:crypto';
import { homedir } from 'node:os';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { parseArgs } from 'node:util';
import { findChrome, SessionBrowser } from './browser/session.mjs';
import { runWorker } from './browser/worker.mjs';
import { ProxyStore, proxyURL } from './browser/proxy-store.mjs';
import { testProxy } from './browser/proxy-test.mjs';
import { listenReplacing } from './browser/launcher-port.mjs';
import { parseProxy } from './browser/proxy.mjs';

export function createLauncher({ origin, browser, store, probeProxy = testProxy }) {
  const testing = new Set();
  const approvals = new Map();
  const runningProxies = new Map();
  if (store && browser.on) browser.on('activity', event => {
    if (event.action === 'login' && event.logged_in_at) {
      void store.recordLogin(event.environment_id, event.logged_in_at).catch(() => console.error('账号登录时间保存失败，请检查本机配置目录'));
    }
    const usage = runningProxies.get(event.environment_id);
    if (!usage) return;
    void store.recordUsage(usage.proxy, { ...event, email: usage.email }).catch(() => console.error('代理使用记录保存失败，请检查本机配置目录'));
    if (event.action === 'close') runningProxies.delete(event.environment_id);
  });
  const fingerprint = (id, value) => createHash('sha256').update(JSON.stringify([id, value])).digest('hex');
  const saveTested = async (id, input) => {
    const value = store.prepare(id, input);
    const approval = approvals.get(input.test_token);
    if (!approval || approval.expires <= Date.now() || approval.fingerprint !== fingerprint(id, value)) {
      throw new Error('请先测试当前代理配置，测试通过后才能保存');
    }
    approvals.delete(input.test_token);
    const saved = await store.save(id, value, approval.result);
    if (!id) await store.recordUsage(store.get(saved.id), { action: 'test', ok: approval.result.ok, exit_ip: approval.result.exit_ip, matches: approval.result.matches });
  };
  return http.createServer(async (request, response) => {
    const send = (status, data) => {
      response.writeHead(status, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' });
      response.end(JSON.stringify(data));
    };
    // 无配对密钥；限定启动时指定的站点、回环 Host 和需要预检的请求头。
    if (request.headers.origin !== origin || request.headers.host !== `127.0.0.1:${request.socket.localPort}`) {
      send(403, { error: '启动器来源不匹配，请检查 --origin 参数' }); return;
    }
    response.setHeader('Access-Control-Allow-Origin', origin);
    response.setHeader('Vary', 'Origin');
    response.setHeader('Access-Control-Allow-Private-Network', 'true');
    response.setHeader('Access-Control-Allow-Headers', 'Content-Type, X-AiTok-Client');
    response.setHeader('Access-Control-Allow-Methods', 'GET, POST, PATCH, DELETE, OPTIONS');
    if (request.method === 'OPTIONS') { response.writeHead(204); response.end(); return; }
    if (request.headers['x-aitok-client'] !== 'browser') {
      send(403, { error: '请从指定的 AiTok 站点调用启动器' }); return;
    }
    try {
      if (request.method === 'GET' && request.url === '/health') {
        send(200, { status: 'ok', version: 2 }); return;
      }
      let input;
      if (['POST', 'PATCH'].includes(request.method)) {
        if (!request.headers['content-type']?.startsWith('application/json')) {
          send(415, { error: '仅接受 JSON 请求' }); return;
        }
        let size = 0;
        const chunks = [];
        for await (const chunk of request) {
          size += chunk.length;
          if (size > 256 * 1024) { send(413, { error: 'Session JSON 过大' }); return; }
          chunks.push(chunk);
        }
        try { input = JSON.parse(Buffer.concat(chunks).toString()); } catch { send(400, { error: 'JSON 格式错误' }); return; }
        if (!input || typeof input !== 'object' || Array.isArray(input)) { send(400, { error: 'JSON 格式错误' }); return; }
      }
      if(request.method==='GET' && request.url?.startsWith('/activity-export?')) { const cursor=Number(new URL(request.url,'http://localhost').searchParams.get('cursor')||0);if(!Number.isSafeInteger(cursor)||cursor<0){send(400,{error:'游标无效'});return}await store.queue;send(200,store.exportActivity(cursor));return; }
      if (request.url === '/proxies' && request.method === 'GET') { send(200, store.list()); return; }
      if (request.url === '/proxies/parse' && request.method === 'POST') {
        const lines = typeof input.text === 'string' ? input.text.split(/\r?\n/).map(line => line.trim()).filter(Boolean) : [];
        if (!lines.length || lines.length > 100) { send(400, { error: '请粘贴 1 到 100 行代理，每行一条' }); return; }
        const items = lines.map((line, index) => {
          try { const value = parseProxy(line); return { line: index + 1, proxy: { ...value, name: value.host + ':' + value.port } }; }
          catch { return { line: index + 1, error: '格式无效，请检查主机、端口和认证信息' }; }
        });
        send(200, { items }); return;
      }
      if (request.method === 'GET' && request.url?.startsWith('/proxy-history?')) {
        const query = new URL(request.url, 'http://localhost').searchParams;
        const page = Number(query.get('page') || 1);
        const pageSize = Number(query.get('page_size') || 20);
        if (!Number.isInteger(pageSize) || pageSize < 1 || pageSize > 100) { send(400, { error: '每页条数无效' }); return; }
        if (!Number.isInteger(page) || page < 1 || page > 100000) { send(400, { error: '页码无效' }); return; }
        await store.queue;
        send(200, store.history(query.get('proxy_id'), page, pageSize)); return;
      }
      if (request.url === '/proxies/test' && request.method === 'POST') {
        if (testing.size >= 5) { send(429, { error: '代理正在测试，请稍后重试' }); return; }
        const id = input.id || null;
        const value = store.prepare(id, input);
        const key = randomUUID();
        testing.add(key);
        try {
          const result = await probeProxy(proxyURL(value));
          if (id) await store.recordUsage({ ...value, id }, { action: 'test', ok: result.ok, exit_ip: result.exit_ip, matches: result.matches });
          for (const [token, approval] of approvals) if (approval.expires <= Date.now()) approvals.delete(token);
          if (result.ok) {
            if (approvals.size >= 100) approvals.delete(approvals.keys().next().value);
            approvals.set(key, { fingerprint: fingerprint(id, value), result, expires: Date.now() + 5 * 60 * 1000 });
          }
          send(200, { result, test_token: result.ok ? key : null });
        } finally { testing.delete(key); }
        return;
      }
      if (request.url === '/proxies' && request.method === 'POST') { await saveTested(null, input); send(201, store.list()); return; }
      const proxyMatch = /^\/proxies\/([a-zA-Z0-9-]+)(\/test)?$/.exec(request.url || '');
      if (proxyMatch) {
        const id = proxyMatch[1];
        if (proxyMatch[2] && request.method === 'POST') {
          if (testing.has(id) || testing.size >= 5) { send(429, { error: '代理正在测试，请稍后重试' }); return; }
          const url = store.url(id);
          testing.add(id);
          const proxy = { ...store.get(id) };
          try {
            const result = await probeProxy(url);
            await store.recordTest(id, result, url);
            await store.recordUsage(proxy, { action: input.action === 'get_ip' ? 'get_ip' : 'test', ok: result.ok, exit_ip: result.exit_ip, matches: result.matches });
          }
          finally { testing.delete(id); }
          send(200, store.list()); return;
        }
        if (!proxyMatch[2] && request.method === 'GET') { send(200, store.get(id)); return; }
        if (!proxyMatch[2] && request.method === 'PATCH') { await saveTested(id, input); send(200, store.list()); return; }
        if (!proxyMatch[2] && request.method === 'DELETE') { await store.remove(id); send(200, store.list()); return; }
      }
      const binding = /^\/browsers\/([^/?]+)\/proxy$/.exec(request.url || '');
      if (binding && request.method === 'PATCH') {
        await store.bind(decodeURIComponent(binding[1]), input.proxy_id);
        send(200, store.list()); return;
      }
      if (request.method === 'POST' && request.url === '/browsers') {
        const proxyID = store?.data.bindings[input.environment_id];
        const proxyURL = proxyID ? store.url(proxyID) : '';
        if (runningProxies.has(input.environment_id)) throw new Error('该环境已经打开，请关闭后再打开');
        if (proxyID) runningProxies.set(input.environment_id, { proxy: { ...store.get(proxyID) }, email: input.expected_email });
        try {
          const status = await browser.start({ ...input, proxy_url: proxyURL, assistant_endpoint: origin + '/api/browser-assistant' });
          if (proxyID) await store.recordUsage(store.get(proxyID), { action: 'open', ok: true, environment_id: input.environment_id, email: input.expected_email });
          send(200, status);
        } catch (error) {
          if (proxyID) await store.recordUsage(store.get(proxyID), { action: 'open', ok: false, environment_id: input.environment_id, email: input.expected_email });
          runningProxies.delete(input.environment_id);
          throw error;
        }
        return;
      }
      const match = /^\/browsers\/([^/?]+)$/.exec(request.url || '');
      if (match && ['GET', 'DELETE'].includes(request.method)) {
        const id = decodeURIComponent(match[1]);
        const status = request.method === 'DELETE' ? await browser.stop(id) : browser.status(id);
        // 即使后台页面暂时关闭，下一次访问也能同步已确认的登录时间。
        send(200, { ...status, authenticated_at: status.authenticated_at || store?.data.logins?.[id] }); return;
      }
      send(404, { error: '接口不存在' });
    } catch (error) {
      // 不把系统异常中的 URL、命令或凭据返回页面。
      const message = /^[\u3400-\u9fff]/.test(error.message) ? error.message : '浏览器启动失败，请检查浏览器路径和本地环境';
      send(400, { error: message });
    }
  });
}

async function main() {
  const { values } = parseArgs({ options: {
    origin: { type: 'string', default: 'http://localhost:15680' },
    port: { type: 'string', default: '15683' },
    chrome: { type: 'string' },
    directory: { type: 'string', default: join(homedir(), '.aitok', 'browsers') },
    help: { type: 'boolean', default: false },
    stdio: { type: 'boolean', default: false },
  } });
  if (values.help) {
    console.log('用法：node scripts/session-browser.mjs [--stdio] [--origin https://站点] [--port 15683] [--chrome 浏览器路径] [--directory 环境目录]');
    return;
  }
  const url = new URL(values.origin);
  if (!['https:', 'http:'].includes(url.protocol) || url.origin !== values.origin) throw new Error('--origin 必须是 AiTok 页面完整来源，不包含末尾斜杠或路径');
  const port = Number(values.port);
  if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error('端口必须介于 1 和 65535');
  const chrome = await findChrome(values.chrome);
  const browser = new SessionBrowser({ chrome, directory: resolve(values.directory) });
  if (values.stdio) {
    runWorker(browser);
    const shutdown = () => { browser.close(); process.stdin.destroy(); };
    process.once('SIGINT', shutdown); process.once('SIGTERM', shutdown);
    return;
  }
  const store = await new ProxyStore(join(resolve(values.directory), 'settings', createHash('sha256').update(values.origin).digest('hex'))).load();
  const server = createLauncher({ origin: values.origin, browser, store });
  server.requestTimeout = 30000;
  await listenReplacing(server, port);
  console.log(`AiTok 本机浏览器启动器\n允许站点：${values.origin}\n本地地址：http://127.0.0.1:${port}\n无需配对，网页点击“打开账号”即可。\n请保持此终端运行。退出启动器将关闭由它启动的浏览器。`);
  const shutdown = () => { browser.close(); server.close(); setTimeout(() => process.exit(0), 1000).unref(); };
  process.once('SIGINT', shutdown); process.once('SIGTERM', shutdown);
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  main().catch(error => {
    const message = /[\u3400-\u9fff]/.test(error.message) ? error.message : ({ EACCES: '没有权限访问浏览器、配置目录或监听端口', ENOENT: '浏览器路径或所需系统命令不存在', EADDRINUSE: '端口仍被占用，请检查是否有服务自动重启' }[error.code] || '请检查浏览器路径及启动参数');
    console.error(`启动失败：${message}。使用 --help 查看说明。`);
    process.exitCode = 1;
  });
}

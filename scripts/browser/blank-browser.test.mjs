import test from 'node:test';
import assert from 'node:assert/strict';
import net from 'node:net';
import { once } from 'node:events';
import { mkdtemp } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { SessionBrowser, findChrome } from './session.mjs';

test('真实空白浏览器不依赖 Session，手动访问通过所选 SOCKS5 代理且窗口相互隔离', { skip: !process.env.AITOK_BROWSER_SMOKE, timeout: 45000 }, async t => {
  const destinations = [], sockets = new Set();
  const proxy = net.createServer(socket => {
    sockets.add(socket); socket.on('close', () => sockets.delete(socket)); socket.on('error', () => {});
    let stage = 0, buffer = Buffer.alloc(0);
    socket.on('data', chunk => {
      buffer = Buffer.concat([buffer, chunk]);
      if (stage === 0) {
        if (buffer.length < 2 || buffer.length < 2 + buffer[1]) return;
        buffer = buffer.subarray(2 + buffer[1]); socket.write(Buffer.from([5, 0])); stage = 1;
      }
      if (stage === 1) {
        if (buffer.length < 5) return;
        const length = buffer[3] === 3 ? 7 + buffer[4] : buffer[3] === 1 ? 10 : 22;
        if (buffer.length < length) return;
        if (buffer[3] === 3) destinations.push(buffer.subarray(5, 5 + buffer[4]).toString());
        buffer = buffer.subarray(length);
        socket.write(Buffer.from([5, 0, 0, 1, 127, 0, 0, 1, 0, 80])); stage = 2;
      }
      if (stage === 2 && buffer.includes(Buffer.from('\r\n\r\n'))) {
        const content = '<!doctype html><title>AiTok proxy verified</title>Manual login test';
        socket.end('HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: ' + Buffer.byteLength(content) + '\r\nConnection: close\r\n\r\n' + content);
        stage = 3;
      }
    });
  });
  proxy.listen(0, '127.0.0.1'); await once(proxy, 'listening');
  t.after(() => { for (const socket of sockets) socket.destroy(); proxy.close(); });
  const browser = new SessionBrowser({ chrome: await findChrome(), directory: await mkdtemp(join(tmpdir(), 'aitok-blank-browser-')), headless: true });
  t.after(() => browser.close());
  const proxyURL = 'socks5://127.0.0.1:' + proxy.address().port;
  assert.equal((await browser.startBlank({ environment_id: 'blank-one', proxy_url: proxyURL })).state, 'opened');
  const env = browser.environments.get('blank-one');
  assert.equal(env.session, null);
  assert.equal(env.assistant, undefined);
  assert.equal(env.verifyTimer, undefined);
  const targets = (await env.cdp.send('Target.getTargets')).targetInfos.filter(target => target.type === 'page');
  assert.equal(targets.length, 1);
  assert.equal(targets[0].url, 'about:blank');
  assert.equal((await env.cdp.send('Storage.getCookies')).cookies.length, 0);
  const sessionID = await env.fingerprintRuntime.pageSession(targets[0].targetId);
  await env.cdp.send('Page.navigate', { url: 'http://aitok-blank.test/' }, sessionID);
  let title;
  for (let attempt = 0; attempt < 100; attempt++) {
    title = (await env.cdp.send('Runtime.evaluate', { expression: 'document.title', returnByValue: true }, sessionID)).result?.value;
    if (title === 'AiTok proxy verified') break;
    await delay(100);
  }
  assert.equal(title, 'AiTok proxy verified');
  assert.ok(destinations.includes('aitok-blank.test'), '目标域名必须经 SOCKS5 代理访问');
  await browser.startBlank({ environment_id: 'blank-two', proxy_url: proxyURL });
  assert.notEqual(env.child.pid, browser.environments.get('blank-two').child.pid);
  const exited = once(env.child, 'exit');
  await browser.stop('blank-one'); await exited;
  assert.equal(browser.status('blank-one').state, 'closed');
  assert.equal(browser.status('blank-two').state, 'opened');
  await browser.close();
  assert.equal(browser.environments.size, 0);
});

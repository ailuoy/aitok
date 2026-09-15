import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { listenReplacing } from './launcher-port.mjs';

for (const force of [false, true]) {
  test(`启动器释放旧监听端口并保留其他端口（强制结束：${force}）`, { timeout: 15000 }, async t => {
    const child = spawn(process.execPath, ['--input-type=module', '-e', `
      import http from 'node:http';
      ${force ? "process.on('SIGTERM', () => {});" : ''}
      const server = http.createServer();
      server.listen(0, '127.0.0.1', () => process.send(server.address().port));
    `], { stdio: ['ignore', 'ignore', 'ignore', 'ipc'] });
    t.after(() => { if (child.exitCode === null && child.signalCode === null) child.kill('SIGKILL'); });
    const [port] = await once(child, 'message');
    const exited = once(child, 'exit');
    const unrelated = http.createServer((request, response) => response.end('unrelated'));
    unrelated.listen(0, '127.0.0.1'); await once(unrelated, 'listening');
    t.after(() => { unrelated.closeAllConnections(); unrelated.close(); });
    const replacement = http.createServer((request, response) => response.end('replacement'));
    t.after(() => { replacement.closeAllConnections(); replacement.close(); });
    await listenReplacing(replacement, port);
    await exited;
    assert.equal(await (await fetch(`http://127.0.0.1:${port}`)).text(), 'replacement');
    assert.equal(await (await fetch(`http://127.0.0.1:${unrelated.address().port}`)).text(), 'unrelated');
  });
}

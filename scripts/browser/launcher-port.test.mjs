import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import { once } from 'node:events';
import { listenLocal } from './launcher-port.mjs';

test('端口冲突保留旧监听器，不影响其他环境', async t => {
  const original = http.createServer((_request, response) => response.end('original'));
  original.listen(0, '127.0.0.1'); await once(original, 'listening');
  const port = original.address().port;
  t.after(() => { original.closeAllConnections(); original.close(); });
  const next = http.createServer();
  await assert.rejects(listenLocal(next, port), /已被占用/);
  assert.equal(await (await fetch(`http://127.0.0.1:${port}`)).text(), 'original');
  const unrelated = http.createServer((_request, response) => response.end('other'));
  t.after(() => { unrelated.closeAllConnections(); unrelated.close(); });
  await listenLocal(unrelated, 0);
  assert.equal(await (await fetch(`http://127.0.0.1:${unrelated.address().port}`)).text(), 'other');
});

// Electron 真机界面验证：临时目录、空站点列表，不连接业务服务。
import { app, BrowserWindow } from 'electron';
import assert from 'node:assert/strict';
import { mkdtemp, writeFile, mkdir } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { once } from 'node:events';
import { setTimeout as delay } from 'node:timers/promises';

async function run() {
setTimeout(() => { console.error('桌面冒烟测试超时'); app.exit(1); }, 30000).unref();
const directory = await mkdtemp(join(tmpdir(), 'aitok-desktop-smoke-'));
process.env.AITOK_DESKTOP_SMOKE_DIRECTORY = directory;
await writeFile(join(directory, 'sites.json'), JSON.stringify({ version: 1, sites: [] }));
const created = once(app, 'browser-window-created');
await import('../build/main.mjs');
const [, window] = await created;
await once(window.webContents, 'did-finish-load');
try {
  const evaluate = expression => window.webContents.executeJavaScript(expression);
  for (let attempt = 0; attempt < 30; attempt++) {
    if (await evaluate('document.querySelector("#summary").textContent.includes("0 个站点")')) break;
    await delay(100);
  }
  assert.equal(await evaluate('typeof window.require'), 'undefined');
  assert.equal(await evaluate('typeof window.assistant.save'), 'function');
  assert.equal((await evaluate('window.assistant.state()')).sites.length, 0);
  await evaluate(`(() => { document.querySelector('#add').click();
    const form = document.querySelector('#site-form');
    form.elements.name.value = '界面验证';
    form.elements.origin.value = ${JSON.stringify('http://localhost:15680')};
    form.elements.port.value = '19883';
    form.elements.enabled.checked = false;
    form.requestSubmit(); })()`);
  for (let attempt = 0; attempt < 30 && await evaluate('document.querySelector("#editor").open'); attempt++) await delay(100);
  assert.equal(await evaluate('document.querySelector("#sites .site h2").textContent'), '界面验证');
  assert.equal(await evaluate('document.querySelector("#login").textContent'), '授权登录');
  await evaluate(`render({ ...state, auth: { status: 'reconnecting', user: { username: '测试账号' }, error: '后台连接暂时不可用，正在自动重试' } })`);
  assert.equal(await evaluate('document.querySelector("#login-title").textContent'), '正在重连 · 测试账号');
  assert.equal(await evaluate('document.querySelector("#login").textContent'), '退出登录');
  await evaluate(`render({ ...state, auth: { status: 'reconnecting', user: null, error: '后台连接暂时不可用，正在自动重试' } })`);
  assert.equal(await evaluate('document.querySelector("#login-title").textContent'), '正在重连');
  await evaluate('refresh()');
  assert.equal((await evaluate('window.assistant.state()')).sites[0].running, false);
  assert.equal(await evaluate('document.querySelector("#startup").disabled'), true);
  await evaluate(`document.querySelector('.buttons button:nth-child(2)').click(); document.querySelector('#site-form').elements.port.value='19884'; document.querySelector('#site-form').requestSubmit();`);
  for (let attempt = 0; attempt < 30 && await evaluate('document.querySelector("#editor").open'); attempt++) await delay(100);
  assert.equal((await evaluate('window.assistant.state()')).sites[0].port, 19884);
  assert.equal(await evaluate('document.querySelector("#editor").open'), false);
  await delay(150);
  await mkdir(new URL('../.pack/test-artifacts/', import.meta.url), { recursive: true });
  await writeFile(new URL('../.pack/test-artifacts/desktop-smoke.png', import.meta.url), (await window.webContents.capturePage()).toPNG());
  window.close();
  assert.equal(window.isDestroyed(), false);
  assert.equal(window.isVisible(), false);
  assert.equal(BrowserWindow.getAllWindows().length, 1);
  console.log('桌面冒烟测试通过：隔离 IPC、站点保存/编辑、托盘后台运行。');
  app.exit(0);
} catch (error) {
  console.error(error);
  app.exit(1);
}

}
void run().catch(error => { console.error(error); app.exit(1); });

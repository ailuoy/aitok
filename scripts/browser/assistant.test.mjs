import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import { once } from 'node:events';
import { spawn } from 'node:child_process';
import { mkdtemp, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { CDP } from './cdp.mjs';
import { findChrome } from './session.mjs';
import { BrowserAssistant } from './assistant.mjs';
import { assistantPage } from './checkout-fill.mjs';

test('助手仅允许官方 HTTPS 页面', () => {
  for (const url of ['https://chatgpt.com/', 'https://checkout.stripe.com/c/pay/test', 'https://pay.openai.com/']) assert.equal(assistantPage(url), true);
  for (const url of ['http://chatgpt.com/', 'https://chatgpt.com.evil.test/', 'https://api.ipify.org/', 'https://cleanip.io/']) assert.equal(assistantPage(url), false);
});

test('真实 Chromium 助手隔离、右侧展示、拖拽、手动填充与页面刷新', { skip: !process.env.AITOK_BROWSER_SMOKE, timeout: 30000 }, async t => {
  const card = { id: 1, label: '工作卡', brand: 'Visa', last4: '4242', cardholder: 'TEST USER', exp_month: 12, exp_year: 2035, platform: '自定义卡平台', notes: '月度订阅\n仅工作用途' };
  const secondCard = { id: 2, label: '备用卡', brand: 'Mastercard', last4: '4444', cardholder: 'SECOND USER', exp_month: 10, exp_year: 2036 };
  const demoCard = { id: 3, label: '演示 Amex（测试卡）', brand: 'Amex', last4: '0005', cardholder: 'TEST USER', exp_month: 12, exp_year: 2030 };
  const address = { id: 1, full_name: 'TEST USER', address_line1: '100 Test Road', address_line2: 'Unit 2', city: 'Portland', state: 'OR', postal_code: '97201', country: 'US' };
  let detailReads = 0;
  const server = http.createServer((request, response) => {
    assert.equal(request.headers.authorization, 'Bearer limited-test-token');
    response.setHeader('Content-Type', 'application/json');
    if (request.url.endsWith('/cards/1')) { detailReads++; response.end(JSON.stringify({ card: { ...card, number: '4242424242424242' } })); }
    else if (request.url.endsWith('/cards/2')) { detailReads++; response.end(JSON.stringify({ card: { ...secondCard, number: '5555555555554444' } })); }
    else if (request.url.endsWith('/cards/3')) { detailReads++; response.end(JSON.stringify({ card: { ...demoCard, number: '378282246310005' } })); }
    else response.end(JSON.stringify({ cards: [card, secondCard, demoCard], addresses: [address] }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  t.after(() => { server.closeAllConnections(); server.close(); });
  const directory = await mkdtemp(join(tmpdir(), 'aitok-assistant-smoke-'));
  const child = spawn(await findChrome(), [`--user-data-dir=${directory}`, '--headless=new', '--remote-debugging-pipe', '--no-first-run', '--disable-background-networking', 'about:blank'], { stdio: ['ignore', 'ignore', 'ignore', 'pipe', 'pipe'] });
  t.after(() => child.kill());
  const cdp = new CDP(child);
  const env = { cdp, state: 'authenticated', actualEmail: 'test@example.com', plan: 'pro', expectedEmail: 'test@example.com', session: {} };
  const assistant = new BrowserAssistant(env, `http://127.0.0.1:${server.address().port}/api/browser-assistant`, 'limited-test-token');
  t.after(() => assistant.close());
  const { targetId } = await cdp.send('Target.createTarget', { url: 'about:blank' });
  const { sessionId } = await cdp.send('Target.attachToTarget', { targetId, flatten: true });
  await cdp.send('Page.enable', {}, sessionId);
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 1200, height: 1100, deviceScaleFactor: 1, mobile: false }, sessionId);
  await cdp.send('Fetch.enable', { patterns: [{ urlPattern: '*' }] }, sessionId);
  const fields = ['cc-name', 'cc-number', 'cc-exp', 'cc-csc', 'address-line1', 'address-line2', 'address-level2', 'address-level1', 'postal-code', 'country'];
  const fixture = '<html><head><meta charset="utf-8"></head><body style="font:16px system-ui;background:#f5f6fa;padding:40px 330px 40px 40px"><h1>收银表单测试</h1><form onsubmit="event.preventDefault();window.submitted=true">' + fields.map(name => `<label style="display:block;margin:12px">${name}<input style="display:block;padding:10px" autocomplete="${name}" id="${name}"></label>`).join('') + '<button>付款</button></form></body></html>';
  cdp.on('message', message => { if (message.method === 'Fetch.requestPaused') void cdp.send('Fetch.fulfillRequest', { requestId: message.params.requestId, responseCode: 200, responseHeaders: [{ name: 'Content-Type', value: 'text/html' }], body: Buffer.from(fixture).toString('base64') }, message.sessionId); });
  await cdp.send('Page.navigate', { url: 'https://chatgpt.com/' }, sessionId);
  const evaluate = async expression => (await cdp.send('Runtime.evaluate', { expression, returnByValue: true }, sessionId)).result.value;
  const wait = async check => { for (let i = 0; i < 100; i++) { if (await check()) return; await delay(80); } assert.fail('助手页面等待超时'); };
  await wait(() => evaluate('Boolean(document.querySelector("#cc-number"))'));
  await assistant.sync((await cdp.send('Target.getTargets')).targetInfos);
  await wait(() => evaluate('Boolean(document.getElementById("aitok-assistant"))'));
  assert.equal(await evaluate('typeof window.aitokAssistant'), 'undefined');
  assert.equal(await evaluate('document.querySelector("#aitok-assistant").shadowRoot'), null);
  async function nodes() {
    const { root } = await cdp.send('DOM.getDocument', { depth: -1, pierce: true }, sessionId);
    const result = []; const walk = node => { result.push(node); for (const child of [...(node.children || []), ...(node.shadowRoots || [])]) walk(child); }; walk(root); return result;
  }
  const text = node => (node.children || []).map(child => child.nodeValue || '').join('');
  await wait(async () => (await nodes()).some(node => text(node).includes('已读取 3 张银行卡')));
  const bounds = () => evaluate('document.querySelector("#aitok-assistant").getBoundingClientRect().toJSON()');
  assert.ok(Math.abs((await bounds()).right - 1188) <= 1, '助手默认靠右');
  assert.equal(detailReads, 1);
  assert.ok((await nodes()).some(node => text(node) === '4242424242424242'));
  assert.ok((await nodes()).some(node => text(node) === '0.0.1'));
  assert.ok((await nodes()).some(node => text(node) === '完整卡号'));
  assert.ok((await nodes()).some(node => text(node) === '账单姓名'));
  assert.ok(!(await nodes()).some(node => /删除登录状态|登录其他新账号|待充值队列/.test(node.nodeValue || '')));
  const detailField = async name => {
    const title = (await nodes()).find(node => node.nodeName === 'DT' && text(node) === name);
    assert.ok(title, name);
    const row = (await nodes()).find(node => node.children?.some(child => child.backendNodeId === title.backendNodeId));
    const dd = row.children.find(node => node.nodeName === 'DD');
    return { value: text(dd.children.find(node => node.nodeName === 'SPAN')), button: dd.children.find(node => node.nodeName === 'BUTTON') };
  };
  assert.equal((await detailField('安全码')).value, '未填写', '普通卡不推断安全码');
  assert.ok((await detailField('安全码')).button.attributes.includes('disabled'));
  const planButtons = (await nodes()).find(node => node.nodeName === 'DIV' && node.attributes?.includes('plans')).children;
  assert.deepEqual(planButtons.map(text), ['Plus', '5X', '20X']);
  const planBoxes = await Promise.all(planButtons.map(node => cdp.send('DOM.getBoxModel', { nodeId: node.nodeId }, sessionId)));
  assert.equal(new Set(planBoxes.map(box => box.model.content[1])).size, 1, '套餐按钮在同一行');
  const password = (await nodes()).find(node => node.nodeName === 'INPUT' && node.attributes?.includes('输入所选银行卡的安全码'));
  await cdp.send('DOM.focus', { nodeId: password.nodeId }, sessionId);
  await cdp.send('Input.insertText', { text: '123' }, sessionId);
  assert.equal((await detailField('安全码')).value, '123');
  async function buttonPoint(label) {
    const node = (await nodes()).find(node => node.nodeName === 'BUTTON' && (text(node) === label || node.attributes?.includes(label)));
    assert.ok(node, label);
    await cdp.send('DOM.scrollIntoViewIfNeeded', { nodeId: node.nodeId }, sessionId);
    const { model } = await cdp.send('DOM.getBoxModel', { nodeId: node.nodeId }, sessionId);
    const x = (model.content[0] + model.content[2]) / 2, y = (model.content[1] + model.content[5]) / 2;
    return { x, y };
  }
  async function click(label) {
    const { x, y } = await buttonPoint(label);
    await cdp.send('Input.dispatchMouseEvent', { type: 'mousePressed', x, y, button: 'left', clickCount: 1 }, sessionId);
    await cdp.send('Input.dispatchMouseEvent', { type: 'mouseReleased', x, y, button: 'left', clickCount: 1 }, sessionId);
  }
  await cdp.send('Browser.grantPermissions', { origin: 'https://chatgpt.com', permissions: ['clipboardReadWrite', 'clipboardSanitizedWrite'] });
  const clipboard = async () => (await cdp.send('Runtime.evaluate', { expression: 'navigator.clipboard.readText()', awaitPromise: true, returnByValue: true }, sessionId)).result.value;
  for (const [name, value] of [['完整卡号', '4242424242424242'], ['街道地址', '100 Test Road'], ['安全码', '123'], ['卡平台', card.platform], ['备注', card.notes]]) {
    await click('复制' + name);
    await wait(async () => (await nodes()).some(node => text(node) === name + '已复制'));
    assert.equal(await clipboard(), value, name + '单独复制');
  }
  await click('填充全部表单');
  await wait(() => evaluate('document.getElementById("cc-number").value === "4242424242424242"'));
  assert.equal(await evaluate('document.getElementById("cc-csc").value'), '123');
  assert.equal(await evaluate('document.getElementById("address-level2").value'), 'Portland');
  assert.equal(await evaluate('Boolean(window.submitted)'), false);
  assert.equal(detailReads, 2);
  assert.equal((await detailField('安全码')).value, '未填写', '填充后清空详情安全码');
  const screenshot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'assistant.png'), Buffer.from(screenshot.data, 'base64'));
  const currentInput = (await nodes()).find(node => node.nodeName === 'INPUT' && node.attributes?.includes('输入所选银行卡的安全码'));
  await cdp.send('DOM.focus', { nodeId: currentInput.nodeId }, sessionId);
  await cdp.send('Input.insertText', { text: '123' }, sessionId);
  await click('切换下一张卡');
  await wait(async () => (await nodes()).some(node => text(node) === '5555555555554444'));
  assert.ok((await nodes()).some(node => text(node) === 'SECOND USER'));
  assert.equal((await detailField('完整卡号')).value, '5555555555554444', '切卡后助手详情不残留上一张卡号');
  assert.equal((await detailField('安全码')).value, '未填写', '切卡后清空详情安全码');
  const refreshedInput = (await nodes()).find(node => node.nodeName === 'INPUT' && node.attributes?.includes('输入所选银行卡的安全码'));
  const { object } = await cdp.send('DOM.resolveNode', { nodeId: refreshedInput.nodeId }, sessionId);
  const cvcValue = await cdp.send('Runtime.callFunctionOn', { objectId: object.objectId, functionDeclaration: 'function(){return this.value}', returnByValue: true }, sessionId);
  assert.equal(cvcValue.result.value, '', '切卡后清空安全码');
  await click('切换下一张卡');
  await wait(async () => (await nodes()).some(node => text(node) === '378282246310005'));
  assert.equal((await detailField('测试安全码')).value, '1234', '明确演示卡显示测试安全码');
  await click('复制测试安全码');
  await wait(async () => (await nodes()).some(node => text(node) === '测试安全码已复制'));
  assert.equal(await clipboard(), '1234');
  await click('收起'); assert.equal(await evaluate('document.querySelector("#aitok-assistant").classList.contains("compact")'), true);
  const beforeDrag = await bounds(), point = await buttonPoint('账号助手');
  await cdp.send('Input.dispatchMouseEvent', { type: 'mousePressed', ...point, button: 'left', buttons: 1, clickCount: 1 }, sessionId);
  await cdp.send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: point.x - 200, y: point.y + 120, button: 'left', buttons: 1 }, sessionId);
  await cdp.send('Input.dispatchMouseEvent', { type: 'mouseReleased', x: point.x - 200, y: point.y + 120, button: 'left', clickCount: 1 }, sessionId);
  const afterDrag = await bounds();
  assert.ok(Math.abs(afterDrag.left - (beforeDrag.left - 200)) < 2 && Math.abs(afterDrag.top - (beforeDrag.top + 120)) < 2, '拖拽移动位置：' + JSON.stringify({ beforeDrag, afterDrag, point }));
  assert.equal(await evaluate('document.querySelector("#aitok-assistant").classList.contains("compact")'), true, '拖拽结束不误触展开');
  const draggedShot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'assistant-dragged.png'), Buffer.from(draggedShot.data, 'base64'));
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 390, height: 600, deviceScaleFactor: 1, mobile: false }, sessionId);
  await wait(async () => (await bounds()).right <= 378);
  await click('账号助手');
  const expanded = await bounds();
  assert.ok(expanded.left >= 12 && expanded.top >= 12 && expanded.right <= 378 && expanded.bottom <= 588, '窗口缩小及展开后仍在可视区域');
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 1200, height: 1100, deviceScaleFactor: 1, mobile: false }, sessionId);
  await cdp.send('Page.reload', {}, sessionId);
  await wait(() => evaluate('Boolean(document.getElementById("aitok-assistant"))'));
  await cdp.send('Page.navigate', { url: 'https://example.com/' }, sessionId);
  await wait(() => evaluate('location.hostname === "example.com" && document.readyState === "complete"'));
  assert.equal(await evaluate('Boolean(document.getElementById("aitok-assistant"))'), false);
  await assert.rejects(assistant.fill(assistant.pages.get(targetId), { card_id: 1, address_id: 1, cvc: '123' }), /官方收银/);
  t.diagnostic(`助手截图：${join(directory, 'assistant.png')}`);
});

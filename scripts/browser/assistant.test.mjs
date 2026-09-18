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

test('真实 Chromium 助手隔离、右侧展示、拖拽、手动填充与页面刷新', { skip: !process.env.AITOK_BROWSER_SMOKE, timeout: 60000 }, async t => {
  const card = { id: 1, label: '工作卡', brand: 'Visa', last4: '4242', cardholder: 'TEST USER', exp_month: 12, exp_year: 2035, platform: '自定义卡平台', notes: '月度订阅\n仅工作用途' };
  const secondCard = { id: 2, label: '备用卡', brand: 'Mastercard', last4: '4444', cardholder: 'SECOND USER', exp_month: 10, exp_year: 2036 };
  const demoCard = { id: 3, label: '演示 Amex（测试卡）', brand: 'Amex', last4: '0005', cardholder: 'TEST USER', exp_month: 12, exp_year: 2030 };
  const address = { id: 1, full_name: 'TEST USER', address_line1: '100 Test Road', address_line2: 'Unit 2', city: 'Portland', state: 'OR', postal_code: '97201', country: 'US' };
  let detailReads = 0;
  const sessionWrites = [];
  let sessionEmail = 'test@example.com', sessionStatus = 200, saveStatus = 200;
  let accountNotes = '账号续费备注\n<img src=x onerror=alert(1)>';
  const secondAddress = { ...address, id: 2, address_line1: '200 Bound Street', city: 'Salem', postal_code: '97301' };
  let billingAddressID = null, boundAddressAvailable = true;
  let paymentCardID = null, boundCardAvailable = true;
  const server = http.createServer(async (request, response) => {
    assert.equal(request.headers.authorization, 'Bearer limited-test-token');
    response.setHeader('Content-Type', 'application/json');
    if (request.url === '/api/browser-assistant/session') {
      assert.equal(request.method, 'POST');
      let body = ''; for await (const chunk of request) body += chunk;
      sessionWrites.push(JSON.parse(body));
      response.writeHead(saveStatus);
      response.end(JSON.stringify(saveStatus === 200 ? { message: 'Session 已更新' } : { error: '助手授权已过期' }));
      return;
    }
    if (request.url.endsWith('/cards/1')) { detailReads++; response.end(JSON.stringify({ card: { ...card, number: '4242424242424242' } })); }
    else if (request.url.endsWith('/cards/2')) { detailReads++; response.end(JSON.stringify({ card: { ...secondCard, number: '5555555555554444', cvc: '0042' } })); }
    else if (request.url.endsWith('/cards/3')) { detailReads++; response.end(JSON.stringify({ card: { ...demoCard, number: '378282246310005' } })); }
    else response.end(JSON.stringify({ cards: [card, secondCard, demoCard].filter(item => boundCardAvailable || item.id !== paymentCardID), addresses: [address, secondAddress].filter(item => boundAddressAvailable || item.id !== billingAddressID), payment_card_id: paymentCardID, billing_address_id: billingAddressID, notes: accountNotes }));
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
  const fixture = '<html><head><meta charset="utf-8"></head><body style="font:16px system-ui;background:#f5f6fa;padding:40px 330px 40px 40px"><div id="settings-modal" role="dialog">官网设置弹框</div><script>window.outsideEvents=[]; for(const name of ["pointerdown","mousedown","click","focusin"]) document.addEventListener(name,event=>{if(window.trackOutside && !document.getElementById("settings-modal").contains(event.target)){window.outsideEvents.push(name);document.getElementById("settings-modal").hidden=true;}},true);</script><h1>收银表单测试</h1><form onsubmit="event.preventDefault();window.submitted=true">' + fields.map(name => `<label style="display:block;margin:12px">${name}<input style="display:block;padding:10px" autocomplete="${name}" id="${name}"></label>`).join('') + '<button>付款</button></form></body></html>';
  let verifying = true;
  const challengeFixture = '<html><body><form id="challenge-form">模拟验证页</form></body></html>';
  cdp.on('message', message => {
    if (message.method !== 'Fetch.requestPaused') return;
    const sessionRequest = message.params.request.url === 'https://chatgpt.com/api/auth/session';
    const body = sessionRequest ? JSON.stringify({ accessToken: 'fresh-assistant-token', user: { email: sessionEmail }, expires: '2099-01-01T00:00:00Z', unrelated: 'not-exported' }) : verifying ? challengeFixture : fixture;
    void cdp.send('Fetch.fulfillRequest', { requestId: message.params.requestId, responseCode: sessionRequest ? sessionStatus : 200, responseHeaders: [{ name: 'Content-Type', value: sessionRequest ? 'application/json' : 'text/html' }], body: Buffer.from(body).toString('base64') }, message.sessionId);
  });
  await cdp.send('Page.navigate', { url: 'https://chatgpt.com/' }, sessionId);
  const evaluate = async expression => (await cdp.send('Runtime.evaluate', { expression, returnByValue: true }, sessionId)).result.value;
  const wait = async check => { for (let i = 0; i < 100; i++) { if (await check()) return; await delay(80); } assert.fail('助手页面等待超时'); };
  await wait(() => evaluate('Boolean(document.querySelector("#challenge-form")) && document.readyState === "complete"'));
  await assistant.sync((await cdp.send('Target.getTargets')).targetInfos);
  assert.equal(await evaluate('Boolean(document.getElementById("aitok-assistant"))'), false);
  assert.equal(detailReads, 0, '验证页不加载银行卡详情');
  verifying = false;
  await cdp.send('Page.reload', {}, sessionId);
  await wait(() => evaluate('Boolean(document.getElementById("aitok-assistant"))'));
  assert.equal(await evaluate('typeof window.aitokAssistant'), 'undefined');
  assert.equal(await evaluate('document.querySelector("#aitok-assistant").shadowRoot'), null);
  async function nodes() {
    const { root } = await cdp.send('DOM.getDocument', { depth: -1, pierce: true }, sessionId);
    const result = []; const walk = node => { result.push(node); for (const child of [...(node.children || []), ...(node.shadowRoots || []), ...(node.contentDocument ? [node.contentDocument] : [])]) walk(child); }; walk(root); return result;
  }
  const text = node => (node.children || []).map(child => child.nodeValue || '').join('');
  const hasCardDetails = async number => (await nodes()).some(node => node.nodeName === 'SPAN' && node.attributes?.includes('field-value') && text(node) === number);
  await wait(async () => (await nodes()).some(node => text(node).includes('已读取 3 张银行卡')));
  const bounds = () => evaluate('document.querySelector("#aitok-assistant").getBoundingClientRect().toJSON()');
  assert.ok(Math.abs((await bounds()).right - 1188) <= 1, '助手默认靠右');
  assert.equal(detailReads, 1);
  assert.ok((await nodes()).some(node => text(node) === '4242424242424242'));
  assert.ok((await nodes()).some(node => text(node) === '0.0.5'));
  const notesNode = (await nodes()).find(node => node.attributes?.includes('account-notes'));
  assert.equal(text(notesNode), accountNotes);
  assert.ok(notesNode.children.every(node => node.nodeName === '#text'), '账号备注按纯文本展示');
  assert.ok((await nodes()).some(node => text(node) === '完整卡号'));
  assert.ok((await nodes()).some(node => text(node) === '账单姓名'));
  assert.ok(!(await nodes()).some(node => /删除登录状态|登录其他新账号|待充值队列/.test(node.nodeValue || '')));
  const detailField = async name => {
    const snapshot = await nodes();
    const title = snapshot.find(node => node.nodeName === 'DT' && text(node) === name);
    assert.ok(title, name);
    const row = snapshot.find(node => node.children?.some(child => child.backendNodeId === title.backendNodeId));
    const dd = row.children.find(node => node.nodeName === 'DD');
    return { value: text(dd.children.find(node => node.nodeName === 'SPAN')), button: dd.children.find(node => node.nodeName === 'BUTTON') };
  };
  assert.equal((await detailField('安全码')).value, '未填写', '普通卡不推断安全码');
  assert.ok((await detailField('安全码')).button.attributes.includes('disabled'));
  const planButtons = (await nodes()).find(node => node.nodeName === 'DIV' && node.attributes?.includes('plans')).children;
  assert.deepEqual(planButtons.map(text), ['Plus', '5X', '20X']);
  const planBoxes = await Promise.all(planButtons.map(node => cdp.send('DOM.getBoxModel', { nodeId: node.nodeId }, sessionId)));
  assert.equal(new Set(planBoxes.map(box => box.model.content[1])).size, 1, '套餐按钮在同一行');
  await evaluate('window.trackOutside=true');
  const hasCVCInput = async () => (await nodes()).some(node => node.nodeName === 'INPUT' && node.attributes?.includes('输入所选银行卡的安全码'));
  assert.equal(await hasCVCInput(), false, '未保存安全码时也不显示临时输入框');
  async function buttonPoint(label) {
    await wait(async () => { const button = (await nodes()).find(node => node.nodeName === 'BUTTON' && (text(node) === label || node.attributes?.includes(label))); return button && !button.attributes?.includes('disabled'); });
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
  async function choose(index, label) {
    const picker = (await nodes()).filter(node => node.nodeName === 'DETAILS')[index];
    assert.ok(!picker.attributes?.includes('hidden'), '未绑定时允许选择');
    const summary = picker.children.find(node => node.nodeName === 'SUMMARY');
    await cdp.send('DOM.scrollIntoViewIfNeeded', {nodeId: summary.nodeId}, sessionId);
    const {model} = await cdp.send('DOM.getBoxModel', {nodeId: summary.nodeId}, sessionId);
    const x = (model.content[0] + model.content[2]) / 2, y = (model.content[1] + model.content[5]) / 2;
    await evaluate('window.trackOutside=true');
    await cdp.send('Input.dispatchMouseEvent', {type: 'mousePressed', x, y, button: 'left', clickCount: 1}, sessionId);
    await cdp.send('Input.dispatchMouseEvent', {type: 'mouseReleased', x, y, button: 'left', clickCount: 1}, sessionId);
    await click(label);
    assert.deepEqual(await evaluate('window.outsideEvents'), [], '展开下拉和选项点击不传递到官网');
    await evaluate('window.trackOutside=false');
  }
  async function reloadPanel() {
    await cdp.send('Page.reload', {}, sessionId);
    await wait(async () => (await nodes()).some(node => text(node).startsWith('已读取 ')));
  }
  async function pickerHidden(index) {
    return (await nodes()).filter(node => node.nodeName === 'DETAILS')[index]?.attributes.includes('hidden');
  }
  accountNotes = '';
  await reloadPanel();
  assert.equal(text((await nodes()).find(node => node.attributes?.includes('account-notes'))), '未填写');
  accountNotes = '新的账号备注';
  await reloadPanel();
  assert.equal(text((await nodes()).find(node => node.attributes?.includes('account-notes'))), accountNotes);
  await cdp.send('Storage.setCookies', { cookies: [
    { name: '__Secure-next-auth.session-token', value: 'fresh-login-cookie', domain: '.chatgpt.com', path: '/', secure: true, httpOnly: true },
    { name: 'unrelated-cookie', value: 'not-exported', domain: '.chatgpt.com', path: '/', secure: true },
    { name: '__Secure-next-auth.session-token', value: 'other-site-cookie', domain: '.example.com', path: '/', secure: true, httpOnly: true },
  ] });
  await click('更新 Session');
  await wait(async () => (await nodes()).some(node => text(node) === 'Session 已更新并保存到后台'));
  assert.equal(sessionWrites.length, 1);
  const savedSession = JSON.parse(sessionWrites[0].session_json);
  assert.equal(savedSession.accessToken, 'fresh-assistant-token');
  assert.equal(savedSession.user.email, 'test@example.com');
  assert.equal(savedSession.cookies.length, 1);
  assert.equal(savedSession.cookies[0].value, 'fresh-login-cookie');
  assert.equal(savedSession.unrelated, undefined);
  assert.deepEqual(env.session, savedSession);
  assert.ok(!(await nodes()).some(node => /fresh-assistant-token|fresh-login-cookie/.test(node.nodeValue || '')), '面板不接收或显示 Session 凭据');
  sessionEmail = 'wrong@example.com';
  await click('更新 Session');
  await wait(async () => (await nodes()).some(node => text(node).includes('当前登录邮箱与此账号不一致')));
  assert.equal(sessionWrites.length, 1, '账号不匹配时不上传凭据');
  sessionEmail = 'test@example.com'; sessionStatus = 401;
  await click('更新 Session');
  await wait(async () => (await nodes()).some(node => text(node).includes('读取 Session 失败')));
  assert.equal(sessionWrites.length, 1);
  sessionStatus = 200; saveStatus = 401;
  const previousSession = env.session;
  await click('更新 Session');
  await wait(async () => (await nodes()).some(node => text(node).includes('助手授权已过期')));
  assert.equal(env.session, previousSession, '保存失败保留本机已有 Session');
  saveStatus = 200;
  await cdp.send('Browser.grantPermissions', { origin: 'https://chatgpt.com', permissions: ['clipboardReadWrite', 'clipboardSanitizedWrite'] });
  const clipboard = async () => (await cdp.send('Runtime.evaluate', { expression: 'navigator.clipboard.readText()', awaitPromise: true, returnByValue: true }, sessionId)).result.value;
  for (const [name, value] of [['完整卡号', '4242424242424242'], ['街道地址', '100 Test Road'], ['卡平台', card.platform], ['备注', card.notes]]) {
    await click('复制' + name);
    await wait(async () => (await nodes()).some(node => text(node) === name + '已复制'));
    assert.equal(await clipboard(), value, name + '单独复制');
  }
  await click('复制卡号和地址');
  await wait(async () => (await nodes()).some(node => text(node) === '卡号和地址已复制'));
  assert.equal(await clipboard(), '银行卡\n持卡人：TEST USER\n卡号：4242424242424242\n有效期：12/2035\n\n账单地址\n账单姓名：TEST USER\n街道地址：100 Test Road\n公寓 / 房间：Unit 2\n城市：Portland\n州 / 省：OR\n邮编：97201\n国家：US');
  assert.deepEqual(await evaluate('window.outsideEvents'), [], '助手复制不会触发官网捕获阶段的外部点击或焦点处理');
  assert.equal(await evaluate('document.getElementById("settings-modal").hidden'), false);
  assert.equal(await evaluate('Array.from(window.frames).some(frame => Boolean(frame.document.body.shadowRoot))'), false, '内嵌文档不能暴露敏感详情');
  await evaluate('window.trackOutside=false');
  const readsBeforeFill = detailReads;
  await click('填充全部表单');
  await wait(() => evaluate('document.getElementById("cc-number").value === "4242424242424242"'));
  assert.equal(await evaluate('document.getElementById("cc-csc").value'), '', '缺少安全码仍能填充其他字段');
  await wait(async () => (await nodes()).some(node => text(node).includes('请在官网手动填写')));
  assert.equal(await evaluate('document.getElementById("address-level2").value'), 'Portland');
  assert.equal(await evaluate('Boolean(window.submitted)'), false);
  assert.equal(detailReads, readsBeforeFill + 1);
  assert.equal((await detailField('安全码')).value, '未填写', '缺少安全码时不推断或伪造');
  const screenshot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'assistant.png'), Buffer.from(screenshot.data, 'base64'));
  await evaluate('document.getElementById("cc-csc").value="567"');
  const readsBeforeManual = detailReads;
  await click('填充全部表单');
  await wait(() => detailReads > readsBeforeManual);
  await wait(async () => !(await nodes()).find(node => node.nodeName === 'BUTTON' && text(node) === '填充全部表单').attributes.includes('disabled'));
  assert.equal(await evaluate('document.getElementById("cc-csc").value'), '567', '不覆盖用户在官网手动填写的安全码');
  await choose(0, '备用卡 · Mastercard •••• 4444');
  await wait(async () => (await nodes()).some(node => text(node) === '5555555555554444'));
  assert.ok((await nodes()).some(node => text(node) === 'SECOND USER'));
  assert.equal((await detailField('完整卡号')).value, '5555555555554444', '切卡后助手详情不残留上一张卡号');
  assert.equal((await detailField('安全码')).value, '0042', '切卡后展示所选卡保存的安全码，保留前导零');
  await click('复制安全码');
  assert.equal(await clipboard(), '0042');
  await click('填充全部表单');
  await wait(() => evaluate('document.getElementById("cc-csc").value === "0042"'));
  assert.equal((await detailField('安全码')).value, '0042', '填充后仍展示保存的安全码');
  assert.equal(await hasCVCInput(), false, '已保存安全码的卡仍不显示临时输入框');

  await choose(0, '演示 Amex（测试卡） · Amex •••• 0005');
  await wait(async () => (await nodes()).some(node => text(node) === '378282246310005'));
  assert.equal((await detailField('测试安全码')).value, '1234', '明确演示卡显示测试安全码');
  await click('复制测试安全码');
  await wait(async () => (await nodes()).some(node => text(node) === '测试安全码已复制'));
  assert.equal(await clipboard(), '1234');
  await choose(1, '200 Bound Street, Salem, OR 97301');
  assert.equal((await detailField('街道地址')).value, '200 Bound Street', '未绑定时可以手动选地址');
  billingAddressID = secondAddress.id;
  await reloadPanel();
  assert.equal(await pickerHidden(0), false, '仅绑定地址时卡片仍可选择');
  assert.equal(await pickerHidden(1), true, '已绑定地址隐藏选择器');
  assert.equal((await detailField('街道地址')).value, '200 Bound Street');
  await click('复制卡号和地址');
  await wait(async () => (await clipboard()).includes('街道地址：200 Bound Street'));
  boundAddressAvailable = false;
  await reloadPanel();
  assert.ok((await nodes()).some(node => text(node).includes('账号绑定的地址不可用')));
  assert.equal(await pickerHidden(1), true, '绑定地址失效也不允许选择其他地址');
  for (const name of ['复制卡号和地址', '填充全部表单']) assert.ok((await nodes()).find(node => node.nodeName === 'BUTTON' && text(node) === name).attributes.includes('disabled'));
  billingAddressID = null;
  paymentCardID = secondCard.id;
  await reloadPanel();
  assert.equal(await pickerHidden(0), true, '已绑定卡片隐藏选择器');
  assert.equal(await pickerHidden(1), false, '仅绑定卡片时地址仍可选择');
  assert.equal((await detailField('名称')).value, '备用卡');
  billingAddressID = secondAddress.id;
  boundAddressAvailable = true;
  await reloadPanel();
  assert.equal(await pickerHidden(0), true);
  assert.equal(await pickerHidden(1), true);
  assert.equal((await detailField('街道地址')).value, '200 Bound Street');
  assert.ok(!(await nodes()).some(node => node.nodeName === 'BUTTON' && ['切换账单地址', '切换下一张卡', '刷新银行卡和地址'].includes(text(node))));
  await assert.rejects(assistant.fill(assistant.pages.get(targetId), {card_id: 1, address_id: 2, cvc: '123'}), /绑定已更新/);
  await assert.rejects(assistant.fill(assistant.pages.get(targetId), {card_id: 2, address_id: 1, cvc: '123'}), /绑定已更新/);
  boundCardAvailable = false;
  const readsBeforeUnavailable = detailReads;
  await reloadPanel();
  assert.ok((await nodes()).some(node => text(node).includes('账号绑定的付款卡不可用')));
  assert.equal(detailReads, readsBeforeUnavailable, '绑定卡不可用时不自动读取其他卡片');
  assert.equal(await hasCardDetails('5555555555554444'), false, '清除旧卡详情');
  assert.equal(await pickerHidden(0), true);
  paymentCardID = null;
  await reloadPanel();
  assert.equal(await pickerHidden(0), false);
  assert.equal(await hasCardDetails('4242424242424242'), true);
  boundCardAvailable = true;
  paymentCardID = secondCard.id;
  await click('收起'); assert.equal(await evaluate('document.querySelector("#aitok-assistant").classList.contains("compact")'), true);
  const beforeDrag = await bounds(), point = await buttonPoint('账号助手');
  assert.ok(beforeDrag.width < 150, '收起后仅保留紧凑的按钮');
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
  await wait(() => hasCardDetails('5555555555554444'));
  assert.equal((await detailField('名称')).value, '备用卡', '重新打开页面时默认展示账号绑定卡');
  await evaluate('document.body.insertAdjacentHTML("beforeend", \'<form id="challenge-form">模拟验证页</form>\')');
  await wait(() => evaluate('!document.getElementById("aitok-assistant")'));
  await evaluate('document.getElementById("challenge-form").remove()');
  await wait(() => evaluate('Boolean(document.getElementById("aitok-assistant"))'));
  await cdp.send('Page.navigate', { url: 'https://example.com/' }, sessionId);
  await wait(() => evaluate('location.hostname === "example.com" && document.readyState === "complete"'));
  assert.equal(await evaluate('Boolean(document.getElementById("aitok-assistant"))'), false);
  await assert.rejects(assistant.fill(assistant.pages.get(targetId), { card_id: 1, address_id: 1, cvc: '123' }), /官方收银/);
  t.diagnostic(`助手截图：${join(directory, 'assistant.png')}`);
});

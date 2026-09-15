import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import { readFile, mkdtemp, writeFile } from 'node:fs/promises';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { tmpdir } from 'node:os';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { setTimeout as delay } from 'node:timers/promises';
import { findChrome } from './session.mjs';
import { CDP } from './cdp.mjs';

// 使用构建后的真实页面和模拟 API，不读取用户账号或连接真实 ChatGPT。
test('账号导入、后台管理与本机打开页面桌面、移动端冒烟测试', { skip: !process.env.AITOK_BROWSER_SMOKE, timeout: 90000 }, async t => {
  const directory = await mkdtemp(join(tmpdir(), 'aitok-ui-smoke-'));
  const dist = join(dirname(fileURLToPath(import.meta.url)), '../../frontend/dist');
  const server = http.createServer(async (request, response) => {
    const path = new URL(request.url, 'http://local').pathname;
    const file = path.startsWith('/assets/') ? join(dist, path) : join(dist, 'index.html');
    try {
      const body = await readFile(file);
      response.setHeader('Content-Type', file.endsWith('.js') ? 'application/javascript' : file.endsWith('.css') ? 'text/css' : 'text/html');
      response.end(body);
    } catch { response.writeHead(404); response.end(); }
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  t.after(() => { server.closeAllConnections(); server.close(); });
  const child = spawn(await findChrome(), [`--user-data-dir=${join(directory, 'profile')}`, '--headless=new', '--remote-debugging-pipe', '--no-first-run', '--no-default-browser-check', '--disable-background-networking', 'about:blank'], { stdio: ['ignore', 'ignore', 'ignore', 'pipe', 'pipe'] });
  t.after(() => child.kill());
  const cdp = new CDP(child);
  const user = { id: 3, username: 'admin', role: 'super_admin' };
  const account = { id: 1, user_id: 1, label: '工作账号', email: 'chat@example.com', has_session: true };
  const errors = [], imported = [], launches = [], requests = [];
  const localLaunches = [], localRequests = [];
  let localState = 'closed', exportCount = 0, exportExpired = false;
  let proxies = [], bindings = {}, testCount = 0, draftFails = false, proxyPassword = '', accountDeletes = 0;
  let browserState = 'closed';
  let groups = [], loginWrites = 0, authenticatedAt;
  let savedProxy = '';
  let addressRows = Array.from({ length: 21 }, (_, index) => ({ id: index + 1, address_line1: `${index + 1} Test Street`, address_line2: '', city: 'Portland', state: 'OR', postal_code: '97201', country: 'US', source_url: 'https://www.meiguodizhi.com/usa-address/oregon', source_data: { Full_Name: 'Test User', Occupation: 'Engineer', Extra_Field: 'Preserved value', CVV2: '123' }, can_edit: true }));
  const addressWrites = [];
  let bankCards = [];
  const bankWrites = [];
  const cardLedger = [], ledgerWrites = [];
  let ledgerResponseLost = false;
  const userRows = [{ id: 3, username: 'admin', email: '', role: 'super_admin', created_at: '2026-09-01T00:00:00Z' }, { id: 1, email: 'member@example.com', role: '', created_at: '2026-09-02T00:00:00Z' }];
  const roleWrites = [];
  const { targetId } = await cdp.send('Target.createTarget', { url: 'about:blank' });
  const { sessionId } = await cdp.send('Target.attachToTarget', { targetId, flatten: true });
  await cdp.send('Network.enable', {}, sessionId);
  await cdp.send('Network.setBlockedURLs', { urls: ['https://fonts.googleapis.com/*', 'https://fonts.gstatic.com/*'] }, sessionId);
  await cdp.send('Runtime.enable', {}, sessionId);
  await cdp.send('Page.enable', {}, sessionId);
  await cdp.send('Fetch.enable', { patterns: [{ urlPattern: '*://*/api/*' }, { urlPattern: 'http://127.0.0.1:15683/*' }] }, sessionId);
  cdp.on('message', message => {
    if (message.method === 'Runtime.exceptionThrown') errors.push(message.params.exceptionDetails.text);
    if (message.method !== 'Fetch.requestPaused') return;
    (async () => {
      const { requestId, request } = message.params;
      const url = new URL(request.url);
      requests.push(request.url);
      let data = {};
      let responseCode = request.method === 'OPTIONS' ? 204 : 200;
      if (url.origin === 'http://127.0.0.1:15683') {
        localRequests.push(request);
        if (url.pathname === '/health') data = { status: 'ok', version: 2 };
        else if (url.pathname === '/proxies/parse') data = { items: [{ line: 1, proxy: { host: '203.0.113.10', port: 1080, username: 'proxy-user', password: 'proxy-secret', name: 'Imported' } }] };
        else if (url.pathname === '/proxy-history') data = { records: [{ id: 'usage-1', action: 'open', ok: true, email: account.email, created_at: '2026-09-15T00:30:00Z' }], total: 1, page: 1, page_size: 20 };
        else if (url.pathname.startsWith('/proxies')) {
          const input = request.postData ? JSON.parse(request.postData) : {};
          if (request.method === 'POST' && url.pathname === '/proxies') { proxyPassword = input.password; proxies.push({ ...input, password: undefined, has_password: Boolean(input.password), id: 'proxy-1' }); }
          if (request.method === 'PATCH') { proxyPassword = input.password; proxies[0] = { ...proxies[0], ...input, password: undefined }; }
          if (request.method === 'POST' && url.pathname !== '/proxies/test' && url.pathname.endsWith('/test')) {
            const matches = ++testCount % 2 === 1;
            proxies[0].last_test = { ok: true, matches, exit_ip: matches ? '203.0.113.10' : '198.51.100.1', proxy_ips: ['203.0.113.10'], latency_ms: 25, tested_at: new Date().toISOString() };
          }
          if (request.method === 'DELETE') {
            if (Object.values(bindings).includes('proxy-1')) { responseCode = 400; data = { error: '此代理仍有账号使用' }; }
            else proxies = [];
          }
          if (responseCode === 200 || responseCode === 204) data = { proxies, bindings };
          if (request.method === 'GET' && url.pathname === '/proxies/proxy-1') data = { ...proxies[0], password: proxyPassword };
          if (request.method === 'POST' && url.pathname === '/proxies/test') data = { result: draftFails ? { ok: false, error: '代理认证失败' } : { ok: true, matches: true, exit_ip: '203.0.113.10' }, test_token: draftFails ? null : 'draft-token' };
        } else if (url.pathname.endsWith('/proxy')) {
          if (request.method === 'PATCH') {
            const id = decodeURIComponent(url.pathname.split('/')[2]);
            const { proxy_id } = JSON.parse(request.postData);
            if (proxy_id) bindings[id] = proxy_id; else delete bindings[id];
          }
          data = { proxies, bindings };
        } else {
          if (request.method === 'POST') { localLaunches.push(JSON.parse(request.postData)); localState = 'opened'; }
          if (request.method === 'DELETE') localState = 'closed';
          data = { state: localState, authenticated_at: authenticatedAt };
        }
      } else if (url.pathname.startsWith('/api/users')) {
        if (request.method === 'PATCH') { const input = JSON.parse(request.postData); roleWrites.push(input); userRows[1].role = input.role; data = { role: input.role }; }
        else data = { users: userRows, total: userRows.length, page: 1, page_size: 20 };
      } else if (url.pathname.startsWith('/api/account-groups')) {
        const input = request.postData ? JSON.parse(request.postData) : {};
        if (request.method === 'POST') groups.push({ id: 10, user_id: input.user_id, name: input.name, account_count: 0 });
        if (request.method === 'PATCH') groups[0].name = input.name;
        if (request.method === 'DELETE') { groups = []; account.group_id = null; }
        data = request.method === 'POST' ? { group: groups.at(-1) } : { groups };
      } else if (url.pathname === '/api/accounts/1/group') {
        if (request.method === 'PATCH') account.group_id = JSON.parse(request.postData).group_id;
        groups.forEach(group => { group.account_count = group.id === account.group_id ? 1 : 0; });
        data = { group_id: account.group_id };
      } else if (url.pathname === '/api/accounts/1/login') {
        if (request.method === 'POST') { loginWrites++; account.last_login_at = JSON.parse(request.postData).logged_in_at; }
        data = { last_login_at: account.last_login_at };
      } else if (/^\/api\/bank-cards\/\d+\/ledger$/.test(url.pathname)) {
        if (request.method === 'POST') {
          const input = JSON.parse(request.postData); ledgerWrites.push(input);
          const existing = cardLedger.find(entry => entry.request_key === input.request_key);
          if (!existing) {
            const amount = Math.round(Number(input.amount_usd) * 100) * (input.kind === 'deposit' ? 1 : -1);
            bankCards[0].balance_usd_minor += amount;
            cardLedger.unshift({ id: cardLedger.length + 1, ...input, kind: input.kind === 'deposit' && !cardLedger.length ? 'opening' : input.kind, amount_usd_minor: amount, balance_after_usd_minor: bankCards[0].balance_usd_minor, account_label: input.account_id ? account.label : '', account_email: input.account_id ? account.email : '', original_php_minor: input.account_id ? 891964 : 0, created_at: '2026-09-15T01:00:00Z' });
          }
          data = { entry: existing || cardLedger[0], replayed: Boolean(existing) };
          if (ledgerResponseLost) { ledgerResponseLost = false; responseCode = 503; data = { error: '模拟响应丢失，请重试' }; }
        } else {
          data = { entries: cardLedger, total: cardLedger.length, balance_usd_minor: bankCards[0].balance_usd_minor, deposited_usd_minor: cardLedger.reduce((sum, entry) => sum + Math.max(0, entry.amount_usd_minor), 0), spent_usd_minor: cardLedger.reduce((sum, entry) => sum - Math.min(0, entry.amount_usd_minor), 0) };
        }
      } else if (url.pathname.startsWith('/api/bank-cards')) {
        const input = request.postData ? JSON.parse(request.postData) : {};
        if (['POST', 'PATCH', 'DELETE'].includes(request.method)) bankWrites.push(request.method);
        if (request.method === 'POST') bankCards.push({ ...input, id: 1, last4: '4242', brand: 'Visa' });
        if (request.method === 'PATCH') bankCards[0] = { ...bankCards[0], ...input };
        if (request.method === 'DELETE') bankCards = [];
        data = url.pathname === '/api/bank-cards/1' ? { card: bankCards[0] } : { cards: bankCards.map(({ number, ...card }) => card), platforms: [...new Set(bankCards.map(card => card.platform).filter(Boolean))], total: bankCards.length, page: 1, page_size: 20 };
      } else if (url.pathname.startsWith('/api/addresses')) {
        const input = request.postData ? JSON.parse(request.postData) : {};
        const id = Number(url.pathname.split('/')[3]);
        if (['POST', 'PATCH', 'DELETE'].includes(request.method)) addressWrites.push(request.method);
        if (request.method === 'POST') addressRows.unshift({ ...input, id: 100, source_url: '', can_edit: true });
        if (request.method === 'PATCH') addressRows = addressRows.map(address => address.id === id ? { ...address, ...input } : address);
        if (request.method === 'DELETE') addressRows = addressRows.filter(address => address.id !== id);
        const rows = addressRows.filter(address => JSON.stringify(address).toLowerCase().includes((url.searchParams.get('q') || '').toLowerCase()));
        const page = Number(url.searchParams.get('page') || 1);
        data = { addresses: rows.slice((page - 1) * 20, page * 20), total: rows.length, page, page_size: 20 };
      } else if (url.pathname === '/api/accounts/1' && request.method === 'DELETE') { accountDeletes++;
      } else if (url.pathname === '/api/accounts/1/browser-session') {
        if (request.method === 'POST') exportCount++;
        if (exportExpired) { responseCode = 422; data = { error: 'Session 已过期，请更新' }; }
        else data = { account_id: 1, session: { accessToken: 'local-test-access', user: { email: account.email } } };
      } else if (url.pathname === '/api/me') data = { user, accounts: [account] };
      else if (url.pathname === '/api/accounts') {
        if (request.method === 'POST') imported.push(JSON.parse(request.postData));
        data = { accounts: [account], account };
      } else if (url.pathname === '/api/wallet') data = { balance: 0, orders: [], ledger: [], renewals: [], renewal_token_cost: 20, renewal_months: 1, topup_options: [{ amount_minor: 100, tokens: 1 }], tokens_per_usd: 1, stripe_enabled: false };
      else if (url.pathname === '/api/accounts/1/browser') {
        if (request.method === 'PATCH' || request.method === 'POST') {
          const input = JSON.parse(request.postData);
          if (input.proxy_url !== undefined) savedProxy = input.proxy_url;
          if (request.method === 'POST') { launches.push(input); browserState = 'opened'; }
        }
        if (request.method === 'DELETE') browserState = 'closed';
        data = { browser: { state: browserState }, settings: { has_proxy: Boolean(savedProxy), proxy_address: savedProxy ? 'socks5://proxy.example:1080' : '' } };
      }
      await cdp.send('Fetch.fulfillRequest', {
        requestId, responseCode,
        responseHeaders: [
          { name: 'Content-Type', value: 'application/json' }, { name: 'Access-Control-Allow-Origin', value: '*' },
          { name: 'Access-Control-Allow-Headers', value: 'Content-Type, Authorization, X-AiTok-Client' },
          { name: 'Access-Control-Allow-Methods', value: 'GET, POST, PATCH, DELETE, OPTIONS' },
          { name: 'Access-Control-Allow-Private-Network', value: 'true' },
        ],
        body: request.method === 'OPTIONS' ? '' : Buffer.from(JSON.stringify(data)).toString('base64'),
      }, message.sessionId);
    })().catch(error => errors.push(error.message));
  });
  await cdp.send('Page.addScriptToEvaluateOnNewDocument', { source: 'localStorage.setItem("token","test-platform-token")' }, sessionId);
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false }, sessionId);
  await cdp.send('Page.navigate', { url: `http://127.0.0.1:${server.address().port}/accounts` }, sessionId);
  const evaluate = async expression => {
    const result = await cdp.send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true }, sessionId);
    if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description || result.exceptionDetails.text);
    return result.result.value;
  };
  const wait = async expression => {
    for (let attempt = 0; attempt < 80; attempt++) {
      try { if (await evaluate(expression)) return; } catch {}
      await delay(100);
    }
    assert.fail(`页面等待超时：${expression}；${await evaluate('document.body.innerText')}`);
  };
  const click = text => evaluate(`Array.from(document.querySelectorAll('button')).find(button => button.textContent === ${JSON.stringify(text)}).click()`);
  const fill = (selector, value, type = 'HTMLInputElement') => evaluate(`(() => { const element = document.querySelector(${JSON.stringify(selector)}); Object.getOwnPropertyDescriptor(${type}.prototype, 'value').set.call(element, ${JSON.stringify(value)}); element.dispatchEvent(new Event('input', { bubbles: true })); })()`);
  const select = async (label, text, query = '', keyboard = false) => {
    await evaluate(`Array.from(document.querySelectorAll('button[role=combobox]')).find(button => button.getAttribute('aria-label') === ${JSON.stringify(label)}).click()`);
    await wait('Boolean(document.querySelector(".select-search input"))');
    if (query) await fill('.select-search input', query);
    await wait(`Array.from(document.querySelectorAll('[role=option]')).some(option => option.textContent === ${JSON.stringify(text)})`);
    if (keyboard) await cdp.send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13 }, sessionId);
    else await evaluate(`Array.from(document.querySelectorAll('[role=option]')).find(option => option.textContent === ${JSON.stringify(text)}).click()`);
    await wait('!document.querySelector(".select-popup")');
  };
  await wait('document.body.innerText.includes("工作账号")');
  await wait('location.pathname === "/admin/accounts"');
  assert.equal(await evaluate('Boolean(document.querySelector("header nav"))'), false);
  assert.deepEqual(await evaluate('Array.from(document.querySelectorAll(".workspace-nav a"), a => a.getAttribute("href"))'), ['/admin/accounts', '/admin/proxies', '/admin/addresses', '/admin/bank-cards', '/admin/users']);
  assert.equal(await evaluate('document.querySelector(".admin-sidebar").getBoundingClientRect().left'), 0);
  assert.equal(await evaluate('document.querySelector(".admin-content").getBoundingClientRect().left'), 208);
  assert.equal(await evaluate('document.querySelector(".account-row").tagName'), 'TR');
  for (const scrollLeft of [0, 300]) {
    await evaluate(`document.querySelector(".data-table-wrap").scrollLeft = ${scrollLeft}`);
    assert.ok(await evaluate('Math.abs(document.querySelector(".accounts-table th.table-actions").getBoundingClientRect().left - document.querySelector(".account-row td.table-actions").getBoundingClientRect().left) < 1'));
  }
  await evaluate('document.querySelector(".data-table-wrap").scrollLeft = 0');
  const sidebarShot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'admin-sidebar.png'), Buffer.from(sidebarShot.data, 'base64'));
  await evaluate('document.querySelector(".workspace-nav a[href$=users]").click()');
  await wait('Boolean(document.querySelector(".user-manager tbody tr"))');
  assert.equal(await evaluate('document.querySelectorAll(".user-manager button[role=combobox]").length'), 1);
  await select('用户 member@example.com 的角色', '管理员');
  await wait('document.querySelector("dialog")?.innerText.includes("确认修改角色")');
  assert.equal(roleWrites.length, 0);
  await click('取消');
  await wait('!document.querySelector("dialog")');
  await select('用户 member@example.com 的角色', '管理员');
  await click('确认修改');
  await wait('!document.querySelector("dialog") && document.querySelector(".user-manager button[role=combobox]").innerText.includes("管理员")');
  assert.deepEqual(roleWrites, [{ role: 'admin' }]);
  const usersShot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'admin-users.png'), Buffer.from(usersShot.data, 'base64'));
  await cdp.send('Page.reload', {}, sessionId);
  await wait('Boolean(document.querySelector(".user-manager tbody tr"))');
  await evaluate('document.querySelector(".workspace-nav a[href$=accounts]").click()');
  await wait('Boolean(document.querySelector(".account-toolbar"))');
  assert.equal(await evaluate('document.querySelectorAll("select").length'), 0);
  assert.equal(await evaluate('document.body.innerText.includes("代币续订")'), false);
  assert.equal(await evaluate('Boolean(document.querySelector(".workspace-nav a[href$=wallet]"))'), false);
  await evaluate('document.querySelector(".user-menu summary").click()');
  await click('白色');
  await wait('document.documentElement.dataset.theme === "light"');
  await click('黑色');
  await wait('document.documentElement.dataset.theme === "dark"');
  await click('自动');
  await cdp.send('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: 'light' }] }, sessionId);
  await wait('document.documentElement.dataset.theme === "light"');
  await cdp.send('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: 'dark' }] }, sessionId);
  await wait('document.documentElement.dataset.theme === "dark"');
  await evaluate('document.querySelector(".user-menu a[href$=wallet]").click()');
  await wait('Boolean(document.querySelector(".wallet-grid"))');
  await evaluate('document.querySelector(".workspace-nav a[href$=accounts]").click()');
  await wait('Boolean(document.querySelector(".account-toolbar"))');
  await evaluate('document.querySelector(".account-group button").click()');
  await wait('Boolean(document.querySelector(".select-create"))');
  await fill('.select-search input', '快速分组');
  await click('＋ 新建分组');
  await wait('Boolean(document.querySelector("dialog input[name=group_name]"))');
  assert.equal(await evaluate('document.querySelector("dialog input[name=group_name]").value'), '快速分组');
  await click('创建并绑定');
  await wait('!document.querySelector("dialog") && document.querySelector(".account-group").innerText.includes("快速分组")');
  assert.equal(account.group_id, 10);
  assert.equal(groups[0].user_id, account.user_id);
  await click('管理分组'); await click('删除'); await click('确认删除');
  await wait('!document.querySelector(".group-row")');
  await evaluate('document.querySelector("dialog button[aria-label=关闭]").click()');
  await click('管理分组');
  await fill('input[name=group_name]', '团队 A');
  await select('分组所属用户', '1');
  await click('添加分组');
  await wait('document.querySelector(".group-row")?.innerText.includes("团队 A")');
  await evaluate('document.querySelector("dialog button[aria-label=关闭]").click()');
  await select('账号 chat@example.com 的分组', '团队 A', '团队', true);
  await wait('document.body.innerText.includes("账号分组已更新")');
  assert.equal(account.group_id, 10);
  await select('筛选账号分组', '未分组 · 0');
  await wait('document.body.innerText.includes("此分组暂无账号")');
  await select('筛选账号分组', '团队 A · 1', '团队');
  await wait('Boolean(document.querySelector(".account-actions"))');
  await click('管理分组');
  await click('编辑');
  await fill('input[name=group_name]', '团队 B');
  await click('保存分组');
  await wait('document.querySelector(".group-row")?.innerText.includes("团队 B")');
  await click('删除');
  await click('取消');
  assert.equal(groups.length, 1);
  await click('删除'); await click('确认删除');
  await wait('!document.querySelector(".group-row")');
  assert.equal(account.group_id, null);
  await evaluate('document.querySelector("dialog button[aria-label=关闭]").click()');
  await select('筛选账号分组', '全部分组 · 1');
  await click('添加账号');
  await wait('Boolean(document.querySelector("dialog textarea"))');
  const raw = JSON.stringify({ user: { email: 'chat@example.com', name: '工作账号' }, accessToken: 'test-only' });
  await fill('textarea', raw, 'HTMLTextAreaElement');
  await wait('document.querySelector("dialog").innerText.includes("识别到账号：chat@example.com")');
  assert.equal(await evaluate('document.querySelector("input[name=email]").required'), false);
  await click('格式化 JSON');
  assert.ok((await evaluate('document.querySelector("textarea").value')).includes('\n'));
  assert.ok(await evaluate('Boolean(document.querySelector(".json-key")) && Boolean(document.querySelector(".json-string"))'));
  await fill('input[name=session_cookie]', 'test-cookie');
  const jsonEditorShot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'json-editor.png'), Buffer.from(jsonEditorShot.data, 'base64'));
  await click('保存');
  await wait('!document.querySelector("dialog")');
  assert.equal(imported.length, 1); assert.deepEqual(JSON.parse(imported[0].session_json), { ...JSON.parse(raw), sessionToken: 'test-cookie' }); assert.equal(imported[0].email, '');
  await evaluate('document.querySelector(".workspace-nav a[href$=addresses]").click()');
  await wait('document.querySelectorAll(".address-row").length === 20');
  assert.equal(await evaluate('Boolean(document.querySelector(".address-row a"))'), false);
  await click('完整资料');
  await wait('Boolean(document.querySelector("dialog .address-source-details[open]"))');
  const addressDetailsShot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'address-details.png'), Buffer.from(addressDetailsShot.data, 'base64'));
  assert.equal(await evaluate('document.querySelector(".address-source-details").innerText.includes("Preserved value")'), true);
  assert.equal(await evaluate('document.querySelector(".address-source-details").innerText.includes("生成安全码")'), true);
  await evaluate('document.querySelector("dialog button[aria-label=关闭]").click()');
  assert.deepEqual(await evaluate('Array.from(document.querySelectorAll(".address-table thead th"), item => item.textContent)'), ['账单姓名', '街道 / 公寓', '城市', '州 / 省', '邮编', '国家 / 地区', '电话 / 邮箱', '操作']);
  assert.equal(await evaluate('document.querySelector(".address-row").innerText.includes("未填写")'), true);
  await click('下一页');
  await wait('document.querySelectorAll(".address-row").length === 1');
  await click('添加地址');
  await fill('dialog input[name=full_name]', 'Test User');
  await fill('dialog input[name=address_line1]', '100 New Road');
  await fill('dialog input[name=city]', 'Salem');
  await fill('dialog input[name=postal_code]', '97301');
  await click('保存地址');
  await wait('!document.querySelector("dialog") && document.querySelector(".address-row")?.innerText.includes("100 New Road")');
  await click('编辑');
  assert.equal(await evaluate('document.querySelector("dialog input[name=full_name]").value'), 'Test User');
  await fill('dialog input[name=full_name]', 'Updated User');
  await fill('dialog input[name=address_line2]', 'Unit 2');
  await click('保存地址');
  await wait('!document.querySelector("dialog") && document.querySelector(".address-row")?.innerText.includes("Unit 2")');
  assert.equal(await evaluate('document.querySelector(".address-row").innerText.includes("Updated User")'), true);
  await fill('input[aria-label=搜索地址]', 'Salem');
  await click('搜索');
  await wait('document.querySelectorAll(".address-row").length === 1');
  await click('删除');
  await click('取消');
  assert.equal(addressWrites.filter(method => method === 'DELETE').length, 0);
  await click('删除');
  await click('确认删除');
  await wait('!document.querySelector("dialog") && document.body.innerText.includes("没有找到地址")');
  assert.deepEqual(addressWrites, ['POST', 'PATCH', 'DELETE']);
  await fill('input[aria-label=搜索地址]', ''); await click('搜索');
  await wait('document.querySelectorAll(".address-row").length === 20');
  const addressShot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'addresses.png'), Buffer.from(addressShot.data, 'base64'));
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true }, sessionId);
  await click('管理菜单');
  await wait('document.querySelector(".admin-sidebar").classList.contains("open")');
  assert.equal(await evaluate('document.querySelector(".admin-sidebar").getBoundingClientRect().left'), 0);
  const sidebarMobile = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'admin-sidebar-mobile.png'), Buffer.from(sidebarMobile.data, 'base64'));
  await cdp.send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Escape', code: 'Escape', windowsVirtualKeyCode: 27 }, sessionId);
  await wait('!document.querySelector(".admin-sidebar").classList.contains("open")');
  assert.equal(await evaluate('document.querySelector(".admin-content").getBoundingClientRect().left'), 0);
  await evaluate('document.querySelector(".address-row").scrollIntoView({block:"center"})');
  assert.equal(await evaluate('document.documentElement.scrollWidth <= innerWidth'), true);
  const addressMobileShot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'addresses-mobile.png'), Buffer.from(addressMobileShot.data, 'base64'));
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false }, sessionId);
  await evaluate('window.scrollTo(0,0)');
  await evaluate('document.querySelector(".workspace-nav a[href$=accounts]").click()');
  await wait('Boolean(document.querySelector(".account-actions"))');
  await click('浏览器管理');
  await wait('Boolean(document.querySelector(".browser-session"))');
  await wait('document.querySelector("dialog").innerText.includes("未打开")');
  assert.equal(await evaluate('document.querySelector("dialog").innerText.includes("配对密钥")'), false);
  await evaluate('document.querySelector(".browser-session button[role=combobox]").click()');
  await wait('Boolean(document.querySelector(".select-popup"))');
  await cdp.send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Escape', code: 'Escape', windowsVirtualKeyCode: 27 }, sessionId);
  await wait('!document.querySelector(".select-popup")');
  assert.equal(await evaluate('Boolean(document.querySelector("dialog"))'), true);
  await select('网络连接', '设置 SOCKS5 代理');
  await wait('Boolean(document.querySelector(".browser-session input[type=password]"))');
  await fill('.browser-session input[type=password]', 'socks5://u:p@proxy.example:1080');
  await click('保存代理');
  await wait('document.querySelector("dialog").innerText.includes("代理配置已加密保存")');
  assert.equal(savedProxy, 'socks5://u:p@proxy.example:1080');
  await click('打开浏览器');
  await wait('document.querySelector(".browser-status-row strong")?.textContent === "已打开"');
  assert.equal(launches.length, 1); assert.deepEqual(launches[0], {});
  assert.ok(!requests.some(url => url.includes('/browser-session')));
  assert.equal(localLaunches.length, 0);
  const desktop = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'desktop.png'), Buffer.from(desktop.data, 'base64'));
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true }, sessionId);
  assert.ok(await evaluate('document.documentElement.scrollWidth <= window.innerWidth'));
  const mobile = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'mobile.png'), Buffer.from(mobile.data, 'base64'));
  await click('关闭浏览器');
  await wait('!document.querySelector("dialog")');

  // 普通用户能打开自己的账号，但不能操作后台桌面或他人的本机凭据。
  user.id = 1; user.role = 'user'; user.username = '';
  await cdp.send('Page.reload', {}, sessionId);
  await wait('document.body.innerText.includes("工作账号")');
  assert.equal(await evaluate('document.body.innerText.includes("浏览器管理")'), false);
  assert.equal(await evaluate('Boolean(document.querySelector(".workspace-nav a[href$=addresses]"))'), true);
  assert.equal(exportCount, 0);
  assert.equal(await evaluate('Boolean(document.querySelector(".workspace-nav a[href$=users]"))'), false);
  await evaluate('document.querySelector(".workspace-nav a[href$=bank-cards]").click()');
  await wait('Boolean(document.querySelector(".bank-card-manager"))');
  await click('添加银行卡');
  await fill('dialog input[name=label]', '工作卡');
  await fill('dialog input[name=cardholder]', 'Test User');
  await fill('dialog input[name=number]', '4242424242424242');
  await evaluate('document.querySelector("button[aria-label=卡平台]").click()');
  await wait('Boolean(document.querySelector(".select-search input"))');
  await fill('.select-search input', '自定义卡平台');
  await click('＋ 使用输入的平台');
  await wait('document.querySelector("input[name=platform]").value === "自定义卡平台"');
  await fill('dialog textarea[name=notes]', '月度订阅\n仅工作用途', 'HTMLTextAreaElement');
  await select('到期年份', '2035', '2035');
  assert.ok(await evaluate('document.documentElement.scrollWidth <= innerWidth'));
  const bankCardShot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'bank-card-platform.png'), Buffer.from(bankCardShot.data, 'base64'));
  await click('保存银行卡');
  await wait('!document.querySelector("dialog") && Boolean(document.querySelector(".bank-card-row"))');
  assert.equal(await evaluate('document.querySelector(".bank-card-row").innerText.includes("4242424242424242")'), false);
  assert.equal(await evaluate('document.querySelector(".bank-card-row").tagName'), 'TR');
  assert.equal(await evaluate('document.documentElement.scrollWidth <= innerWidth'), true);
  assert.equal(bankCards[0].platform, '自定义卡平台');
  assert.equal(bankCards[0].notes, '月度订阅\n仅工作用途');
  assert.ok(await evaluate('document.querySelector(".bank-card-row").innerText.includes("自定义卡平台") && document.querySelector(".bank-card-row").innerText.includes("仅工作用途")'));
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 1600, height: 900, deviceScaleFactor: 1, mobile: false }, sessionId);
  await evaluate('window.scrollTo(0,0)');
  await writeFile(join(directory, 'bank-cards-table.png'), Buffer.from((await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId)).data, 'base64'));
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true }, sessionId);
  await click('编辑');
  await wait('Boolean(document.querySelector("dialog input[name=number]"))');
  assert.equal(await evaluate('document.querySelector("dialog input[name=number]").value'), '4242424242424242');
  assert.equal(await evaluate('document.querySelector("dialog textarea[name=notes]").value'), '月度订阅\n仅工作用途');
  await select('卡平台', '未设置');
  await select('卡平台', '自定义卡平台', '自定义', true);
  await fill('dialog textarea[name=notes]', '已修改备注', 'HTMLTextAreaElement');
  await fill('dialog input[name=label]', '工作卡已编辑');
  await click('保存银行卡');
  await wait('!document.querySelector("dialog") && document.querySelector(".bank-card-row").innerText.includes("工作卡已编辑")');
  assert.equal(bankCards[0].notes, '已修改备注');
  await click('删除'); await click('取消');
  assert.equal(bankWrites.filter(method => method === 'DELETE').length, 0);
  await click('删除'); await click('确认删除');
  await wait('!document.querySelector("dialog") && !document.querySelector(".bank-card-row")');
  assert.deepEqual(bankWrites, ['POST', 'PATCH', 'DELETE']);
  bankCards = [{ id: 2, label: '对账测试卡', last4: '4242', brand: 'Visa', cardholder: 'Test User', exp_month: 12, exp_year: 2030, balance_usd_minor: 0 }];
  await click('刷新');
  await wait('document.querySelector(".bank-card-row")?.innerText.includes("对账测试卡")');
  await click('余额 / 对账单');
  await wait('document.querySelector("dialog")?.innerText.includes("暂无流水")');
  await click('记录存入');
  await fill('input[aria-label="记账金额 USD"]', '500.00');
  await click('核对并记账');
  assert.equal(ledgerWrites.length, 0);
  await click('返回修改');
  await click('核对并记账');
  ledgerResponseLost = true;
  await click('确认记账');
  await wait('document.querySelector("dialog")?.innerText.includes("模拟响应丢失")');
  await click('确认记账');
  await wait('document.querySelector(".card-ledger-table")?.innerText.includes("初始余额")');
  assert.equal(ledgerWrites[0].request_key, ledgerWrites[1].request_key);
  assert.equal(cardLedger.length, 1);
  await click('记录开通扣款');
  await select('扣款关联账号', `${account.label} · ${account.email}`);
  await fill('input[aria-label="记账金额 USD"]', '150.25');
  await click('核对并记账');
  assert.ok(await evaluate('document.querySelector(".card-ledger-confirm").innerText.includes("8,919.64")'));
  await click('确认记账');
  await wait('document.querySelector(".card-ledger-table")?.innerText.includes("349.75")');
  assert.equal(bankCards[0].balance_usd_minor, 34975);
  assert.equal(ledgerWrites.at(-1).account_id, 1);
  assert.equal(await evaluate('document.querySelector("dialog").scrollWidth <= document.querySelector("dialog").clientWidth'), true);
  await writeFile(join(directory, 'card-ledger-mobile.png'), Buffer.from((await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId)).data, 'base64'));
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false }, sessionId);
  await writeFile(join(directory, 'card-ledger.png'), Buffer.from((await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId)).data, 'base64'));
  await evaluate('document.querySelector("dialog button[aria-label=关闭]").click()');
  await wait('!document.querySelector("dialog") && document.querySelector(".bank-card-row").innerText.includes("349.75")');
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true }, sessionId);
  await evaluate('document.querySelector(".workspace-nav a[href$=proxies]").click()');
  await wait('Boolean(document.querySelector(".proxy-manager"))');

  await click('导入代理');
  await fill('dialog textarea', 'socks5://203.0.113.10:1080:proxy-user:proxy-secret', 'HTMLTextAreaElement');
  await click('测试并导入');
  await wait('document.querySelector(".import-results")?.innerText.includes("已导入")');
  await click('关闭');
  await click('删除'); await click('确认删除');
  await wait('!document.querySelector("dialog") && !document.querySelector(".proxy-row")');
  await click('添加代理');
  await fill('.proxy-form input[name=name]', '美国代理');
  await fill('.proxy-form input[name=host]', '203.0.113.10');
  await fill('.proxy-form input[name=username]', 'proxy-user');
  await fill('.proxy-form input[name=password]', 'proxy-secret');
  assert.equal(await evaluate('document.querySelector(".proxy-form .primary").disabled'), true);
  draftFails = true;
  await click('测试');
  await wait('document.querySelector(".proxy-form").innerText.includes("代理认证失败")');
  assert.equal(await evaluate('document.querySelector(".proxy-form .primary").disabled'), true);
  draftFails = false;
  await click('测试');
  await wait('!document.querySelector(".proxy-form .primary").disabled');
  await click('保存代理');
  await wait('document.querySelector(".proxy-row")?.innerText.includes("美国代理")');
  assert.equal(await evaluate('document.querySelector(".proxy-row").tagName'), 'TR');
  await click('测试');
  await wait('Boolean(document.querySelector(".proxy-result.match"))');
  await click('使用记录');
  await wait('document.querySelector(".usage-row")?.innerText.includes("08:30:00")');
  await evaluate('document.querySelector("dialog button[aria-label=关闭]").click()');
  await click('获取 IP');
  await wait('Boolean(document.querySelector(".proxy-result.mismatch"))');
  assert.ok(await evaluate('document.documentElement.scrollWidth <= window.innerWidth'));
  await evaluate('document.querySelector(".data-table-wrap").scrollLeft = 0');
  const proxyShot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'proxy-mobile.png'), Buffer.from(proxyShot.data, 'base64'));
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 1600, height: 900, deviceScaleFactor: 1, mobile: false }, sessionId);
  await evaluate('window.scrollTo(0,0)');
  await writeFile(join(directory, 'proxies-table.png'), Buffer.from((await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId)).data, 'base64'));
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true }, sessionId);
  await click('编辑');
  await wait('Boolean(document.querySelector(".proxy-form input[name=password]"))');
  assert.equal(await evaluate('document.querySelector(".proxy-form input[name=password]").type'), 'text');
  assert.equal(await evaluate('document.querySelector(".proxy-form input[name=password]").value'), 'proxy-secret');
  await click('测试');
  await wait('!document.querySelector(".proxy-form .primary").disabled');
  await fill('.proxy-form input[name=name]', '美国代理已编辑');
  assert.equal(await evaluate('document.querySelector(".proxy-form .primary").disabled'), true);
  await click('测试');
  await wait('!document.querySelector(".proxy-form .primary").disabled');
  await click('保存代理');
  await wait('document.querySelector(".proxy-row")?.innerText.includes("美国代理已编辑")');
  await evaluate('document.querySelector(".workspace-nav a[href$=accounts]").click()');
  await wait('Boolean(document.querySelector(".account-proxy button:not(:disabled)"))');
  await select('账号 chat@example.com 的 SOCKS5', '美国代理已编辑 · 203.0.113.10', '美国');
  await wait('document.body.innerText.includes("代理选择已保存")');
  assert.equal(Object.values(bindings)[0], 'proxy-1');
  await click('打开账号');
  await wait('document.querySelector(".browser-status-row strong")?.textContent === "已打开"');
  assert.equal(await evaluate('document.querySelector("dialog").innerText.includes("配对密钥")'), false);
  assert.equal(exportCount, 1); assert.equal(localLaunches.length, 1);
  assert.equal(localLaunches[0].session.accessToken, 'local-test-access');
  assert.equal(localLaunches[0].expected_email, account.email);
  assert.equal(localLaunches[0].environment_id, 'http://127.0.0.1:' + server.address().port + ':user:1:account:1');
  for (const request of localRequests.filter(request => request.method !== 'OPTIONS')) {
    const headers = Object.fromEntries(Object.entries(request.headers).map(([name, value]) => [name.toLowerCase(), value]));
    assert.equal(headers['x-aitok-client'], 'browser');
    assert.equal(headers['x-aitok-key'], undefined);
    assert.equal(headers.authorization, undefined);
  }
  assert.equal(await evaluate('JSON.stringify([localStorage,sessionStorage]).includes("local-test")'), false);
  assert.ok(await evaluate('document.documentElement.scrollWidth <= window.innerWidth'));
  const localMobile = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'local-mobile.png'), Buffer.from(localMobile.data, 'base64'));
  await wait('document.querySelector(".account-actions").innerText.includes("关闭浏览器")');
  await evaluate('document.querySelector("dialog button[aria-label=关闭]").click()');
  await cdp.send('Page.reload', {}, sessionId);
  await wait('document.querySelector(".account-actions")?.innerText.includes("关闭浏览器")');
  assert.equal(localLaunches.length, 1);
  await click('关闭浏览器');
  await wait('document.querySelector(".account-actions").innerText.includes("打开账号")');
  assert.equal(localState, 'closed');
  await click('打开账号');
  await wait('document.querySelector(".browser-status-row strong")?.textContent === "已打开"');
  assert.equal(localLaunches.length, 2);
  authenticatedAt = '2026-09-15T00:30:00Z'; localState = 'authenticated';
  await wait('document.querySelector(".last-login")?.innerText.includes("08:30:00")');
  assert.equal(loginWrites, 1);
  localState = 'closed'; authenticatedAt = undefined;
  await wait('!document.querySelector("dialog")');
  await wait('document.querySelector(".account-actions").innerText.includes("打开账号")');
  await click('打开账号');
  await wait('document.querySelector(".browser-status-row strong")?.textContent === "已打开"');
  await click('关闭账号窗口');
  await wait('!document.querySelector("dialog")');
  await wait('document.querySelector(".account-actions").innerText.includes("打开账号")');
  exportExpired = true;
  await click('打开账号');
  await wait('document.querySelector("dialog").innerText.includes("Session 已过期")');
  assert.equal(localLaunches.length, 3);
  await evaluate('document.querySelector("dialog button[aria-label=关闭]").click()');
  await evaluate('document.querySelector(".workspace-nav a[href$=proxies]").click()');
  await wait('Boolean(document.querySelector(".proxy-row"))');
  const deletesBefore = localRequests.filter(request => request.method === 'DELETE').length;
  await click('删除');
  await wait('Boolean(document.querySelector("dialog"))');
  assert.equal(localRequests.filter(request => request.method === 'DELETE').length, deletesBefore);
  await click('取消');
  await wait('!document.querySelector("dialog")');
  assert.equal(localRequests.filter(request => request.method === 'DELETE').length, deletesBefore);
  await click('删除'); await click('确认删除');
  await wait('document.querySelector("dialog").innerText.includes("此代理仍有账号使用")');
  await click('取消');
  await wait('!document.querySelector("dialog")');
  await evaluate('document.querySelector(".workspace-nav a[href$=accounts]").click()');
  await wait('Boolean(document.querySelector(".account-proxy button:not(:disabled)"))');
  await select('账号 chat@example.com 的 SOCKS5', '直连（不使用代理）');
  await wait('!document.querySelector(".account-proxy button").disabled');
  await evaluate('document.querySelector(".workspace-nav a[href$=proxies]").click()');
  await wait('Boolean(document.querySelector(".proxy-row"))');
  await click('删除'); await click('确认删除');
  await wait('!document.querySelector(".proxy-row")');
  await evaluate('document.querySelector(".workspace-nav a[href$=accounts]").click()');
  await wait('Boolean(document.querySelector(".account-proxy"))');
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false }, sessionId);
  const localDesktop = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'local-desktop.png'), Buffer.from(localDesktop.data, 'base64'));
  await evaluate('Array.from(document.querySelectorAll("button")).find(button => button.getAttribute("aria-label") === "删除 工作账号").click()');
  await wait('Boolean(document.querySelector("dialog"))');
  assert.equal(accountDeletes, 0);
  await click('取消');
  await wait('!document.querySelector("dialog")');
  assert.equal(accountDeletes, 0);
  await evaluate('Array.from(document.querySelectorAll("button")).find(button => button.getAttribute("aria-label") === "删除 工作账号").click()');
  await click('确认删除');
  await wait('!document.querySelector("dialog")');
  assert.equal(accountDeletes, 1);
  await evaluate('document.querySelector(".user-menu summary").click()');
  await click('白色');
  await evaluate('document.querySelector(".user-menu summary").click()');
  const lightShot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'light-accounts.png'), Buffer.from(lightShot.data, 'base64'));
  await cdp.send('Page.reload', {}, sessionId);
  await wait('Boolean(document.querySelector(".account-toolbar")) && document.documentElement.dataset.theme === "light"');
  await evaluate('document.querySelector(".user-menu summary").click()');
  await click('黑色');
  await evaluate('document.querySelector(".user-menu summary").click()');
  const darkShot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'dark-accounts.png'), Buffer.from(darkShot.data, 'base64'));
  account.has_session = false;
  await cdp.send('Page.reload', {}, sessionId);
  await wait('document.body.innerText.includes("未保存 Session")');
  assert.equal(await evaluate('Array.from(document.querySelectorAll(".account-actions button")).find(button => button.textContent === "打开账号").disabled'), true);
  account.user_id = 2;
  await cdp.send('Page.reload', {}, sessionId);
  await wait('document.body.innerText.includes("工作账号")');
  assert.equal(await evaluate('Array.from(document.querySelectorAll(".account-actions button")).some(button => button.textContent === "打开账号")'), false);
  user.role = 'admin';
  const userRequestsBefore = requests.filter(url => url.includes('/api/users')).length;
  await cdp.send('Page.navigate', { url: 'http://127.0.0.1:' + server.address().port + '/admin/users' }, sessionId);
  await wait('document.body.innerText.includes("无权访问用户列表")');
  assert.equal(await evaluate('Boolean(document.querySelector(".workspace-nav a[href$=users]"))'), false);
  assert.equal(requests.filter(url => url.includes('/api/users')).length, userRequestsBefore);
  await cdp.send('Page.navigate', { url: 'http://127.0.0.1:' + server.address().port + '/wallet?reference=legacy' }, sessionId);
  await wait('location.pathname === "/admin/wallet" && location.search === "?reference=legacy" && Boolean(document.querySelector(".wallet-grid"))');
  await cdp.send('Page.navigate', { url: 'http://127.0.0.1:' + server.address().port + '/features' }, sessionId);
  await wait('Boolean(document.querySelector("header nav"))');
  assert.deepEqual(errors, []);
  t.diagnostic(`页面截图：${directory}`);
});

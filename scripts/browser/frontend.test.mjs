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
  const account = { id: 1, user_id: 1, label: '工作账号', email: 'chat@example.com', has_session: true, renewal_enabled: true, payment_card_id: null };
  const errors = [], imported = [], launches = [], requests = [];
  const localLaunches = [], localRequests = [], blankLaunches = [];
  let notesFail = false, blankFail = false, quickOrderFail = false;
  let failedQuickInput;
  let fingerprintResets = 0;
  let localFingerprint = null;
  let localState = 'closed', exportCount = 0, exportExpired = false;
  let proxies = [], bindings = {}, testCount = 0, draftFails = false, proxyPassword = '', accountDeletes = 0;
  let proxyBindingFails = false, proxiesUnavailable = false;
  let browserState = 'closed';
  let groups = [], loginWrites = 0, authenticatedAt;
  let savedProxy = '';
  let twoFactorEnabled = false;
  const otpHeaders = [];
  let addressRows = Array.from({ length: 21 }, (_, index) => ({ id: index + 1, address_line1: index === 0 ? '4111 Gateway [Road]' : `${index + 1} Test Street`, address_line2: '', city: 'Portland', state: 'OR', postal_code: '97201', country: 'US', source_url: 'https://www.meiguodizhi.com/usa-address/oregon', source_data: { Full_Name: 'Test User', Occupation: 'Engineer', Extra_Field: 'Preserved value', CVV2: '123' }, can_edit: true }));
  const addressWrites = [];
  let bankCards = [];
  let bankUploadTooLarge = false;
  let accountSettingsFail = false;
  const bankWrites = [], bankPayloads = [];
  let bankDetailReads = 0;
  const cardLedger = [], ledgerWrites = [];
  let ledgerResponseLost = false;
  const userRows = [{ id: 3, username: 'admin', email: '', role: 'super_admin', created_at: '2026-09-01T00:00:00Z' }, { id: 1, email: 'member@example.com', role: '', created_at: '2026-09-02T00:00:00Z' }];
  const roleWrites = [];
  const ownerLookups = [], ownerWrites = [];
  const ownerTargets = [{ id: 4, email: 'recipient@example.com' }, { id: 1, email: 'member@example.com' }];
  let rechargePackages=[], rechargeOrders=[];
  let phpRate='0.01589', phpCNYRate='0.1067';
  const operationWrites=[];
  const adminActivities=[];
  const { targetId } = await cdp.send('Target.createTarget', { url: 'about:blank' });
  const { sessionId } = await cdp.send('Target.attachToTarget', { targetId, flatten: true });
  await cdp.send('Network.enable', {}, sessionId);
  await cdp.send('Network.setBlockedURLs', { urls: ['https://fonts.googleapis.com/*', 'https://fonts.gstatic.com/*'] }, sessionId);
  await cdp.send('Runtime.enable', {}, sessionId);
  await cdp.send('Page.enable', {}, sessionId);
  await cdp.send('Fetch.enable', { patterns: [{ urlPattern: '*://*/api/*' }, { urlPattern: 'http://127.0.0.1:15684/*' }, { urlPattern: 'http://127.0.0.1:15685/*' }] }, sessionId);
  cdp.on('message', message => {
    if (message.method === 'Runtime.exceptionThrown') errors.push(message.params.exceptionDetails.text);
    if (message.method !== 'Fetch.requestPaused') return;
    (async () => {
      const { requestId, request } = message.params;
      const url = new URL(request.url);
      requests.push(request.url);
      if (request.method === 'POST' && /^\/api\/accounts\/\d+\/(browser|browser-session)$/.test(url.pathname)) {
        otpHeaders.push(Object.entries(request.headers).find(([key])=>key.toLowerCase()==='x-aitok-totp')?.[1]);
      }
      let data = {};
      let responseCode = request.method === 'OPTIONS' ? 204 : 200;
      if (['http://127.0.0.1:15684', 'http://127.0.0.1:15685'].includes(url.origin)) {
        localRequests.push(request);
        if (url.pathname === '/activity-export') data = {device_id:'test-device',events:[],next_cursor:0};
        else if (url.pathname === '/health') data = { status: 'ok', version: 2, fingerprint: 'native-noise-v1', blank_browser: true };
        else if (url.pathname === '/blank-browsers' && request.method === 'POST') {
          if (blankFail) { responseCode = 503; data = { error: '模拟空白浏览器启动失败' }; }
          else { blankLaunches.push(JSON.parse(request.postData)); data = { state: 'opened', environment_id: 'blank-test' }; }
        }
        else if (url.pathname.endsWith('/fingerprint')) { if (request.method === 'POST') fingerprintResets++; localFingerprint = { id: 'test-fingerprint', generation: 2, mode: 'native-noise-v1', created_at: '2026-09-17T00:30:00Z' }; data = { state: 'closed', fingerprint: localFingerprint, message: '已重新生成账号指纹，下次打开生效' }; }
        else if (url.pathname === '/proxies/parse') data = { items: [{ line: 1, proxy: { host: '203.0.113.10', port: 1080, username: 'proxy-user', password: 'proxy-secret', name: 'Imported' } }] };
        else if (url.pathname === '/proxy-history') {
          // 模拟未重启的旧启动器，忽略 page_size，前端需合并旧分页。
          const page = Number(url.searchParams.get('page') || 1);
          const records = Array.from({ length: 61 }, (_, i) => ({ id: 'usage-' + i, action: 'open', ok: true, email: account.email, created_at: '2026-09-15T00:30:00Z' }));
          data = { records: records.slice((page - 1) * 20, page * 20), total: records.length, page, page_size: 20 };
        }
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
          if (proxiesUnavailable && request.method !== 'OPTIONS') { responseCode = 503; data = { error: '模拟本机代理配置读取失败' }; }
        } else if (url.pathname.endsWith('/proxy')) {
          if (request.method === 'PATCH' && !proxyBindingFails) {
            const id = decodeURIComponent(url.pathname.split('/')[2]);
            const { proxy_id } = JSON.parse(request.postData);
            if (proxy_id) bindings[id] = proxy_id; else delete bindings[id];
          }
          data = { proxies, bindings };
          if (proxyBindingFails && request.method === 'PATCH') { responseCode = 400; data = { error: '模拟代理绑定保存失败' }; }
        } else {
          if (request.method === 'POST') { localLaunches.push(JSON.parse(request.postData)); localState = 'opened'; }
          if (request.method === 'DELETE') localState = 'closed';
          data = { state: localState, authenticated_at: authenticatedAt, fingerprint: localFingerprint };
        }
      } else if (url.pathname === '/api/two-factor') {
        const input = request.postData ? JSON.parse(request.postData) : {};
        if (input.action === 'setup') data = {secret:'GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ',otpauth_url:'otpauth://totp/AiTok:test?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&issuer=AiTok'};
        else { if (input.action === 'confirm') twoFactorEnabled = true; data = {enabled:twoFactorEnabled}; }
      } else if (url.pathname === '/api/admin-activity') {
        if (request.method === 'POST') adminActivities.push(JSON.parse(request.postData));
        responseCode=204;
      } else if (url.pathname.startsWith('/api/packages')) {
        const input=request.postData?JSON.parse(request.postData):{};
        if(request.method==='POST') rechargePackages.push({...input,id:1});
        if(request.method==='PATCH') rechargePackages[0]={...rechargePackages[0],...input};
        if(request.method==='DELETE') rechargePackages=[];
        const rate={id:1,rate:phpRate,cny_rate:phpCNYRate,source:'https://www.exchangerate-api.com',effective_at:new Date().toISOString(),synced_at:new Date().toISOString()};
        data={packages:rechargePackages.map(p=>p.auto_usd?{...p,sale_usd_minor:Math.round(p.original_amount_minor*Number(phpRate)),sale_cny_minor:Math.round(p.original_amount_minor*Number(phpCNYRate)),cny_price_ready:true,price_ready:true,exchange_rate:rate}:p),can_manage:true,exchange_rate:rate,exchange_rate_fresh:true};
      } else if (url.pathname.endsWith('/collection-quote')) {
        const currency=url.searchParams.get('currency'),amountMinor=Math.round(Number(url.searchParams.get('amount'))*100);
        const usdMinor=currency==='CNY'?Math.round(amountMinor/7):amountMinor;
        data={currency,amount_minor:amountMinor,usd_minor:usdMinor,exchange_rate:{usd_per_unit:currency==='CNY'?'1/7':'1',...(currency==='CNY'?{batch:{id:99,source:'test',effective_at:new Date().toISOString(),synced_at:new Date().toISOString()}}:{})},profit:{usd_minor:usdMinor-20000,received_minor:amountMinor-(currency==='CNY'?140000:20000),cost_usd_minor:20000,rate_percent:((usdMinor-20000)/usdMinor*100).toFixed(2),estimated:true}};
      } else if (url.pathname === '/api/orders/record' && request.method === 'POST' && quickOrderFail) {
        failedQuickInput = JSON.parse(request.postData);
        responseCode = 409; data = { error: '模拟月订单记账失败' };
      } else if (url.pathname.startsWith('/api/orders')) {
        const input=request.postData?JSON.parse(request.postData):{};
        if(request.method==='POST') {
          operationWrites.push(input);
          if(url.pathname==='/api/orders/record') { input.action='record'; rechargeOrders.push({id:1,order_no:'order-smoke-1',order_status:'active',account_id:account.id,account_email:account.email,package_snapshot:rechargePackages[0],period_start:'2030-01-01',period_end:'2030-02-01',payment_status:'unpaid',fulfillment_status:'pending',sale_usd_minor:20000,wallet_tokens:100,cost_usd_minor:0,refunded_usd_minor:0,version:0}); }
          {
            const order=rechargeOrders[0];order.version++;
            if(input.action==='record' && input.received_amount) {
              order.payment_status='paid';order.payment_method='manual';
              order.received_currency=input.received_currency;order.received_amount_minor=Math.round(Number(input.received_amount)*100);
              order.received_usd_minor=input.received_currency==='CNY'?Math.round(order.received_amount_minor/7):order.received_amount_minor;
              order.profit={usd_minor:order.received_usd_minor-order.sale_usd_minor,received_minor:order.received_amount_minor-(input.received_currency==='CNY'?order.sale_usd_minor*7:order.sale_usd_minor),rate_percent:((order.received_usd_minor-order.sale_usd_minor)/order.received_usd_minor*100).toFixed(2),cost_usd_minor:order.sale_usd_minor,estimated:true};
            }
            if(input.quick_month) account.renewal_date = '2030-02-28';
            if(input.action==='record'){order.order_source=input.order_source || order.order_source || '';order.cost_usd_minor=order.sale_usd_minor;order.fulfillment_status='completed';if(order.profit)order.profit.estimated=false}
            if(input.action==='refund_note'){order.order_status='refunded';order.profit=null}
            if(input.action==='discard'){order.order_status='discarded';order.profit=null}
            if(input.evidence && input.action!=='refund_note') order.evidence=input.evidence
          }
        }
        data=url.pathname==='/api/orders/1'?{order:rechargeOrders[0],events:operationWrites.filter(v=>v.action).map((input,index)=>({id:index+1,actor_id:3,action:input.action,created_at:'2026-09-15T00:00:00Z',after_data:{input}}))}:{orders:rechargeOrders,sources:[...new Set(rechargeOrders.map(o=>o.order_source).filter(Boolean))],total:rechargeOrders.length,can_manage:true,can_finance:true,can_refund:true};
      } else if (url.pathname==='/api/notices') data={notices:[]};
      else if (url.pathname==='/api/audit') data={events:[
        {id:2,actor:'audit@example.com',actor_id:3,entity_type:'admin_request',entity_id:0,action:'DELETE /api/accounts/1',created_at:'2026-09-15T00:00:00Z',after_data:{source:'server',page:'/admin/accounts',resource:'/api/accounts/1',result:'failure',status:403}},
        {id:1,actor:'audit@example.com',actor_id:3,entity_type:'admin_ui',entity_id:3,action:'访问页面',created_at:'2026-09-15T00:00:00Z',after_data:{source:'browser',page:'/admin/accounts',result:'visited'}},
      ]};
      else if (url.pathname==='/api/proxy-activity') data={events:[],records:[]};
      else if (url.pathname==='/api/payment-exceptions') data={exceptions:[]};
      else if (url.pathname.startsWith('/api/users')) {
        if (request.method === 'PATCH') { const input = JSON.parse(request.postData); roleWrites.push(input); userRows[1].role = input.role; data = { role: input.role }; }
        else data = { users: userRows, total: userRows.length, page: 1, page_size: 20 };
      } else if (url.pathname.startsWith('/api/account-groups')) {
        const input = request.postData ? JSON.parse(request.postData) : {};
        if (request.method === 'POST') groups.push({ id: 10, user_id: input.user_id, name: input.name, account_count: 0 });
        if (request.method === 'PATCH') groups[0].name = input.name;
        if (request.method === 'DELETE') { groups = []; account.group_id = null; }
        data = request.method === 'POST' ? { group: groups.at(-1) } : { groups };
      } else if (url.pathname === '/api/accounts/1/notes' && request.method === 'PATCH') {
        if (notesFail) { responseCode = 500; data = { error: '模拟备注保存失败' }; }
        else { account.notes = JSON.parse(request.postData).notes; data = { id: account.id, notes: account.notes }; }
      } else if (url.pathname === '/api/accounts/1/group') {
        if (request.method === 'PATCH') account.group_id = JSON.parse(request.postData).group_id;
        groups.forEach(group => { group.account_count = group.id === account.group_id ? 1 : 0; });
        data = { group_id: account.group_id };
      } else if (url.pathname === '/api/accounts/1/owner') {
        const input = request.postData ? JSON.parse(request.postData) : {};
        const target = ownerTargets.find(user => user.email === input.email);
        if (request.method === 'POST') {
          ownerLookups.push(input);
          if (target) data = { user: target };
          else { responseCode = 404; data = { error: '未找到该邮箱对应的可用注册用户，请核对完整邮箱' }; }
        } else if (request.method === 'PATCH') {
          ownerWrites.push(input);
          if (!target || input.user_id !== target.id || input.expected_owner_id !== account.user_id) { responseCode = 409; data = { error: '账号或目标用户已变更，请重新查找' }; }
          else {
            account.user_id = target.id; account.owner_email = target.email; account.group_id = null;
            groups.forEach(group => { group.account_count = 0; });
            data = { user_id: account.user_id, owner_email: account.owner_email, group_id: null };
          }
        }
      } else if (url.pathname === '/api/accounts/1/login') {
        if (request.method === 'POST') { loginWrites++; account.last_login_at = JSON.parse(request.postData).logged_in_at; }
        data = { last_login_at: account.last_login_at };
      } else if (/^\/api\/bank-cards\/\d+\/ledger$/.test(url.pathname)) {
        if (request.method === 'GET' && url.searchParams.get('quote') === '1') {
          const pkg = rechargePackages.find(item => item.id === Number(url.searchParams.get('package_id')));
          const mode = url.searchParams.get('charge_mode');
          const minor = mode === 'package' ? pkg.sale_usd_minor : Math.round(Number(url.searchParams.get('charge_amount')) * 100);
          data = { package: pkg, mode, charge_currency: mode === 'CNY' ? 'CNY' : 'USD', charge_amount_minor: minor, amount_usd_minor: mode === 'CNY' ? Math.round(minor / 7) : minor, exchange_rate: mode === 'CNY' ? { usd_per_unit: '1/7', batch: { id: 1, synced_at: '2026-09-18T01:00:00Z' } } : null };
        } else if (request.method === 'POST') {
          const input = JSON.parse(request.postData); ledgerWrites.push(input);
          const existing = cardLedger.find(entry => entry.request_key === input.request_key);
          if (!existing) {
            const amount = (input.expected_amount_usd_minor || Math.round(Number(input.amount_usd) * 100)) * (input.kind === 'deposit' ? 1 : -1);
            bankCards[0].balance_usd_minor += amount;
            cardLedger.unshift({ id: cardLedger.length + 1, ...input, kind: input.kind === 'deposit' && !cardLedger.length ? 'opening' : input.kind, amount_usd_minor: amount, balance_after_usd_minor: bankCards[0].balance_usd_minor, account_label: input.account_id ? account.label : '', account_email: input.account_id ? account.email : '', original_php_minor: input.account_id ? 891964 : 0, created_at: '2026-09-15T01:00:00Z' });
          }
          data = { entry: existing || cardLedger[0], replayed: Boolean(existing) };
          if (ledgerResponseLost) { ledgerResponseLost = false; responseCode = 503; data = { error: '模拟响应丢失，请重试' }; }
        } else {
          data = { entries: cardLedger, total: cardLedger.length, balance_usd_minor: bankCards[0].balance_usd_minor, deposited_usd_minor: cardLedger.reduce((sum, entry) => sum + Math.max(0, entry.amount_usd_minor), 0), spent_usd_minor: cardLedger.reduce((sum, entry) => sum - Math.min(0, entry.amount_usd_minor), 0) };
        }
      } else if (bankUploadTooLarge && url.pathname.startsWith('/api/bank-cards') && ['POST', 'PATCH'].includes(request.method)) {
        responseCode = 413;
        data = '<html><body>413 Request Entity Too Large</body></html>';
      } else if (url.pathname.startsWith('/api/bank-cards')) {
        const input = request.postData ? JSON.parse(request.postData) : {};
        if (['POST', 'PATCH', 'DELETE'].includes(request.method)) bankWrites.push(request.method);
        if (request.method === 'POST') bankCards.push({ balance_usd_minor: 0, ...input, id: 1, last4: '4242', brand: 'Visa' });
        if (request.method === 'PATCH') {
          assert.equal(input.edit_token, 'verified-card-edit');
          bankPayloads.push(input);
          bankCards[0] = { ...bankCards[0], ...input };
        }
        if (request.method === 'DELETE') bankCards = [];
        if (request.method === 'GET' && url.pathname === '/api/bank-cards/1') {
          bankDetailReads++;
          const code = Object.entries(request.headers).find(([key]) => key.toLowerCase() === 'x-aitok-totp')?.[1];
          if (code !== '123456') {
            responseCode = 403;
            data = { error: '验证码无效或已使用' };
          } else {
            const {number, cvc, ...card} = bankCards[0];
            data = {card: {...card, number: '*'.repeat(number.length - 4) + card.last4, has_cvc:Boolean(cvc)}, edit_token:'verified-card-edit'};
          }
        } else data = url.pathname === '/api/bank-cards/1' ? { card: bankCards[0] } : { cards: bankCards.map(({ number, cvc, ...card }) => ({...card, has_cvc:Boolean(cvc)})), platforms: [...new Set(bankCards.map(card => card.platform).filter(Boolean))], total: bankCards.length, page: 1, page_size: 20 };
      } else if (url.pathname.startsWith('/api/addresses')) {
        const input = request.postData ? JSON.parse(request.postData) : {};
        const id = Number(url.pathname.split('/')[3]);
        if (['POST', 'PATCH', 'DELETE'].includes(request.method)) addressWrites.push(request.method);
        if (request.method === 'POST') addressRows.unshift({ ...input, id: 100, source_url: '', can_edit: true });
        if (request.method === 'PATCH') addressRows = addressRows.map(address => address.id === id ? { ...address, ...input } : address);
        if (request.method === 'DELETE') addressRows = addressRows.filter(address => address.id !== id);
        const rows = addressRows.filter(address => JSON.stringify(address).toLowerCase().includes((url.searchParams.get('q') || '').toLowerCase()) || (url.searchParams.get('state_codes') || '').split(',').includes(address.state));
        const page = Number(url.searchParams.get('page') || 1);
        const pageSize = Number(url.searchParams.get('page_size') || 20);
        data = { addresses: rows.slice((page - 1) * pageSize, page * pageSize), total: rows.length, page, page_size: pageSize };
      } else if (url.pathname === '/api/accounts/1/billing-address' && request.method === 'PATCH') {
        if (accountSettingsFail) { responseCode = 409; data = { error: '模拟地址绑定失败' }; }
        else {
          const input = JSON.parse(request.postData);
          const address = addressRows.find(row => row.id === (input.random ? 2 : input.billing_address_id));
          data = { billing_address: address ? { ...address, full_name: 'Test User' } : null, billing_address_id: address?.id || null, billing_address_label: address ? [address.address_line1, address.city, address.state, address.postal_code].join(', ') : '' };
          Object.assign(account, data);
        }
      } else if (url.pathname === '/api/accounts/payment-cards') {
        data = { cards: bankCards.map(({ id, label, last4, brand }) => ({ id, label, last4, brand })) };
      } else if (request.method === 'PATCH' && (url.pathname === '/api/accounts/1/subscription' || url.pathname === '/api/accounts/1/subscription-package' || url.pathname === '/api/accounts/1/payment-card')) {
        if (accountSettingsFail) { responseCode = 409; data = { error: '模拟账号设置保存失败' }; }
        else {
          const input = JSON.parse(request.postData);
          Object.assign(account, input);
          if ('payment_card_id' in input) {
            const card = bankCards.find(card => card.id === input.payment_card_id);
            Object.assign(account, { payment_card_label: card?.label || '', payment_card_last4: card?.last4 || '', payment_card_available: Boolean(card) });
          }
          data = input;
        }
      } else if (url.pathname === '/api/accounts/1' && request.method === 'DELETE') { accountDeletes++;
      } else if (url.pathname === '/api/accounts/1/browser-session') {
        if (request.method === 'POST') exportCount++;
        if (exportExpired) { responseCode = 422; data = { error: 'Session 已过期，请更新' }; }
        else data = { account_id: 1, session: { accessToken: 'local-test-access', user: { email: account.email } }, assistant_token: 'local-test-assistant' };
      } else if (url.pathname === '/api/me') data = { user, accounts: [account] };
      else if (url.pathname === '/api/accounts') {
        if (request.method === 'POST') imported.push(JSON.parse(request.postData));
        const group=url.searchParams.get('group');
        const accountRows=(!group || (group==='none' ? !account.group_id : String(account.group_id)===group)) ? [account] : [];
        data = { accounts: accountRows, account, total:accountRows.length, page:1, page_size:20 };
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
          { name: 'Content-Type', value: typeof data === 'string' ? 'text/html' : 'application/json' }, { name: 'Access-Control-Allow-Origin', value: '*' },
          { name: 'Access-Control-Allow-Headers', value: 'Content-Type, Authorization, X-AiTok-Client, X-Aitok-Page, X-Aitok-TOTP' },
          { name: 'Access-Control-Allow-Methods', value: 'GET, POST, PATCH, DELETE, OPTIONS' },
          { name: 'Access-Control-Allow-Private-Network', value: 'true' },
        ],
        body: request.method === 'OPTIONS' ? '' : Buffer.from(typeof data === 'string' ? data : JSON.stringify(data)).toString('base64'),
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
  const click = async text => {
    await wait(`(() => { const button = Array.from(document.querySelectorAll('button')).find(button => button.textContent === ${JSON.stringify(text)} && !button.disabled); if (!button) return false; button.click(); return true; })()`);
  };
  const navigateAdmin = async page => {
    const selector = JSON.stringify('.workspace-nav a[href="/admin/' + page + '"]');
    await evaluate(`(() => {
      const sidebar = document.querySelector('.admin-sidebar');
      if (getComputedStyle(sidebar).visibility === 'hidden') document.querySelector('.sidebar-toggle').click();
      const link = document.querySelector(${selector});
      const group = link.closest('.sidebar-group');
      const button = group?.querySelector('.sidebar-group-toggle');
      if (button?.getAttribute('aria-expanded') === 'false') button.click();
    })()`);
    await wait(`document.querySelector(${selector}).checkVisibility()`);
    await evaluate(`document.querySelector(${selector}).click()`);
    await wait(`location.pathname === ${JSON.stringify('/admin/' + page)}`);
  };
  const fill = async (selector, value, type = 'HTMLInputElement') => {
    await wait(`document.querySelector(${JSON.stringify(selector)}) instanceof ${type}`);
    return evaluate(`(() => { const element = document.querySelector(${JSON.stringify(selector)}); Object.getOwnPropertyDescriptor(${type}.prototype, 'value').set.call(element, ${JSON.stringify(value)}); element.dispatchEvent(new Event('input', { bubbles: true })); })()`);
  };
  const select = async (label, text, query = '', keyboard = false) => {
    await wait(`(() => { const button = Array.from(document.querySelectorAll('button[role=combobox]')).find(button => button.getAttribute('aria-label') === ${JSON.stringify(label)} && !button.disabled); if (!button) return false; button.click(); return true; })()`);
    await wait('Boolean(document.querySelector(".select-search input"))');
    if (query) await fill('.select-search input', query);
    await wait(`Array.from(document.querySelectorAll('[role=option]')).some(option => option.textContent === ${JSON.stringify(text)})`);
    if (keyboard) await cdp.send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13 }, sessionId);
    else await evaluate(`Array.from(document.querySelectorAll('[role=option]')).find(option => option.textContent === ${JSON.stringify(text)}).click()`);
    await wait('!document.querySelector(".select-popup")');
  };
  await wait('document.body.innerText.includes("工作账号")');
  await wait('location.pathname === "/admin/accounts"');
  const noteText = '账号使用记录\n' + '多行长文本内容'.repeat(200) + '\n<img src=x onerror="window.notesExecuted=true">';
  await click('添加备注');
  await fill('dialog textarea[name=notes]', noteText, 'HTMLTextAreaElement');
  notesFail = true;
  await click('保存备注');
  await wait('document.querySelector("dialog").innerText.includes("模拟备注保存失败")');
  assert.equal(await evaluate('document.querySelector("dialog textarea[name=notes]").value'), noteText);
  notesFail = false;
  await click('保存备注');
  await wait('!document.querySelector("dialog")');
  assert.equal(account.notes, noteText);
  assert.equal(await evaluate('document.querySelector(".account-notes-preview").textContent'), noteText);
  assert.equal(await evaluate('Boolean(window.notesExecuted)'), false);
  await click('编辑备注');
  assert.equal(await evaluate('document.querySelector("dialog textarea[name=notes]").value'), noteText);
  await fill('dialog textarea[name=notes]', '', 'HTMLTextAreaElement');
  await click('保存备注');
  await wait('!document.querySelector("dialog")');
  assert.equal(account.notes, '');
  await click('打开空白浏览器');
  await wait('document.querySelector("dialog").innerText.includes("暂无代理")');
  assert.equal(await evaluate('document.querySelector("dialog button.primary").disabled'), true);
  await evaluate('document.querySelector("dialog button[aria-label=关闭]").click()');
  const statusRequestCount = () => localRequests.filter(request => request.method === 'GET' && /^\/browsers\/[^/]+$/.test(new URL(request.url).pathname)).length;
  await delay(2200);
  assert.equal(statusRequestCount(), 0, '进入账号页不能自动逐账号查询浏览器状态');
  await click('刷新浏览器状态');
  await wait('Array.from(document.querySelectorAll("button")).some(button => button.textContent === "刷新浏览器状态" && !button.disabled)');
  assert.equal(statusRequestCount(), 1, '手动刷新只检查当前页一次');
  await delay(2200);
  assert.equal(statusRequestCount(), 1, '手动刷新后不能启动自动轮询');
  assert.equal(await evaluate('Boolean(document.querySelector("header nav"))'), false);
  assert.deepEqual(await evaluate('Array.from(document.querySelectorAll(".workspace-nav a"), a => a.getAttribute("href"))'), ['/admin/accounts', '/admin/notices', '/admin/orders', '/admin/packages', '/admin/bank-cards', '/admin/payment-exceptions', '/admin/proxies', '/admin/addresses', '/admin/proxy-activity', '/admin/users', '/admin/audit']);
  assert.equal(await evaluate('document.querySelector(".admin-sidebar").getBoundingClientRect().left'), 0);
  assert.equal(await evaluate('document.querySelector(".admin-content").getBoundingClientRect().left'), 208);
  assert.deepEqual(await evaluate('Array.from(document.querySelectorAll(".sidebar-group-toggle"), button => button.textContent)'), ['账号管理', '充值管理', '资源管理', '系统管理']);
  assert.equal(await evaluate('document.querySelector(".sidebar-group-toggle.active").getAttribute("aria-expanded")'), 'true');
  await click('账号管理');
  await wait('!document.querySelector(".workspace-nav a[href$=accounts]").checkVisibility()');
  await click('系统管理');
  await wait('document.querySelector(".workspace-nav a[href$=audit]").checkVisibility()');
  await cdp.send('Page.reload', {}, sessionId);
  await wait('document.querySelector(".account-row") && document.querySelector(".workspace-nav a[href$=audit]").checkVisibility()');
  assert.equal(await evaluate('document.querySelector(".workspace-nav a[href$=accounts]").checkVisibility()'), true);
  await click('系统管理');
  await cdp.send('Page.reload', {}, sessionId);
  await wait('Boolean(document.querySelector(".account-row"))');
  assert.equal(await evaluate('document.querySelector(".workspace-nav a[href$=audit]").checkVisibility()'), false);
  await navigateAdmin('notices');
  await delay(250);
  const otherPageStatusCount = statusRequestCount();
  await delay(2200);
  assert.equal(statusRequestCount(), otherPageStatusCount, '离开账号页必须停止账号状态轮询');
  await click('账号管理');
  await evaluate('history.back()');
  await wait('location.pathname === "/admin/accounts" && document.querySelector(".workspace-nav a[href$=accounts]").checkVisibility()');
  assert.equal(await evaluate('document.querySelector(".account-row").tagName'), 'TR');
  for (const scrollLeft of [0, 300]) {
    await evaluate(`document.querySelector(".data-table-wrap").scrollLeft = ${scrollLeft}`);
    assert.ok(await evaluate('Math.abs(document.querySelector(".accounts-table th.table-actions").getBoundingClientRect().left - document.querySelector(".account-row td.table-actions").getBoundingClientRect().left) < 1'));
  }
  await evaluate('document.querySelector(".data-table-wrap").scrollLeft = 0');
  await evaluate('document.querySelector("button[aria-label=续订日期：升序排序]").click()');
  await wait('document.querySelector("button[aria-label=续订日期：降序排序]")?.closest("th").getAttribute("aria-sort") === "ascending"');
  await delay(100);
  assert.ok(requests.some(url => url.includes('sort=renewal_date') && url.includes('direction=asc')));
  await evaluate('document.querySelector("button[aria-label=续订日期：降序排序]").click()');
  await wait('document.querySelector("button[aria-label=续订日期：升序排序]")?.closest("th").getAttribute("aria-sort") === "descending"');
  await delay(100);
  assert.ok(requests.some(url => url.includes('sort=renewal_date') && url.includes('direction=desc')));
  await evaluate('document.querySelector(".account-renewal-switch").click()');
  await wait('document.querySelector(".account-renewal-switch").getAttribute("aria-checked") === "false" && !document.querySelector(".account-renewal-switch").disabled');
  assert.equal(account.renewal_enabled, false);
  await cdp.send('Page.reload', {}, sessionId);
  await wait('document.querySelector(".account-renewal-switch")?.getAttribute("aria-checked") === "false"');
  accountSettingsFail = true;
  await evaluate('document.querySelector(".account-renewal-switch").click()');
  await wait('document.body.innerText.includes("模拟账号设置保存失败") && !document.querySelector(".account-renewal-switch").disabled');
  assert.equal(await evaluate('document.querySelector(".account-renewal-switch").getAttribute("aria-checked")'), 'false');
  accountSettingsFail = false;
  await evaluate('document.querySelector(".account-renewal-switch").click()');
  await wait('document.querySelector(".account-renewal-switch").getAttribute("aria-checked") === "true"');
  // 新充值页面使用真实构建产物，资金接口在浏览器边界全部模拟。
  await navigateAdmin('packages');
  await wait('document.body.innerText.includes("尚未配置套餐")');
  await click('新增套餐');
  await fill('input[name=name]','测试 Plus');
  await fill('input[name=original_amount]','1000.00');
  await fill('input[name=sale_usd]','200.00');
  await fill('input[name=wallet_tokens]','100');
  await click('确认并保存');
  await wait('!document.querySelector("dialog") && document.body.innerText.includes("测试 Plus")');
  assert.equal(rechargePackages[0].original_amount_minor,100000);
  await navigateAdmin('orders');
  await wait('document.body.innerText.includes("暂无充值订单")');
  bankCards=[{id:9,label:'运营测试卡',last4:'4242',balance_usd_minor:100000,exp_month:12,exp_year:2035,status:'active'}];
  await navigateAdmin('accounts');
  const productLabel = `${rechargePackages[0].name} · ${rechargePackages[0].region} · ${rechargePackages[0].months}个月`;
  assert.deepEqual(await evaluate('Array.from(document.querySelectorAll(".accounts-table th")).slice(1, 4).map(th => th.textContent.trim())'), ['所属用户', '产品选型', '分组']);
  await select('账号 chat@example.com 的产品选型', productLabel, rechargePackages[0].region);
  await wait('document.querySelector(".account-product button").innerText.includes("测试 Plus") && !document.querySelector(".account-product button").disabled');
  assert.equal(account.subscription_package_id, rechargePackages[0].id);
  await cdp.send('Page.reload', {}, sessionId);
  await wait('document.querySelector(".account-product button")?.innerText.includes("测试 Plus") && !document.querySelector(".account-product button").disabled');
  accountSettingsFail = true;
  await select('账号 chat@example.com 的产品选型', '未设置');
  await wait('document.body.innerText.includes("模拟账号设置保存失败") && !document.querySelector(".account-product button").disabled');
  assert.equal(account.subscription_package_id, rechargePackages[0].id);
  assert.equal(await evaluate('document.querySelector(".account-product button").innerText'), productLabel);
  accountSettingsFail = false;
  await select('账号 chat@example.com 的产品选型', '未设置');
  await wait('document.querySelector(".account-product button").innerText === "未设置" && !document.querySelector(".account-product button").disabled');
  assert.equal(account.subscription_package_id, null);
  await wait('Boolean(document.querySelector(".account-payment-card button:not(:disabled)"))');
  await select('账号 chat@example.com 的付款卡', '运营测试卡 · •••• 4242');
  await wait('document.querySelector(".account-payment-card button").innerText.includes("运营测试卡") && !document.querySelector(".account-payment-card button").disabled');
  assert.equal(account.payment_card_id, 9);
  await cdp.send('Page.reload', {}, sessionId);
  await wait('document.querySelector(".account-payment-card button")?.innerText.includes("运营测试卡") && !document.querySelector(".account-payment-card button").disabled');
  accountSettingsFail = true;
  await select('账号 chat@example.com 的付款卡', '未绑定付款卡');
  await wait('document.body.innerText.includes("模拟账号设置保存失败")');
  assert.equal(account.payment_card_id, 9);
  assert.ok(await evaluate('document.querySelector(".account-payment-card button").innerText.includes("运营测试卡")'));
  accountSettingsFail = false;
  await select('账号 chat@example.com 的付款卡', '未绑定付款卡');
  await wait('document.querySelector(".account-payment-card button").innerText.includes("未绑定付款卡") && !document.querySelector(".account-payment-card button").disabled');
  assert.equal(account.payment_card_id, null);
  await select('账号 chat@example.com 的付款卡', '运营测试卡 · •••• 4242');
  await wait('document.querySelector(".account-payment-card button").innerText.includes("运营测试卡") && !document.querySelector(".account-payment-card button").disabled');
  await evaluate('document.querySelector(".data-table-wrap").scrollLeft = document.querySelector(".data-table-wrap").scrollWidth');
  await evaluate('document.querySelector(".account-address-button").click()');
  await wait('document.querySelectorAll(".address-binding-row").length === 10');
  await fill('input[aria-label="搜索绑定地址"]', 'Gateway');
  await wait('document.querySelectorAll(".address-binding-row").length === 1');
  await writeFile(join(directory, 'account-address-binding.png'), Buffer.from((await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId)).data, 'base64'));
  await click('绑定此地址');
  await wait('!document.querySelector("dialog") && document.querySelector(".account-address-button").innerText.includes("Gateway")');
  assert.equal(account.billing_address_id, 1);
  assert.equal(await evaluate('document.querySelector(".account-address-summary strong").innerText'), '4111 Gateway [Road]');
  assert.equal(await evaluate('document.querySelector(".account-address-summary>span").innerText'), 'Portland, OR, 97201, US');
  assert.equal(await evaluate('document.querySelector(".account-address-summary small").innerText'), 'Test User');
  await evaluate('document.querySelector(".account-address-button").scrollIntoView({block:"center",inline:"center"})');
  await writeFile(join(directory, 'account-address-summary.png'), Buffer.from((await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId)).data, 'base64'));

  await cdp.send('Page.reload', {}, sessionId);
  await wait('document.querySelector(".account-address-button")?.innerText.includes("Gateway")');
  await evaluate('document.querySelector(".account-address-button").click()');
  accountSettingsFail = true;
  await click('随机绑定');
  await wait('document.querySelector("dialog .error")?.innerText.includes("模拟地址绑定失败")');
  assert.equal(account.billing_address_id, 1);
  accountSettingsFail = false;
  await click('随机绑定');
  await wait('!document.querySelector("dialog") && document.querySelector(".account-address-button").innerText.includes("2 Test Street")');
  assert.equal(account.billing_address_id, 2);
  await evaluate('document.querySelector(".account-address-button").click()');
  await click('解除绑定');
  await wait('!document.querySelector("dialog") && document.querySelector(".account-address-button").innerText === "绑定地址"');
  assert.equal(account.billing_address_id, null);
  await writeFile(join(directory, 'account-settings.png'), Buffer.from((await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId)).data, 'base64'));
  await navigateAdmin('orders');
  // 未绑定及不可用的卡片不能提交，也不能在订单弹框改选其他卡。
  const boundCard = { payment_card_id: account.payment_card_id, payment_card_available: account.payment_card_available };
  for (const unavailable of [{ payment_card_id: null, payment_card_available: false }, { ...boundCard, payment_card_available: false }]) {
    Object.assign(account, unavailable);
    await click('录入充值订单');
    await select('订单账号', account.email);
    await wait('document.querySelector("dialog").innerText.includes("请先在账号管理中绑定可用的付款卡")');
    assert.equal(await evaluate('Array.from(document.querySelectorAll("dialog button")).find(b => b.textContent === "确认录入并记账").disabled'), true);
    assert.equal(await evaluate('Boolean(document.querySelector("button[aria-label=订单付款卡]"))'), false);
    await click('取消');
  }
  Object.assign(account, boundCard);
  await click('录入充值订单');
  await wait('!document.querySelector("button[aria-label=订单账号]").disabled');
  await select('订单账号',account.email);
  assert.ok(await evaluate('document.querySelector("input[aria-label=订单付款卡]").value.includes("运营测试卡")'));
  await select('订单套餐','测试 Plus · $200.00 / 1个月');
  assert.equal(await evaluate('Boolean(document.querySelector("input[name=period_start]"))'),false);
  await fill('input[aria-label="实收金额"]','1680.00');
  await wait('document.querySelector(".collection-preview")?.innerText.includes("16.67%")');
  assert.ok(await evaluate('document.querySelector(".collection-preview").innerText.includes("240.00")'));
  await select('收款币种','USD 美元');
  await fill('input[aria-label="实收金额"]','250.00');
  await wait('document.querySelector(".collection-preview")?.innerText.includes("20.00%")');
  await select('收款币种','CNY 人民币');
  await fill('input[aria-label="实收金额"]','1680.00');
  await wait('document.querySelector(".collection-preview")?.innerText.includes("16.67%")');
  await writeFile(join(directory,'collection-profit.png'),Buffer.from((await cdp.send('Page.captureScreenshot',{format:'png'},sessionId)).data,'base64'));
  assert.equal(await evaluate('document.querySelector("dialog").innerText.includes("查找付款卡")'),false);
  await wait('document.querySelector("input[aria-label=订单付款卡]")?.value.includes("运营测试卡")');
  assert.equal(await evaluate('document.querySelector("input[aria-label=订单付款卡]").readOnly'), true);
  assert.equal(await evaluate('Boolean(document.querySelector("button[aria-label=订单付款卡]"))'), false);
  assert.equal(await evaluate(`document.querySelector('input[aria-label="扣款 USD"]').value`),'200.00');
  assert.equal(await evaluate(`document.querySelector('input[aria-label="扣款 USD"]').readOnly`),true);
  await evaluate('document.querySelector("button[aria-label=订单来源]").click()');
  await fill('input[aria-label="过滤订单来源"]','  微信   老客户  ');
  await click('＋ 使用输入的来源');
  await wait('document.querySelector("input[name=order_source]")?.value === "微信 老客户"');
  assert.ok(await evaluate('document.querySelector("button[aria-label=订单来源]").getBoundingClientRect().top < document.querySelector("input[name=reference]").getBoundingClientRect().top'));
  await fill('input[name=reference]','official-paid-1');
  await fill('.evidence-editor textarea','official receipt', 'HTMLTextAreaElement');
  for (const mime of ['image/jpeg', 'image/jpg', '', 'application/octet-stream']) {
    await evaluate(`(() => {
      const canvas=document.createElement('canvas'); canvas.width=20; canvas.height=20;
      const uri=canvas.toDataURL('image/jpeg');
      const transfer=new DataTransfer();
      transfer.items.add(new File([Uint8Array.from(atob(uri.split(',')[1]),c=>c.charCodeAt(0))],'receipt.JPG',{type:${JSON.stringify(mime)}}));
      const input=document.querySelector('.evidence-editor input[type=file]'); input.files=transfer.files; input.dispatchEvent(new Event('change',{bubbles:true}));
    })()`);
    await wait('document.querySelector(".evidence-block img")?.src.startsWith("data:image/jpeg;base64,") && !document.querySelector(".evidence-editor [role=status]")');
    assert.equal(await evaluate('Boolean(document.querySelector(".evidence-editor [role=alert]"))'),false);
    await evaluate(`document.querySelector('button[aria-label="删除第 2 块"]').click()`);
    await click('确认删除');
    await wait('!document.querySelector(".evidence-block img")');
  }
  await evaluate(`(() => {
    const transfer=new DataTransfer();transfer.items.add(new File(['not an image'],'fake.jpg',{type:'image/jpeg'}));
    document.querySelector('.evidence-editor').dispatchEvent(new DragEvent('drop',{bubbles:true,cancelable:true,dataTransfer:transfer}));
  })()`);
  await wait('document.querySelector(".evidence-editor [role=alert]")?.innerText.includes("检查文件格式")');
  assert.equal(await evaluate('document.querySelector(".evidence-editor textarea").value'),'official receipt');
  const pasteEvidenceImage = () => evaluate(`(() => {
    const canvas=document.createElement('canvas'); canvas.width=240; canvas.height=100;
    const context=canvas.getContext('2d'); context.fillStyle='#ede9fe'; context.fillRect(0,0,240,100); context.fillStyle='#322478'; context.font='16px sans-serif'; context.fillText('Test receipt USD 150.00',20,55);
    const transfer=new DataTransfer(); const uri=canvas.toDataURL('image/png');
    transfer.items.add(new File([Uint8Array.from(atob(uri.split(',')[1]),c=>c.charCodeAt(0))],'receipt.png',{type:'image/png'}));
    document.querySelector('.evidence-editor').dispatchEvent(new ClipboardEvent('paste',{bubbles:true,cancelable:true,clipboardData:transfer}));
  })()`);
  await pasteEvidenceImage();
  await wait('document.querySelectorAll(".evidence-block img").length===1 && !document.querySelector(".evidence-editor [role=status]")');
  assert.ok(await evaluate('document.querySelector(".evidence-block img").getBoundingClientRect().height <= 88'));
  await evaluate('document.querySelector(".evidence-editor .image-thumbnail").click()');
  await wait('document.querySelectorAll("dialog[open]").length===2');
  await cdp.send('Input.dispatchKeyEvent', {type:'keyDown',key:'Escape',code:'Escape',windowsVirtualKeyCode:27},sessionId);
  await wait('document.querySelectorAll("dialog[open]").length===1');
  assert.equal(await evaluate('document.querySelector("input[name=reference]").value'),'official-paid-1');
  await click('文字');
  await fill('textarea[aria-label="凭据文字 3"]','after image','HTMLTextAreaElement');
  await evaluate(`document.querySelector('button[aria-label="上移第 3 块"]').click()`);
  await wait(`document.querySelector('textarea[aria-label="凭据文字 2"]')?.value === "after image"`);
  await evaluate(`document.querySelector('button[aria-label="删除第 2 块"]').click()`);
  await click('确认删除');
  assert.ok(await evaluate('document.querySelector(".order-record-dialog .workspace-dialog-body").scrollHeight <= document.querySelector(".order-record-dialog .workspace-dialog-body").clientHeight + 2'), '桌面录入信息与单张凭据应完整展示');
  const evidenceShot=await cdp.send('Page.captureScreenshot',{format:'png'},sessionId);
  await writeFile(join(directory,'order-evidence.png'),Buffer.from(evidenceShot.data,'base64'));
  await click('确认录入并记账');
  await wait('!document.querySelector("dialog") && document.body.innerText.includes("开通完成")');
  assert.equal(operationWrites.at(-1).received_currency,'CNY');
  assert.equal(operationWrites.at(-1).received_amount,'1680.00');
  assert.equal(operationWrites.at(-1).collection_rate_id,99);
  assert.equal(operationWrites.length,1);
  assert.equal(operationWrites[0].order_source,'微信 老客户');
  assert.equal(await evaluate('document.querySelector("table[aria-label=充值订单] thead th:nth-child(2)").textContent'),'订单来源');
  assert.equal(await evaluate('document.querySelector("table[aria-label=充值订单] tbody td:nth-child(2)").textContent'),'微信 老客户');
  await click('刷新');
  await click('录入充值订单');
  await select('订单来源','微信 老客户','微信');
  assert.equal(await evaluate('document.querySelector("input[name=order_source]").value'),'微信 老客户');
  await click('取消');
  assert.equal(operationWrites.length,1);

  assert.ok(await evaluate('document.querySelector("table[aria-label=充值订单]").innerText.includes("16.67%")'));
  assert.equal(await evaluate('Array.from(document.querySelectorAll(".row-actions button")).some(b=>["收款 / 钱包付款","官网扣款"].includes(b.textContent))'),false);

  assert.equal(await evaluate('Array.from(document.querySelectorAll(".row-actions button")).some(b=>b.textContent==="核验开通")'),false);
  await click('退款');
  assert.equal(await evaluate('Boolean(document.querySelector("dialog input[name=amount_usd]"))'),false);
  await fill('input[name=reference]','refund-paid-1');
  await fill('textarea[name=reason]','partial refund', 'HTMLTextAreaElement');
  await fill('.evidence-editor textarea','refund receipt', 'HTMLTextAreaElement');
  await pasteEvidenceImage();
  await wait('document.querySelectorAll(".evidence-block img").length===1 && !document.querySelector(".evidence-editor [role=status]")');
  await click('确认并保存');
  await wait('!document.querySelector("dialog") && document.body.innerText.includes("已退款")');
  assert.deepEqual(operationWrites.slice(1).map(v=>v.action),['refund_note']);
  assert.equal(operationWrites[0].expected_sale_usd_minor,20000);
  assert.equal(operationWrites[0].card_id,9);
  assert.equal(operationWrites[0].amount_usd,undefined);
  assert.equal(operationWrites[0].period_start,undefined);
  assert.deepEqual(await evaluate('Array.from(document.querySelectorAll("table[aria-label=充值订单] .row-actions button"), b => b.textContent)'), ['详情']);
  assert.equal(operationWrites.at(-1).amount_usd,undefined);
  assert.equal(rechargeOrders[0].payment_status,'paid');
  assert.equal(rechargeOrders[0].refunded_usd_minor,0);
  const proof=JSON.parse(operationWrites[0].evidence);
  assert.deepEqual(proof.blocks.map(b=>b.type),['text','image']);
  assert.ok(proof.blocks[1].src.startsWith('data:image/png;base64,'));
  await click('详情');
  await wait('document.querySelectorAll("dialog .evidence-view img").length===3');
  assert.ok(await evaluate('document.querySelector("dialog").innerText.includes("订单来源：微信 老客户")'));
  assert.ok(await evaluate('document.querySelector("dialog").innerText.includes("生效日期：2030-01-01")'));
  assert.ok(await evaluate('document.querySelector("dialog").innerText.includes("到期日期：2030-02-01")'));
  assert.equal(await evaluate('document.querySelector("dialog").innerText.includes("退款登记（余额未退回）")'),true);
  await evaluate('document.querySelector("dialog button[aria-label=关闭]").click()');
  assert.ok(operationWrites.every(v=>v.request_key));
  rechargeOrders[0] = {...rechargeOrders[0], order_status: 'active', payment_status: 'unpaid', fulfillment_status: 'pending', cost_usd_minor: 0};
  await click('刷新');
  await click('废弃');
  assert.equal(operationWrites.at(-1).action, 'refund_note');
  await fill('textarea[name=reason]', 'duplicate order', 'HTMLTextAreaElement');
  await click('取消');
  assert.equal(operationWrites.at(-1).action, 'refund_note');
  await click('废弃');
  await fill('textarea[name=reason]', 'duplicate order', 'HTMLTextAreaElement');
  await click('确认并保存');
  await wait('!document.querySelector("dialog") && document.body.innerText.includes("已废弃")');
  assert.equal(operationWrites.at(-1).action, 'discard');
  assert.deepEqual(await evaluate('Array.from(document.querySelectorAll("table[aria-label=充值订单] .row-actions button"), b => b.textContent)'), ['详情']);
  await writeFile(join(directory,'order-status.png'),Buffer.from((await cdp.send('Page.captureScreenshot',{format:'png'},sessionId)).data,'base64'));
  await cdp.send('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true},sessionId);
  assert.ok(await evaluate('document.documentElement.scrollWidth<=innerWidth'));
  await writeFile(join(directory,'recharge-orders-mobile.png'),Buffer.from((await cdp.send('Page.captureScreenshot',{format:'png'},sessionId)).data,'base64'));
  await cdp.send('Emulation.setDeviceMetricsOverride',{width:1280,height:900,deviceScaleFactor:1,mobile:false},sessionId);
  // 实收必填且必须有效；旧已收款订单补录只填写卡片支出。
  rechargeOrders=[];
  await click('刷新');
  await wait('document.body.innerText.includes("暂无充值订单")');
  await click('录入充值订单');
  await select('订单账号',account.email);
  await select('订单套餐','测试 Plus · $200.00 / 1个月');
  await wait('document.querySelector("input[aria-label=订单付款卡]")?.value.includes("运营测试卡")');
  assert.equal(await evaluate('document.querySelector("input[aria-label=订单付款卡]").readOnly'), true);
  assert.equal(await evaluate('Boolean(document.querySelector("button[aria-label=订单付款卡]"))'), false);

  await fill('input[name=reference]','no-receipt-payment');
  await fill('.evidence-editor textarea','card payment proof','HTMLTextAreaElement');
  const writesBeforeRequired = operationWrites.length;
  assert.equal(await evaluate('document.querySelector("input[aria-label=实收金额]").required'),true);
  const recordDisabled = 'Array.from(document.querySelectorAll("dialog button")).find(b=>b.textContent==="确认录入并记账").disabled';
  for (const amount of ['', '0', '-1', 'abc']) {
    await fill('input[aria-label="实收金额"]',amount);
    assert.equal(await evaluate(recordDisabled),true);
  }
  await select('收款币种','USD 美元');
  await fill('input[aria-label="实收金额"]','250.00');
  await wait('document.querySelector(".collection-preview")?.innerText.includes("20.00%")');
  assert.equal(await evaluate(recordDisabled),false);
  await fill('input[aria-label="实收金额"]','');
  assert.equal(await evaluate(recordDisabled),true);
  await evaluate('Array.from(document.querySelectorAll("dialog button")).find(b=>b.textContent==="确认录入并记账").click()');
  assert.equal(operationWrites.length,writesBeforeRequired);
  await fill('input[aria-label="实收金额"]','250.00');
  await wait('document.querySelector(".collection-preview")?.innerText.includes("20.00%")');
  await click('确认录入并记账');
  await wait('!document.querySelector("dialog") && document.body.innerText.includes("开通完成")');
  assert.equal(operationWrites.at(-1).received_currency,'USD');
  assert.equal(operationWrites.at(-1).received_amount,'250.00');
  rechargeOrders[0]={...rechargeOrders[0],payment_status:'unpaid',received_currency:'',received_amount_minor:0,received_usd_minor:0,cost_usd_minor:0,fulfillment_status:'pending',profit:null};
  await click('刷新');
  await click('补录订单');
  assert.equal(await evaluate('document.querySelector("input[aria-label=实收金额]").required'),true);
  assert.equal(await evaluate(recordDisabled),true);
  await click('取消');
  assert.equal(await evaluate('Array.from(document.querySelectorAll(".row-actions button")).some(b=>b.textContent==="核验开通")'),false);
  rechargeOrders[0]={...rechargeOrders[0],payment_status:'paid',received_currency:'USD',received_amount_minor:25000,received_usd_minor:25000,cost_usd_minor:0,fulfillment_status:'pending'};
  await click('刷新');
  await click('补录订单');
  assert.equal(await evaluate('Boolean(document.querySelector("input[aria-label=实收金额]"))'),false);
  assert.ok(await evaluate('document.querySelector("dialog").innerText.includes("250.00")'));
  await wait('document.querySelector("input[aria-label=订单付款卡]")?.value.includes("运营测试卡")');
  assert.equal(await evaluate('document.querySelector("input[aria-label=订单付款卡]").readOnly'), true);
  assert.equal(await evaluate('Boolean(document.querySelector("button[aria-label=订单付款卡]"))'), false);
  await evaluate('document.querySelector("button[aria-label=订单来源]").click()');
  await fill('input[aria-label="过滤订单来源"]','合作渠道');
  await click('＋ 使用输入的来源');
  await fill('input[name=reference]','legacy-payment');
  await fill('.evidence-editor textarea','legacy card proof','HTMLTextAreaElement');
  await click('确认录入并记账');
  await wait('!document.querySelector("dialog") && document.body.innerText.includes("开通完成")');
  assert.equal(operationWrites.at(-1).action,'record');
  assert.equal(operationWrites.at(-1).order_source,'合作渠道');
  assert.equal(operationWrites.at(-1).received_amount,undefined);
  assert.equal(rechargeOrders[0].received_usd_minor,25000);
  await navigateAdmin('packages');
  await wait('document.body.innerText.includes("测试 Plus")');
  await click('编辑');
  await select('美元定价方式','PHP 每日汇率折算');
  await fill('input[name=original_amount]','999.00');
  await wait('document.querySelector("input[aria-label=\\"折算 USD\\"]")?.value === "15.87"');
  assert.equal(await evaluate('document.querySelector("input[aria-label=\\"折算 USD\\"]").readOnly'),true);
  assert.equal(await evaluate('document.querySelector("input[name=currency]").readOnly'),true);
  await click('确认并保存');
  await wait('!document.querySelector("dialog") && document.body.innerText.includes("每日汇率折算") && document.body.innerText.includes("$15.87")');
  assert.equal(await evaluate('document.querySelector(".admin-content").innerText.includes("人民币 CNY")'),true);
  assert.equal(await evaluate('document.querySelector(".admin-content").innerText.includes("106.59")'),true);
  assert.equal(rechargePackages[0].auto_usd,true);
  assert.equal(rechargePackages[0].original_amount_minor,99900);
  phpRate='0.02000';phpCNYRate='0.12000';
  await click('刷新');
  await wait('document.body.innerText.includes("$19.98") && document.body.innerText.includes("119.88")');
  assert.equal(rechargeOrders[0].sale_usd_minor,20000);
  await writeFile(join(directory,'php-package-pricing.png'),Buffer.from((await cdp.send('Page.captureScreenshot',{format:'png'},sessionId)).data,'base64'));
  bankCards=[];
  await navigateAdmin('accounts');
  await wait('Boolean(document.querySelector(".account-toolbar"))');
  const sidebarShot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'admin-sidebar.png'), Buffer.from(sidebarShot.data, 'base64'));
  const userListRequestsBeforeBinding = requests.filter(url => new URL(url).pathname === '/api/users').length;
  await click('绑定用户');
  await wait('Boolean(document.querySelector(".account-owner-dialog input[name=owner_email]"))');
  assert.equal(await evaluate('document.querySelectorAll(".account-owner-dialog select,.account-owner-dialog [role=combobox],.account-owner-dialog datalist").length'), 0);
  assert.equal(await evaluate('document.querySelector(".account-owner-dialog .primary").disabled'), true);
  await fill('input[name=owner_email]', 'recipient');
  await click('查找用户');
  await delay(100);
  assert.equal(ownerLookups.length, 0, '不完整邮箱不能发起查找');
  await fill('input[name=owner_email]', 'recipient@example.co');
  await click('查找用户');
  await wait('document.querySelector(".account-owner-dialog .error")?.innerText.includes("未找到")');
  assert.equal(await evaluate('document.querySelector(".account-owner-dialog .primary").disabled'), true);
  await fill('input[name=owner_email]', 'RECIPIENT@EXAMPLE.COM');
  await click('查找用户');
  await wait('document.querySelector(".account-owner-match")?.innerText.includes("recipient@example.com")');
  assert.deepEqual(ownerLookups.at(-1), { email: 'recipient@example.com' });
  assert.equal(ownerWrites.length, 0, '查找成功不能自动绑定');
  await fill('input[name=owner_email]', 'other@example.com');
  assert.equal(await evaluate('Boolean(document.querySelector(".account-owner-match"))'), false);
  assert.equal(await evaluate('document.querySelector(".account-owner-dialog .primary").disabled'), true, '修改邮箱必须重新查找');
  await click('取消');
  await wait('!document.querySelector("dialog")');
  assert.equal(ownerWrites.length, 0);
  await click('绑定用户');
  await fill('input[name=owner_email]', 'recipient@example.com');
  await click('查找用户');
  await wait('!document.querySelector(".account-owner-dialog .primary").disabled');
  await writeFile(join(directory, 'account-owner-binding.png'), Buffer.from((await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId)).data, 'base64'));
  await click('确认绑定');
  await wait('!document.querySelector("dialog") && document.querySelector(".account-owner")?.innerText.includes("recipient@example.com")');
  assert.deepEqual(ownerWrites[0], { email: 'recipient@example.com', user_id: 4, expected_owner_id: 1 });
  await cdp.send('Page.reload', {}, sessionId);
  await wait('document.querySelector(".account-owner")?.innerText.includes("recipient@example.com")');
  await click('绑定用户');
  await fill('input[name=owner_email]', 'recipient@example.com');
  await click('查找用户');
  await wait('document.querySelector(".account-owner-match")?.innerText.includes("无需重复绑定")');
  assert.equal(await evaluate('document.querySelector(".account-owner-dialog .primary").disabled'), true);
  await fill('input[name=owner_email]', 'member@example.com');
  await click('查找用户');
  await wait('!document.querySelector(".account-owner-dialog .primary").disabled');
  await click('确认绑定');
  await wait('!document.querySelector("dialog") && document.querySelector(".account-owner")?.innerText.includes("member@example.com")');
  assert.equal(ownerWrites.length, 2);
  assert.equal(requests.filter(url => new URL(url).pathname === '/api/users').length, userListRequestsBeforeBinding, '绑定操作不能下载用户列表');
  await navigateAdmin('users');
  await wait('Boolean(document.querySelector(".user-manager tbody tr"))');
  assert.equal(await evaluate('document.querySelectorAll(".user-manager tbody button[role=combobox]").length'), 1);
  await select('用户 member@example.com 的角色', '管理员');
  await wait('document.querySelector("dialog")?.innerText.includes("确认修改角色")');
  assert.equal(roleWrites.length, 0);
  await click('取消');
  await wait('!document.querySelector("dialog")');
  await select('用户 member@example.com 的角色', '管理员');
  await click('确认修改');
  await wait('!document.querySelector("dialog") && document.querySelector(".user-manager tbody button[role=combobox]").innerText.includes("管理员")');
  assert.deepEqual(roleWrites, [{ role: 'admin' }]);
  const usersShot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'admin-users.png'), Buffer.from(usersShot.data, 'base64'));
  await cdp.send('Page.reload', {}, sessionId);
  await wait('Boolean(document.querySelector(".user-manager tbody tr"))');
  await navigateAdmin('accounts');
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
  await navigateAdmin('accounts');
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
  await select('分组所属用户', 'member@example.com');
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
  assert.equal(await evaluate('Array.from(document.querySelectorAll(".account-section button")).some(button => button.textContent === "批量导入")'), false);
  await click('添加账号');
  await wait('Boolean(document.querySelector("dialog textarea"))');
  assert.equal(await evaluate('Boolean(document.querySelector("dialog input[name=email]"))'), false);
  await fill('textarea', JSON.stringify({ accessToken: 'test-only' }), 'HTMLTextAreaElement');
  await wait('document.querySelector("dialog input[name=email]")?.required');
  const emailToken = 'header.' + Buffer.from(JSON.stringify({ 'https://api.openai.com/profile': { email: 'jwt@example.com' } })).toString('base64url') + '.signature';
  await fill('textarea', JSON.stringify({ user: { email: 'other@example.com' }, accessToken: emailToken }), 'HTMLTextAreaElement');
  await wait('!document.querySelector("dialog input[name=email]") && document.querySelector("dialog").innerText.includes("jwt@example.com")');
  await fill('textarea', '', 'HTMLTextAreaElement');
  assert.equal(await evaluate('Boolean(document.querySelector("dialog input[name=email]"))'), false);
  const raw = JSON.stringify({ user: { email: 'chat@example.com', name: '工作账号' }, accessToken: 'test-only', sessionToken: 'test-cookie' });
  await fill('textarea', raw, 'HTMLTextAreaElement');
  await wait('document.querySelector("dialog").innerText.includes("识别到账号：chat@example.com")');
  const dropFiles = files => evaluate(`(() => {
    const transfer = new DataTransfer();
    for (const file of ${JSON.stringify(files)}) transfer.items.add(new File([file.content], file.name, { type: 'application/json' }));
    const field = document.querySelector('.json-field');
    field.dispatchEvent(new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer: transfer }));
    return !field.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: transfer }));
  })()`);
  for (const [files, message] of [
    [[{ name: 'session.txt', content: raw }], '请选择 .json 文件'],
    [[{ name: 'session.json', content: '{broken' }], '无法读取有效 JSON'],
    [[{ name: 'session.json', content: '[]' }], '单个非空 Session 对象'],
    [[{ name: 'session.json', content: 'x'.repeat(240001) }], '不能超过 240 KB'],
    [[{ name: 'one.json', content: raw }, { name: 'two.json', content: raw }], '一次拖入一个 JSON 文件'],
  ]) {
    assert.equal(await dropFiles(files), true);
    await wait(`document.querySelector('.json-field .error')?.textContent.includes(${JSON.stringify(message)})`);
    assert.equal(await evaluate('document.querySelector("textarea").value'), raw);
  }
  await fill('textarea', '', 'HTMLTextAreaElement');
  assert.equal(await dropFiles([{name:'session.json',content:'\uFEFF'+raw}]),true);
  await wait(`document.querySelector('textarea').value === ${JSON.stringify(raw)} && !document.querySelector('.json-field .error')`);
  await wait('document.querySelector("dialog").innerText.includes("识别到账号：chat@example.com")');
  assert.equal(imported.length,0);
  assert.equal(await evaluate('Boolean(document.querySelector("input[name=email]"))'), false);
  await click('格式化 JSON');
  assert.ok((await evaluate('document.querySelector("textarea").value')).includes('\n'));
  assert.ok(await evaluate('Boolean(document.querySelector(".json-key")) && Boolean(document.querySelector(".json-string"))'));
  assert.equal(await evaluate('Boolean(document.querySelector("dialog input[name=session_cookie],dialog input[name=label]"))'), false);
  const jsonEditorShot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'json-editor.png'), Buffer.from(jsonEditorShot.data, 'base64'));
  await click('保存');
  await wait('!document.querySelector("dialog")');
  assert.equal(imported.length, 1); assert.deepEqual(JSON.parse(imported[0].session_json), JSON.parse(raw)); assert.equal(imported[0].email, ''); assert.equal(imported[0].label, undefined);
  await navigateAdmin('addresses');
  await wait('document.querySelectorAll(".address-row").length === 20');
  assert.equal(await evaluate('document.querySelector(".address-row td:nth-child(3)").textContent'), 'Oregon');
  await evaluate('document.querySelector(".sidebar-collapse").click()');
  await wait('document.querySelector(".admin-content").getBoundingClientRect().left === 64');
  assert.equal(await evaluate('localStorage.getItem("admin-sidebar-collapsed")'), 'true');
  await cdp.send('Page.reload', {}, sessionId);
  await wait('document.querySelectorAll(".address-row").length === 20 && document.querySelector(".admin-content").getBoundingClientRect().left === 64');
  const collapsedShot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'sidebar-collapsed.png'), Buffer.from(collapsedShot.data, 'base64'));
  assert.equal(await evaluate('Array.from(document.querySelectorAll(".workspace-nav a")).some(link => link.checkVisibility())'), false);
  assert.equal(await evaluate('Array.from(document.querySelectorAll(".sidebar-group-toggle")).filter(button => button.checkVisibility()).length'), 4);
  await click('充值管理');
  await wait('document.querySelector(".admin-content").getBoundingClientRect().left === 208 && document.querySelector(".workspace-nav a[href$=orders]").checkVisibility()');
  assert.equal(await evaluate('localStorage.getItem("admin-sidebar-collapsed")'), 'false');
  const groupedShot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'sidebar-grouped.png'), Buffer.from(groupedShot.data, 'base64'));
  await evaluate(`document.querySelector('.pagination-number[aria-label="第 2 页"]').click()`);
  await wait('document.querySelectorAll(".address-row").length === 1');
  await select('每页条数', '50 条 / 页');
  await wait('document.querySelectorAll(".address-row").length === 21');
  assert.ok(requests.some(value => value.includes('/addresses?') && value.includes('page_size=50') && value.includes('page=1')));
  await select('每页条数', '20 条 / 页');
  await wait('document.querySelectorAll(".address-row").length === 20');
  for (const query of ['111', '[Road]', 'Oregon']) {
    await fill('input[aria-label=搜索地址]', query); await click('搜索');
    await wait(`document.querySelector('.address-table mark')?.textContent === ${JSON.stringify(query)}`);
    if (query === '111') {
      const highlightShot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
      await writeFile(join(directory, 'address-highlight.png'), Buffer.from(highlightShot.data, 'base64'));
    }
  }
  await fill('input[aria-label=搜索地址]', ''); await click('搜索');
  await wait('document.querySelectorAll(".address-row").length === 20');
  assert.equal(await evaluate('Boolean(document.querySelector(".address-row a"))'), false);
  await click('完整资料');
  await wait('Boolean(document.querySelector("dialog .address-source-details[open]"))');
  const addressDetailsShot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'address-details.png'), Buffer.from(addressDetailsShot.data, 'base64'));
  assert.equal(await evaluate('document.querySelector(".address-source-details").innerText.includes("Preserved value")'), true);
  assert.equal(await evaluate('document.querySelector(".address-source-details").innerText.includes("生成安全码")'), true);
  await evaluate('document.querySelector("dialog button[aria-label=关闭]").click()');
  assert.deepEqual(await evaluate('Array.from(document.querySelectorAll(".address-table thead th"), item => item.textContent)'), ['姓名', '国家 / 地区', '州', '城市', '街道', '邮编', '电话', '操作']);
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
  await click('资源管理');
  await wait('!document.querySelector(".workspace-nav a[href$=addresses]").checkVisibility()');
  await evaluate('document.querySelector("button[aria-controls=sidebar-group-resources]").focus()');
  assert.equal(await evaluate('document.activeElement.getAttribute("aria-controls")'), 'sidebar-group-resources');
  await cdp.send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Enter', code: 'Enter', text: '\r', windowsVirtualKeyCode: 13 }, sessionId);
  await cdp.send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13 }, sessionId);
  await wait('document.querySelector(".workspace-nav a[href$=addresses]").checkVisibility()');
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
  await navigateAdmin('accounts');
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
  assert.equal(await evaluate('document.querySelector("dialog button[aria-label=网络连接]").textContent'), '直连（不使用代理）');
  await click('打开浏览器');
  await wait('Boolean(document.querySelector(".two-factor input[type=password]"))');
  assert.equal(launches.length,0);
  await fill('.two-factor input[type=password]', 'test-password');
  await click('生成绑定二维码');
  await wait('Boolean(document.querySelector(".totp-qr"))');
  assert.equal(await evaluate('document.querySelector(".totp-qr").src.startsWith("data:image/png")'),true);
  await writeFile(join(directory,'two-factor-setup.png'),Buffer.from((await cdp.send('Page.captureScreenshot',{format:'png'},sessionId)).data,'base64'));
  await fill('.two-factor input[inputmode=numeric]', '123456');
  await click('确认绑定');
  await wait('document.querySelector(".two-factor")?.innerText.includes("验证器已绑定")');
  await evaluate('document.querySelector("dialog button[aria-label=关闭]").click()');
  assert.ok(!requests.some(url => url.includes('/browser-session')));
  assert.equal(localLaunches.length, 0);
  assert.equal(requests.some(url => new URL(url).pathname === '/api/accounts/1/browser'), false, '浏览器管理不能请求服务器浏览器配置');
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true }, sessionId);

  // 普通用户只有账号入口，管理列、行操作和其他路由均不可访问。
  user.id = 1; user.role = 'user'; user.username = '';
  await cdp.send('Page.reload', {}, sessionId);
  await wait('document.body.innerText.includes("工作账号")');
  assert.deepEqual(await evaluate('Array.from(document.querySelectorAll(".workspace-nav a"),a=>a.getAttribute("href"))'),['/admin/accounts']);
  assert.deepEqual(await evaluate('Array.from(document.querySelectorAll(".accounts-table th"),th=>th.textContent)'),['账号','上次登录（UTC+8）']);
  assert.equal(await evaluate('Boolean(document.querySelector(".account-owner-bind"))'), false, '普通用户不能分配账号');
  assert.equal(await evaluate('Boolean(document.querySelector(".account-notes"))'), false);
  assert.equal(await evaluate('Boolean(document.querySelector(".account-product"))'), false);
  assert.equal(await evaluate('Array.from(document.querySelectorAll("button")).some(button=>button.textContent==="打开空白浏览器")'), false);
  assert.equal(await evaluate('Boolean(document.querySelector(".account-actions,.account-toolbar,.stats"))'),false);
  assert.equal(await evaluate('Boolean(document.querySelector(".user-menu a[href$=wallet]"))'),false);
  const requestMark = requests.length, localMark = localRequests.length;
  await delay(2200);
  assert.equal(localRequests.length,localMark);
  assert.equal(exportCount,0);
  assert.equal(await evaluate('document.querySelector(".user-accounts-table").scrollWidth <= document.querySelector(".data-table-wrap").clientWidth'),true);
  await writeFile(join(directory,'user-accounts.png'),Buffer.from((await cdp.send('Page.captureScreenshot',{format:'png'},sessionId)).data,'base64'));
  await cdp.send('Page.navigate',{url:'http://127.0.0.1:'+server.address().port+'/admin/bank-cards'},sessionId);
  await wait('document.body.innerText.includes("无权访问此页面")');
  assert.equal(requests.slice(requestMark).some(url=>url.includes('/api/bank-cards')),false);
  user.role = 'admin';
  await cdp.send('Page.navigate',{url:'http://127.0.0.1:'+server.address().port+'/admin/accounts'},sessionId);
  await wait('Boolean(document.querySelector(".account-toolbar"))');
  await navigateAdmin('bank-cards');
  await wait('Boolean(document.querySelector(".bank-card-manager"))');
  await click('添加银行卡');
  await fill('dialog input[name=label]', '工作卡');
  await fill('dialog input[name=cardholder]', 'Test User');
  await fill('dialog input[name=number]', '4242424242424242');
  await fill('dialog input[name=cvc]', '0042');
  const uploadWalletQR = method => evaluate(`(() => {
    const canvas=document.createElement('canvas');canvas.width=240;canvas.height=240;
    const ctx=canvas.getContext('2d');ctx.fillStyle='white';ctx.fillRect(0,0,240,240);ctx.fillStyle='black';ctx.fillRect(20,20,60,60);ctx.fillRect(160,20,60,60);ctx.fillRect(20,160,60,60);
    const uri=canvas.toDataURL('image/png');window.testWalletQR=uri;
    const transfer=new DataTransfer();transfer.items.add(new File([Uint8Array.from(atob(uri.split(',')[1]),c=>c.charCodeAt(0))],'wallet.png',{type:'image/png'}));
    const group=document.querySelector('.image-upload');
    if (${JSON.stringify(method)}==='paste') group.dispatchEvent(new ClipboardEvent('paste',{bubbles:true,cancelable:true,clipboardData:transfer}));
    else group.dispatchEvent(new DragEvent('drop',{bubbles:true,cancelable:true,dataTransfer:transfer}));
  })()`);
  await uploadWalletQR('paste');
  await wait('Boolean(document.querySelector(".image-upload img"))');
  await evaluate('document.querySelector(".image-upload .image-thumbnail").click()');
  await wait('document.querySelectorAll("dialog[open]").length===2');
  await cdp.send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Escape', code: 'Escape', windowsVirtualKeyCode: 27 }, sessionId);
  await wait('document.querySelectorAll("dialog[open]").length===1');
  assert.equal(await evaluate('document.querySelector("input[name=cvc]").value'), '0042');

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
  bankUploadTooLarge = true;
  await click('保存银行卡');
  await wait('document.querySelector("dialog .error")?.textContent.includes("提交内容超过服务器大小限制")');
  assert.equal(bankWrites.length, 0);
  assert.equal(await evaluate('document.querySelector(".image-upload img").src'), await evaluate('window.testWalletQR'));
  assert.equal(await evaluate('document.querySelector("dialog textarea[name=notes]").value'), '月度订阅\n仅工作用途');
  bankUploadTooLarge = false;
  await click('保存银行卡');
  await wait('!document.querySelector("dialog") && Boolean(document.querySelector(".bank-card-row"))');
  assert.equal(await evaluate('document.querySelector(".bank-card-row").innerText.includes("4242424242424242")'), false);
  assert.equal(await evaluate('document.querySelector(".bank-card-row").innerText.includes("**** **** **** 4242")'), true);
  assert.equal(requests.some(url => url.includes('include_numbers')), false, '列表不得请求完整卡号');
  assert.equal(await evaluate('document.querySelector(".bank-card-row").innerText.includes("***")'), true);
  assert.equal(await evaluate('document.querySelector(".bank-card-row").innerText.includes("0042")'), false);
  assert.equal(await evaluate('document.querySelector(".bank-card-row").tagName'), 'TR');
  assert.equal(await evaluate('document.documentElement.scrollWidth <= innerWidth'), true);
  assert.equal(bankCards[0].platform, '自定义卡平台');
  assert.equal(bankCards[0].cvc,'0042');
  assert.equal(bankCards[0].wallet_qr_image,await evaluate('window.testWalletQR'));
  await evaluate('document.querySelector(".bank-card-wallet .image-thumbnail").click()');
  await wait('Boolean(document.querySelector(".image-preview-dialog"))');
  assert.equal(await evaluate('document.querySelector(".image-full").src'),bankCards[0].wallet_qr_image);
  await evaluate('document.querySelector(".image-preview-dialog .close").click()');
  await wait('!document.querySelector("dialog")');

  assert.equal(bankCards[0].notes, '月度订阅\n仅工作用途');
  assert.ok(await evaluate('document.querySelector(".bank-card-row").innerText.includes("自定义卡平台") && document.querySelector(".bank-card-row").innerText.includes("仅工作用途")'));
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 1600, height: 900, deviceScaleFactor: 1, mobile: false }, sessionId);
  await evaluate('window.scrollTo(0,0)');
  await writeFile(join(directory, 'bank-cards-table.png'), Buffer.from((await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId)).data, 'base64'));
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true }, sessionId);
  twoFactorEnabled = true;
  await click('编辑');
  await wait('Boolean(document.querySelector("dialog input[aria-label=验证器验证码]"))');
  assert.equal(bankDetailReads, 0, '验证前不得请求卡详情');
  assert.equal(await evaluate('Boolean(document.querySelector("dialog input[name=number]"))'), false);
  await fill('dialog input[aria-label=验证器验证码]', '000000');
  await click('验证并编辑银行卡');
  await wait('document.querySelector("dialog .error")?.textContent.includes("验证码无效")');
  assert.equal(await evaluate('Boolean(document.querySelector("dialog input[name=number]"))'), false);
  await fill('dialog input[aria-label=验证器验证码]', '123456');
  await click('验证并编辑银行卡');
  await wait('Boolean(document.querySelector("dialog input[name=number]"))');
  assert.equal(await evaluate('document.querySelector("dialog input[name=number]").value'), '************4242');
  assert.equal(await evaluate('document.querySelector("dialog input[name=cvc]").value'), '');
  assert.equal(await evaluate('document.querySelector("dialog input[name=cvc]").type'), 'password');
  await fill('dialog input[name=label]', '工作卡仅改名称');
  await click('保存银行卡');
  await wait('!document.querySelector("dialog")');
  assert.equal(bankPayloads[0].number, undefined, '未修改卡号不得提交掩码');
  assert.equal(bankPayloads[0].cvc, undefined, '留空安全码不得覆盖原值');
  assert.equal(bankCards[0].number, '4242424242424242');
  assert.equal(bankCards[0].cvc, '0042');
  await click('编辑');
  await wait('Boolean(document.querySelector("dialog input[aria-label=验证器验证码]"))');
  assert.equal(await evaluate('Boolean(document.querySelector("dialog input[name=number]"))'), false, '重新编辑需重新验证');
  await fill('dialog input[aria-label=验证器验证码]', '123456');
  await click('验证并编辑银行卡');
  await wait('Boolean(document.querySelector("dialog input[name=number]"))');
  assert.equal(await evaluate('document.querySelector("dialog textarea[name=notes]").value'), '月度订阅\n仅工作用途');
  await select('卡平台', '未设置');
  await select('卡平台', '自定义卡平台', '自定义', true);
  assert.equal(await evaluate('document.querySelector("dialog input[name=cvc]").value'),'');
  assert.equal(await evaluate('document.querySelector(".image-upload img").src'),bankCards[0].wallet_qr_image);
  await click('移除截图'); await click('取消');
  assert.ok(await evaluate('Boolean(document.querySelector(".image-upload img"))'));
  await click('移除截图'); await click('确认移除');
  await wait('!document.querySelector(".image-upload img")');
  await uploadWalletQR('drop');
  await wait('Boolean(document.querySelector(".image-upload img"))');
  await fill('dialog input[name=cvc]','007');
  await fill('dialog textarea[name=notes]', '已修改备注', 'HTMLTextAreaElement');
  await fill('dialog input[name=label]', '工作卡已编辑');
  await click('保存银行卡');
  await wait('!document.querySelector("dialog") && document.querySelector(".bank-card-row").innerText.includes("工作卡已编辑")');
  assert.equal(bankCards[0].notes, '已修改备注');
  assert.equal(bankCards[0].cvc,'007');
  assert.equal(bankCards[0].wallet_qr_image,await evaluate('window.testWalletQR'));
  await click('删除'); await click('取消');
  assert.equal(bankWrites.filter(method => method === 'DELETE').length, 0);
  await click('删除'); await click('确认删除');
  await wait('!document.querySelector("dialog") && !document.querySelector(".bank-card-row")');
  assert.deepEqual(bankWrites, ['POST', 'PATCH', 'PATCH', 'DELETE']);
  bankCards = [{ id: 2, label: '对账测试卡', last4: '4242', brand: 'Visa', cardholder: 'Test User', exp_month: 12, exp_year: 2030, balance_usd_minor: 0 }];
  await click('刷新');
  await wait('document.querySelector(".bank-card-row")?.innerText.includes("对账测试卡")');
  await click('余额 / 对账单');
  await wait('document.querySelector("dialog")?.innerText.includes("暂无流水")');
  await click('记录存入');
  await fill('input[aria-label="记账金额 USD"]', '500.00');
  await fill('input[name=reference]', 'smoke-deposit');
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
  rechargePackages = [{ id: 1, name: '历史补录套餐', enabled: true, months: 1, currency: 'PHP', original_amount_minor: 891964, sale_usd_minor: 14219 }];
  await click('记录开通扣款');
  await fill('input[name=reference]', 'smoke-subscription');
  await fill('input[name=period_start]', '2030-01-01');
  await fill('input[name=period_end]', '2030-02-01');
  await wait('!document.querySelector("button[aria-label=补录套餐]")?.disabled');
  await select('补录套餐', '历史补录套餐 · PHP 8919.64 / 1个月');
  assert.equal(await evaluate('document.querySelector("input[name=original_amount]")'), null);
  assert.equal(await evaluate(`document.querySelector('input[aria-label="套餐扣款 USD"]').value`), '142.19');
  assert.equal(await evaluate(`document.querySelector('input[aria-label="套餐扣款 USD"]').readOnly`), true);
  await select('扣款关联账号', `${account.label} · ${account.email}`);
  await click('核对并记账');
  await wait('Boolean(document.querySelector(".card-ledger-confirm"))');
  assert.ok(await evaluate('document.querySelector(".card-ledger-confirm").innerText.includes("142.19")'));
  await click('返回修改');
  await select('实际扣款方式', '输入人民币金额');
  await fill('input[aria-label="记账金额 CNY"]', '1051.75');
  assert.equal(await evaluate('document.querySelector("input[aria-label=记账备注]").required'), false);
  await click('核对并记账');
  await wait('Boolean(document.querySelector(".card-ledger-confirm"))');
  assert.ok(await evaluate('document.querySelector(".card-ledger-confirm").innerText.includes("8919.64")'));
  assert.ok(await evaluate('document.querySelector(".card-ledger-confirm").innerText.includes("1051.75")'));
  await click('确认记账');
  await wait('document.querySelector(".card-ledger-table")?.innerText.includes("349.75")');
  assert.equal(bankCards[0].balance_usd_minor, 34975);
  assert.equal(ledgerWrites.at(-1).account_id, 1);
  assert.equal(ledgerWrites.at(-1).package_id, 1);
  assert.equal(ledgerWrites.at(-1).charge_mode, 'CNY');
  assert.equal(ledgerWrites.at(-1).notes, '');
  assert.equal(ledgerWrites.at(-1).expected_amount_usd_minor, 15025);
  rechargePackages[0].sale_usd_minor = 40000;
  await click('记录开通扣款');
  await wait('!document.querySelector("button[aria-label=补录套餐]")?.disabled');
  await select('补录套餐', '历史补录套餐 · PHP 8919.64 / 1个月');
  await select('扣款关联账号', `${account.label} · ${account.email}`);
  await fill('input[name=reference]', 'smoke-overdraft');
  await fill('input[name=period_start]', '2030-02-01');
  await fill('input[name=period_end]', '2030-03-01');
  await click('核对并记账');
  await wait('Boolean(document.querySelector(".card-ledger-confirm"))');
  assert.ok(await evaluate('document.querySelector(".card-ledger-confirm").innerText.includes("-$50.25")'));
  assert.ok(await evaluate('document.querySelector(".card-ledger-confirm").innerText.includes("历史补录允许负余额")'));
  await click('确认记账');
  await wait('document.querySelector(".card-ledger-table")?.innerText.includes("-$50.25")');
  assert.equal(await evaluate('document.querySelector("dialog").scrollWidth <= document.querySelector("dialog").clientWidth'), true);
  await writeFile(join(directory, 'card-ledger-mobile.png'), Buffer.from((await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId)).data, 'base64'));
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false }, sessionId);
  await writeFile(join(directory, 'card-ledger.png'), Buffer.from((await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId)).data, 'base64'));
  await evaluate('document.querySelector("dialog button[aria-label=关闭]").click()');
  await wait('!document.querySelector("dialog") && document.querySelector(".bank-card-row").innerText.includes("-$50.25")');
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true }, sessionId);
  await navigateAdmin('proxies');
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
  await select('每页条数', '50 条 / 页');
  await wait('document.querySelectorAll(".usage-row").length === 50');
  await click('下一页');
  await wait('document.querySelectorAll(".usage-row").length === 11');
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
  await navigateAdmin('accounts');
  await wait('Boolean(document.querySelector(".account-proxy button:not(:disabled)"))');
  await click('打开空白浏览器');
  await select('空白浏览器代理', '美国代理已编辑 · 203.0.113.10:1080');
  blankFail = true;
  await click('打开浏览器');
  await wait('document.querySelector("dialog").innerText.includes("模拟空白浏览器启动失败")');
  blankFail = false;
  await click('打开浏览器');
  await wait('!document.querySelector("dialog")');
  assert.deepEqual(blankLaunches, [{ proxy_id: 'proxy-1' }]);
  await select('账号 chat@example.com 的 SOCKS5', '美国代理已编辑 · 203.0.113.10', '美国');
  await wait('document.body.innerText.includes("代理选择已保存")');
  assert.equal(Object.values(bindings)[0], 'proxy-1');
  await click('浏览器管理');
  await wait('document.querySelector(".browser-status-row strong")?.textContent === "未打开"');
  assert.equal(await evaluate('document.querySelector("dialog button[aria-label=网络连接]").textContent'), '美国代理已编辑 · 203.0.113.10');
  proxyBindingFails = true;
  await select('网络连接', '直连（不使用代理）');
  await wait('document.querySelector("dialog").innerText.includes("模拟代理绑定保存失败")');
  assert.equal(await evaluate('document.querySelector("dialog button[aria-label=网络连接]").textContent'), '美国代理已编辑 · 203.0.113.10');
  assert.equal(await evaluate('document.querySelector(".account-proxy button").textContent'), '美国代理已编辑 · 203.0.113.10');
  proxyBindingFails = false;
  await select('网络连接', '直连（不使用代理）');
  await wait('document.querySelector(".account-proxy button").textContent === "直连（不使用代理）"');
  assert.equal(Object.values(bindings).length, 0);
  await select('网络连接', '美国代理已编辑 · 203.0.113.10');
  await wait('document.querySelector(".account-proxy button").textContent === "美国代理已编辑 · 203.0.113.10"');
  await evaluate('document.querySelector("dialog button[aria-label=关闭]").click()');
  await click('浏览器管理');
  await wait('document.querySelector(".browser-status-row strong")?.textContent === "未打开"');
  assert.equal(await evaluate('document.querySelector("dialog button[aria-label=网络连接]").textContent'), '美国代理已编辑 · 203.0.113.10');
  await evaluate('document.querySelector("dialog button[aria-label=关闭]").click()');
  await click('打开账号');
  await wait('Boolean(document.querySelector(".two-factor input[inputmode=numeric]"))');
  assert.equal(await evaluate('document.querySelector(".browser-assistant-option input").checked'), true);
  assert.equal(await evaluate('document.querySelector(".browser-assistant-option input").disabled'), false);
  assert.equal(await evaluate('document.querySelector(".browser-fingerprint").open'), false);
  await writeFile(join(directory, 'local-compact-mobile.png'), Buffer.from((await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId)).data, 'base64'));
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false }, sessionId);
  await writeFile(join(directory, 'local-compact-desktop.png'), Buffer.from((await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId)).data, 'base64'));
  assert.ok(await evaluate('document.querySelector("dialog").getBoundingClientRect().height < 600'));
  await cdp.send('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true }, sessionId);

  await evaluate('document.querySelector(".launcher-settings").open = true');
  assert.equal(await evaluate('document.querySelector("input[aria-label=本机连接端口]").value'), '15684');
  await fill('input[aria-label=本机连接端口]', '15685');
  await click('测试连接');
  await wait('document.querySelector(".launcher-settings .success")?.textContent.includes("连接成功")');
  await click('保存端口');
  await wait('localStorage.getItem("aitok.launcher.port") === "15685"');
  await evaluate('window.__originalConfirm = window.confirm; window.confirm = () => false');
  assert.equal(await evaluate('document.querySelector(".browser-fingerprint").open'), false);
  await evaluate('document.querySelector(".browser-fingerprint summary").click()');
  await click('重新随机生成指纹');
  assert.equal(fingerprintResets, 0);
  await evaluate('window.confirm = () => true');
  await click('重新随机生成指纹');
  await wait('document.querySelector("dialog").innerText.includes("第 2 代")');
  assert.equal(fingerprintResets, 1);
  await evaluate('window.confirm = window.__originalConfirm');
  await click('重新打开');
  await wait('Boolean(document.querySelector(".two-factor input[inputmode=numeric]"))');
  await fill('.two-factor input[inputmode=numeric]', '654321');
  await click('验证并打开浏览器');
  await wait('document.querySelector(".browser-status-row strong")?.textContent === "已打开"');
  assert.equal(await evaluate('document.querySelector("dialog").innerText.includes("配对密钥")'), false);
  assert.equal(exportCount, 1); assert.equal(localLaunches.length, 1);
  assert.equal(localLaunches[0].session.accessToken, 'local-test-access');
  assert.equal(localLaunches[0].expected_email, account.email);
  assert.equal(localLaunches[0].assistant_token, 'local-test-assistant');
  assert.equal(await evaluate('document.querySelector(".browser-assistant-option input").disabled'), true);
  assert.ok(localRequests.some(request => request.url.startsWith('http://127.0.0.1:15685/browsers') && request.method === 'POST'));
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
  await wait('Boolean(document.querySelector(".account-actions"))');
  await click('刷新浏览器状态');
  await wait('document.querySelector(".account-actions")?.innerText.includes("关闭浏览器")');
  assert.equal(await evaluate('localStorage.getItem("aitok.launcher.port")'), '15685');
  assert.equal(localLaunches.length, 1);
  await click('关闭浏览器');
  await wait('document.querySelector(".account-actions").innerText.includes("打开账号")');
  assert.equal(localState, 'closed');
  await click('浏览器管理');
  await wait('document.querySelector(".browser-status-row strong")?.textContent === "未打开"');
  await click('打开浏览器');
  await wait('Boolean(document.querySelector(".two-factor input[inputmode=numeric]"))');
  await evaluate('document.querySelector(".browser-assistant-option input").click()');
  assert.equal(await evaluate('document.querySelector(".browser-assistant-option input").checked'), false);
  await fill('.two-factor input[inputmode=numeric]', '654321');
  await click('验证并打开浏览器');
  await wait('document.querySelector(".browser-status-row strong")?.textContent === "已打开"');
  assert.equal(localLaunches.length, 2);
  assert.equal(Object.hasOwn(localLaunches[1], 'assistant_token'), false);
  assert.equal(localLaunches[1].session.accessToken, 'local-test-access');
  assert.equal(localLaunches[1].expected_email, account.email);
  authenticatedAt = '2026-09-15T00:30:00Z'; localState = 'authenticated';
  const openStatusCount = statusRequestCount();
  await delay(5200);
  assert.equal(statusRequestCount(), openStatusCount, '运行中的账号及打开的弹窗不能自动轮询');
  assert.equal(loginWrites, 0, '登录状态在手动刷新时同步');
  await click('刷新状态');
  await wait('document.querySelector(".last-login")?.innerText.includes("08:30:00")');
  assert.equal(loginWrites, 1);
  const beforeSecondRefresh = statusRequestCount();
  await click('刷新状态');
  for (let attempt = 0; attempt < 80 && statusRequestCount() === beforeSecondRefresh; attempt++) await delay(100);
  assert.equal(statusRequestCount(), beforeSecondRefresh + 1);
  await wait('!document.querySelector(".local-browser-session .browser-buttons button").disabled');
  assert.equal(loginWrites, 1, '重复刷新不能重复记录登录');
  localState = 'closed'; authenticatedAt = undefined;
  await click('刷新状态');
  await wait('!document.querySelector("dialog")');
  await wait('document.querySelector(".account-actions").innerText.includes("打开账号")');
  await click('打开账号');
  await wait('Boolean(document.querySelector(".two-factor input[inputmode=numeric]"))');
  await fill('.two-factor input[inputmode=numeric]', '654321');
  await click('验证并打开浏览器');
  await wait('document.querySelector(".browser-status-row strong")?.textContent === "已打开"');
  await click('关闭账号窗口');
  await wait('!document.querySelector("dialog")');
  await wait('document.querySelector(".account-actions").innerText.includes("打开账号")');
  proxiesUnavailable = true;
  await cdp.send('Page.reload', {}, sessionId);
  await wait('document.querySelector(".account-proxy button")?.textContent === "请先连接本机助手"');
  await click('浏览器管理');
  await wait('document.querySelector("dialog").innerText.includes("模拟本机代理配置读取失败")');
  assert.equal(await evaluate('document.querySelector("dialog button[aria-label=网络连接]").textContent'), '请先连接本机助手');
  assert.equal(await evaluate('document.querySelector("dialog button[aria-label=网络连接]").disabled'), true);
  await evaluate('document.querySelector("dialog button[aria-label=关闭]").click()');
  proxiesUnavailable = false;
  await cdp.send('Page.reload', {}, sessionId);
  await wait('Boolean(document.querySelector(".account-proxy button:not(:disabled)"))');
  exportExpired = true;
  await click('打开账号');
  await wait('Boolean(document.querySelector(".two-factor input[inputmode=numeric]"))');
  await fill('.two-factor input[inputmode=numeric]', '654321');
  await click('验证并打开浏览器');
  await wait('document.querySelector("dialog").innerText.includes("Session 已过期")');
  assert.equal(localLaunches.length, 3);
  assert.equal(localLaunches[2].assistant_token, 'local-test-assistant', '重新进入弹窗时默认加载小助手');
  await evaluate('document.querySelector("dialog button[aria-label=关闭]").click()');
  await navigateAdmin('proxies');
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
  await navigateAdmin('accounts');
  await wait('Boolean(document.querySelector(".account-proxy button:not(:disabled)"))');
  await select('账号 chat@example.com 的 SOCKS5', '直连（不使用代理）');
  await wait('!document.querySelector(".account-proxy button").disabled');
  await navigateAdmin('proxies');
  await wait('Boolean(document.querySelector(".proxy-row"))');
  await click('删除'); await click('确认删除');
  await wait('!document.querySelector(".proxy-row")');
  await navigateAdmin('accounts');
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
  await wait('Boolean(document.querySelector(".account-row .account-actions"))');
  assert.equal(await evaluate('Array.from(document.querySelectorAll(".account-actions button")).find(button => button.textContent === "打开账号").disabled'), true);
  account.user_id = 2;
  await cdp.send('Page.reload', {}, sessionId);
  await wait('document.body.innerText.includes("工作账号")');
  assert.equal(await evaluate('Array.from(document.querySelectorAll(".account-actions button")).some(button => button.textContent === "打开账号")'), true);
  // 从账号快速创建月订单，仅填写实收、来源、交易号和凭据。
  Object.assign(account, { subscription_package_id: null, renewal_date: '2030-01-31', payment_card_id: 9, payment_card_available: true, payment_card_label: '运营测试卡', payment_card_last4: '4242' });
  rechargePackages = [{ id: 1, name: '快捷月套餐', region: 'PH', months: 1, enabled: true, price_ready: true, sale_usd_minor: 20000 }];
  rechargeOrders = [];
  await cdp.send('Page.reload', {}, sessionId);
  await wait('Boolean(document.querySelector(".quick-month-order"))');
  const beforeQuickWrites = operationWrites.length;
  await click('快速创建月订单');
  await wait('document.querySelector("dialog").innerText.includes("请先在账号产品选型中选择")');
  assert.equal(await evaluate('Array.from(document.querySelectorAll("dialog button")).find(b => b.textContent === "确认录入并记账").disabled'), true);
  await click('取消');
  assert.equal(operationWrites.length, beforeQuickWrites);
  account.subscription_package_id = 1;
  rechargePackages[0].months = 3;
  await click('快速创建月订单');
  await wait('document.querySelector("input[aria-label=月订单套餐]")?.value.includes("3个月")');
  assert.equal(await evaluate('Array.from(document.querySelectorAll("dialog button")).find(b => b.textContent === "确认录入并记账").disabled'), true);
  await click('取消');
  rechargePackages[0].months = 1;
  await click('快速创建月订单');
  await wait('document.querySelector("input[aria-label=月订单套餐]")?.value.includes("1个月")');
  assert.equal(await evaluate('document.querySelector("input[aria-label=月订单账号]").value'), account.email);
  assert.equal(await evaluate('document.querySelector("input[aria-label=收款币种]").value'), 'CNY 人民币');
  assert.equal(await evaluate('document.querySelector("input[aria-label=订单付款卡]").value'), '运营测试卡 · •••• 4242');
  assert.equal(await evaluate(`document.querySelector('input[aria-label="扣款 USD"]').value`), '200.00');
  assert.ok(await evaluate('document.querySelector(".quick-order-period").textContent.includes("2030-01-31 → 2030-02-28")'));
  assert.equal(await evaluate('Boolean(document.querySelector("dialog textarea[name=notes]"))'), false);
  await fill('input[aria-label=实收金额]', '1680.00');
  await wait('Boolean(document.querySelector(".collection-preview"))');
  await evaluate('document.querySelector("dialog button[aria-label=订单来源]").click()');
  await fill('input[aria-label=过滤订单来源]', '快捷渠道');
  await click('＋ 使用输入的来源');
  await fill('input[name=reference]', 'quick-month-ui-reference');
  await fill('.evidence-editor textarea', '快捷月订单凭据', 'HTMLTextAreaElement');
  await writeFile(join(directory, 'quick-month-order.png'), Buffer.from((await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId)).data, 'base64'));
  quickOrderFail = true;
  await click('确认录入并记账');
  await wait('document.querySelector("dialog").innerText.includes("模拟月订单记账失败")');
  assert.equal(account.renewal_date, '2030-01-31');
  assert.equal(await evaluate('document.querySelector("input[aria-label=实收金额]").value'), '1680.00');
  assert.equal(await evaluate('document.querySelector("input[name=reference]").value'), 'quick-month-ui-reference');
  quickOrderFail = false;
  await click('确认录入并记账');
  await wait('!document.querySelector("dialog") && document.querySelector(".account-row").innerText.includes("2030-02-28")');
  const quickInput = operationWrites.at(-1);
  assert.equal(quickInput.request_key, failedQuickInput.request_key);
  assert.equal(quickInput.quick_month, true);
  assert.equal(quickInput.expected_renewal_date, '2030-01-31');
  assert.equal(quickInput.account_id, account.id);
  assert.equal(quickInput.package_id, 1);
  assert.equal(quickInput.card_id, 9);
  assert.equal(quickInput.received_currency, 'CNY');
  assert.equal(quickInput.received_amount, '1680.00');
  assert.equal(quickInput.order_source, '快捷渠道');
  assert.equal(operationWrites.length, beforeQuickWrites + 1);
  user.role = 'admin';
  const userRequestsBefore = requests.filter(url => url.includes('/api/users')).length;
  await cdp.send('Page.navigate', { url: 'http://127.0.0.1:' + server.address().port + '/admin/users' }, sessionId);
  await wait('document.body.innerText.includes("无权访问用户列表")');
  assert.equal(await evaluate('Boolean(document.querySelector(".workspace-nav a[href$=users]"))'), false);
  assert.equal(requests.filter(url => url.includes('/api/users')).length, userRequestsBefore);
  await navigateAdmin('audit');
  await wait('document.body.innerText.includes("HTTP 403")');
  for (const text of ['前端上报','后端执行','已访问','失败','audit@example.com','/admin/accounts']) {
    assert.equal(await evaluate(`document.querySelector('.admin-content').innerText.includes(${JSON.stringify(text)})`),true);
  }
  const auditShot = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId);
  await writeFile(join(directory, 'admin-audit.png'), Buffer.from(auditShot.data, 'base64'));
  await cdp.send('Page.navigate', { url: 'http://127.0.0.1:' + server.address().port + '/wallet?reference=legacy' }, sessionId);
  await wait('location.pathname === "/admin/wallet" && location.search === "?reference=legacy" && Boolean(document.querySelector(".wallet-grid"))');
  await cdp.send('Page.navigate', { url: 'http://127.0.0.1:' + server.address().port + '/features' }, sessionId);
  await wait('Boolean(document.querySelector("header nav"))');
  await delay(200);
  assert.ok(adminActivities.some(e => e.kind==='page_view' && e.page==='/admin/accounts'));
  assert.ok(adminActivities.some(e => e.kind==='page_view' && e.page==='/admin/orders'));
  assert.ok(adminActivities.some(e => e.kind==='local_request' && e.control==='proxy_test' && e.result==='failure'));
  assert.ok(adminActivities.some(e => e.kind==='local_request' && e.control==='open_browser' && e.result==='success'));
  assert.ok(adminActivities.some(e => e.kind==='local_request' && e.control==='browser_fingerprint' && e.result==='success'));
  assert.ok(adminActivities.some(e => e.kind==='click' && e.control==='delete'));
  assert.ok(adminActivities.every(e=>Object.keys(e).every(k=>['request_key','page','kind','control','result'].includes(k))));
  const auditPayload=JSON.stringify(adminActivities);
  for (const secret of ['proxy-secret','4242424242424242','local-test-access']) assert.equal(auditPayload.includes(secret),false);
  assert.ok(adminActivities.every(e=>e.page.startsWith('/admin/') && !e.page.includes('?')));
  assert.equal(otpHeaders.length, 4);
  assert.ok(otpHeaders.every(code=>code === '654321'));
  assert.equal(requests.some(url => new URL(url).pathname === '/api/accounts/1/browser'), false, '两个浏览器入口必须共用本机环境');
  assert.deepEqual(errors, []);
  t.diagnostic(`页面截图：${directory}`);
});

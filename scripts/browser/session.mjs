import { spawn } from 'node:child_process';
import { mkdir, access } from 'node:fs/promises';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
import { CDP } from './cdp.mjs';
import { loginCookies, restoreLoginCookies } from './cookies.mjs';
import { parseProxy, createProxyBridge } from './proxy.mjs';
import { checkBrowserIP, IPIFY_URL, BILLING_URL, CLEANIP_URL } from './browser-ip.mjs';
import { configureProfile } from './profile.mjs';
import { EventEmitter } from 'node:events';
import { BrowserAssistant } from './assistant.mjs';
import { homedir } from 'node:os';

export function validateSession(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value) || typeof value.accessToken !== 'string' || !value.accessToken || value.accessToken.length > 128000 || /[^\x21-\x7e]/.test(value.accessToken)) {
    throw new Error('Session 缺少有效 accessToken');
  }
  const expiries = [];
  if (typeof value.expires === 'string' && Number.isFinite(Date.parse(value.expires))) expiries.push(Date.parse(value.expires));
  try {
    const claims = JSON.parse(Buffer.from(value.accessToken.split('.')[1], 'base64url').toString());
    if (Number.isFinite(claims.exp) && claims.exp > 0) expiries.push(claims.exp * 1000);
  } catch { /* 不在本地判断签名或非 JWT Token 的有效性。 */ }
  if (expiries.some(expiry => expiry <= Date.now())) throw new Error('Session 已过期，请先更新');
  const clean = Object.fromEntries(['accessToken', 'user', 'expires', 'account', 'authProvider'].filter(key => value[key] !== undefined).map(key => [key, value[key]]));
  const cookies = loginCookies(value);
  if (cookies.length) clean.cookies = cookies;
  return clean;
}

export async function findChrome(explicit) {
  if (explicit) { await access(explicit); return explicit; }
  const paths = process.platform === 'darwin' ? [
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    '/Applications/Chromium.app/Contents/MacOS/Chromium',
    '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge',
    join(homedir(), 'Applications/Google Chrome.app/Contents/MacOS/Google Chrome'),
    join(homedir(), 'Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge'),
  ] : process.platform === 'win32' ? [
    join(process.env.PROGRAMFILES || 'C:/Program Files', 'Google/Chrome/Application/chrome.exe'),
    join(process.env.LOCALAPPDATA || '', 'Google/Chrome/Application/chrome.exe'),
    join(process.env['PROGRAMFILES(X86)'] || 'C:/Program Files (x86)', 'Google/Chrome/Application/chrome.exe'),
    join(process.env.PROGRAMFILES || 'C:/Program Files', 'Microsoft/Edge/Application/msedge.exe'),
    join(process.env.LOCALAPPDATA || '', 'Microsoft/Edge/Application/msedge.exe'),
    join(process.env['PROGRAMFILES(X86)'] || 'C:/Program Files (x86)', 'Microsoft/Edge/Application/msedge.exe'),
  ] : ['/usr/bin/google-chrome', '/usr/bin/chromium', '/usr/bin/chromium-browser'];
  for (const path of paths) { try { await access(path); return path; } catch {} }
  throw new Error('未找到 Chrome / Edge，请先安装浏览器；命令行也可用 --chrome 指定可执行文件路径');
}

export class SessionBrowser extends EventEmitter {
  constructor({ chrome, directory, headless = false, startURL = IPIFY_URL, billingURL = BILLING_URL, cleanipURL = CLEANIP_URL, checkIP = checkBrowserIP }) {
    super();
    this.chrome = chrome;
    this.directory = directory;
    this.headless = headless;
    this.startURL = startURL;
    this.billingURL = billingURL;
    this.cleanipURL = cleanipURL;
    this.checkIP = checkIP;
    this.environments = new Map();
    this.closed = false;
  }

  status(id) {
    const environment = this.environments.get(id);
    if (!environment) return { state: 'closed', api_status: null };
    return { state: environment.state, api_status: environment.apiStatus, message: environment.message, ip_check: environment.ipCheck, authenticated_at: environment.authenticatedAt };
  }

  async start({ environment_id: id, session: raw, proxy_url: proxyURL = '', expected_email: expectedEmail, assistant_token: assistantToken, assistant_endpoint: assistantEndpoint }) {
    if (this.closed) throw new Error('此站点的浏览器服务已停止');
    if (typeof id !== 'string' || id.length < 1 || id.length > 300) throw new Error('浏览器环境标识无效');
    const session = validateSession(raw);
    const proxy = parseProxy(proxyURL);
    if (this.environments.has(id)) throw new Error('该环境已经打开，请关闭后再更新 Session 或代理');
    if (this.environments.size >= 10) throw new Error('最多同时打开 10 个浏览器环境');
    const environment = { id, state: 'starting', apiStatus: null, message: '正在启动浏览器', session, expectedEmail, pages: new Map(), controller: new AbortController() };
    this.environments.set(id, environment);
    try {
      const profile = join(this.directory, createHash('sha256').update(id).digest('hex'));
      await mkdir(profile, { recursive: true, mode: 0o700 });
      await configureProfile(profile, expectedEmail || session.user?.email);
      if (this.closed) throw new Error('此站点的浏览器服务已停止');
      if (proxy) environment.proxy = await createProxyBridge(proxy);
      if (this.closed) throw new Error('此站点的浏览器服务已停止');
      const args = [
        `--user-data-dir=${profile}`, '--profile-directory=Default', '--remote-debugging-pipe', '--no-first-run', '--no-default-browser-check',
        '--disable-background-networking', '--disable-quic', '--force-webrtc-ip-handling-policy=disable_non_proxied_udp',
        ...(proxy ? [`--proxy-server=${environment.proxy.url}`] : ['--no-proxy-server']),
        ...(this.headless ? ['--headless=new'] : []), 'about:blank',
      ];
      const child = spawn(this.chrome, args, { stdio: ['ignore', 'ignore', 'ignore', 'pipe', 'pipe'], windowsHide: false });
      environment.child = child;
      const cleanup = () => {
        if (environment.cleaned) return;
        environment.cleaned = true;
        this.emit('activity', { environment_id: id, action: 'close', ok: true });
        environment.controller.abort();
        environment.proxy?.close();
        environment.assistant?.close();
        environment.session = null;
        clearInterval(environment.verifyTimer);
        clearInterval(environment.pageTimer);
        if (this.environments.get(id) === environment) this.environments.delete(id);
      };
      child.once('exit', cleanup); child.once('error', cleanup);
      const cdp = new CDP(child);
      environment.cdp = cdp;
      if (typeof assistantToken === 'string' && assistantToken.length <= 2048 && assistantEndpoint) environment.assistant = new BrowserAssistant(environment, assistantEndpoint, assistantToken);
      environment.hasLoginCookie = await restoreLoginCookies(cdp, session);
      // Cookie 已在浏览器级恢复，无需暂停或递归接管新标签、iframe 和 Worker。
      // 复用 Chromium 启动时的空白标签，避免每次打开都额外留下 about:blank。
      const { targetInfos } = await cdp.send('Target.getTargets');
      const initialPage = targetInfos.find(target => target.type === 'page' && target.url === 'about:blank');
      const { targetId } = initialPage || await cdp.send('Target.createTarget', { url: 'about:blank' });
      const { sessionId: pageSession } = await cdp.send('Target.attachToTarget', { targetId, flatten: true });
      environment.pages.set(targetId, pageSession);
      environment.pageTimer = setInterval(() => this.syncPages(environment), 2000);
      environment.pageTimer.unref();
      environment.state = 'checking_ip';
      environment.message = '浏览器已打开，正在通过 ipify 核对出口 IP。';
      // 先返回窗口状态，让页面立即提供关闭按钮；IP 检查完成前不打开 ChatGPT。
      environment.opening = this.openBilling(environment, pageSession, proxyURL);
      return this.status(id);
    } catch (error) {
      environment.child?.kill();
      environment.proxy?.close();
      this.environments.delete(id);
      throw error;
    }
  }

  async syncPages(environment) {
    if (!environment.session || environment.syncingPages || environment.state === 'closing') return;
    environment.syncingPages = true;
    try {
      const { targetInfos } = await environment.cdp.send('Target.getTargets');
      // macOS 关闭最后一个窗口后进程可能仍存活，需要主动结束该账号环境。
      if (!targetInfos.some(target => target.type === 'page')) { await this.stop(environment.id); return; }
      await environment.assistant?.sync(targetInfos);
    } catch { /* 浏览器可能正在关闭。 */ }
    finally { environment.syncingPages = false; }
  }

  async openBilling(environment, pageSession, proxyURL) {
    try {
      const ipCheck = this.checkIP(environment.cdp, pageSession, proxyURL, { url: this.startURL, signal: environment.controller?.signal });
      if (this.cleanipURL) void environment.cdp.send('Target.createTarget', { url: this.cleanipURL }).catch(() => {});
      const result = await ipCheck;
      if (!environment.session || environment.state === 'closing') return;
      environment.ipCheck = result;
      this.emit('activity', { environment_id: environment.id, action: 'ip_check', ok: result.ok && (!proxyURL || result.matches), exit_ip: result.exit_ip, matches: result.matches });
      if (!result.ok || (proxyURL && !result.matches)) {
        environment.state = 'ip_check_failed';
        environment.message = result.ok ? `出口 IP ${result.exit_ip} 与代理 IP ${result.proxy_ips.join(' / ')} 不一致，未打开 ChatGPT。请检查代理后关闭并重新打开账号。` : `${result.error}；未打开 ChatGPT，请关闭后重试。`;
        return;
      }
      await environment.cdp.send('Target.createTarget', { url: this.billingURL });
      if (!environment.session || environment.state === 'closing') return;
      environment.state = 'opened';
      environment.message = proxyURL ? `出口 IP ${result.exit_ip} 与代理 IP 一致，已新开 ChatGPT 账单页。` : `直连出口 IP：${result.exit_ip}，已新开 ChatGPT 账单页。`;
      if (!environment.hasLoginCookie) environment.message += '本次 Session 未提供登录 Cookie；若尚未登录，请在窗口中登录或补充 Cookie。';
      environment.verifyTimer = setInterval(() => this.verifyLogin(environment), 5000);
      environment.verifyTimer.unref();
      void this.verifyLogin(environment);
    } catch {
      if (!environment.session || environment.state === 'closing') return;
      environment.state = 'ip_check_failed';
      environment.message = '打开账号流程未完成，请检查 IP 页面和网络后关闭并重新打开账号。';
    }
  }

  async verifyLogin(environment) {
    if (!environment.session || environment.verifying) return;
    environment.verifying = true;
    try {
      // 请求 ChatGPT 的真实会话接口，不伪造响应、不用 accessToken 冒充登录 Cookie。
      // 只将状态及邮箱带回进程，完整会话和 Cookie 不进入日志或状态响应。
      const { targetInfos } = await environment.cdp.send('Target.getTargets');
      const liveIDs = new Set(targetInfos.map(target => target.targetId));
      for (const id of environment.pages.keys()) if (!liveIDs.has(id)) environment.pages.delete(id);
      let value;
      for (const target of targetInfos) {
        if (target.type !== 'page' || !/^https:\/\/chatgpt\.com(?:\/|$)/.test(target.url)) continue;
        try {
          let pageSession = environment.pages.get(target.targetId);
          if (!pageSession) {
            const attached = await environment.cdp.send('Target.attachToTarget', { targetId: target.targetId, flatten: true });
            pageSession = attached.sessionId;
            environment.pages.set(target.targetId, pageSession);
          }
          const result = await environment.cdp.send('Runtime.evaluate', {
            expression: "(async () => { if (location.origin !== 'https://chatgpt.com') return null; try { const response = await fetch('/api/auth/session', { credentials: 'include', cache: 'no-store', signal: AbortSignal.timeout(8000) }); const body = await response.json().catch(() => ({})); let claims = {}; try { claims = JSON.parse(atob(body.accessToken.split('.')[1].replace(/-/g, '+').replace(/_/g, '/'))); } catch {} return { status: response.status, email: typeof body.user?.email === 'string' ? body.user.email : null, plan: body.account?.planType || body.account?.plan_type || body.user?.planType || claims['https://api.openai.com/auth']?.chatgpt_plan_type || null }; } catch { return { status: 0 }; } })()",
            returnByValue: true, awaitPromise: true,
          }, pageSession);
          value = result.result?.value;
          if (value) break;
        } catch { /* 标签关闭或导航时检查可能中断；继续检查其他 ChatGPT 标签，不影响浏览器。 */ }
      }
      if (!environment.session || environment.state === 'closing') return;
      if (!value) return;
      environment.apiStatus = value.status;
      environment.actualEmail = value.email || null;
      environment.plan = typeof value.plan === 'string' ? value.plan.slice(0, 80) : null;
      if (value.status === 200 && value.email) {
        const expected = environment.expectedEmail || environment.session.user?.email;
        if (!expected || expected.toLowerCase() !== value.email.toLowerCase()) {
          environment.state = 'rejected';
          environment.message = '窗口内登录的账号与导入账号不一致，请退出该窗口中的账号后重新登录';
        } else {
          environment.state = 'authenticated';
          if (!environment.authenticatedAt) {
            environment.authenticatedAt = new Date().toISOString();
            this.emit('activity', { environment_id: environment.id, action: 'login', ok: true, logged_in_at: environment.authenticatedAt });
          }
          environment.message = 'ChatGPT 已确认网页登录账号与导入账号一致';
        }
      } else if (value.status === 200 || value.status === 401) {
        environment.state = 'login_required';
        environment.message = environment.hasLoginCookie ? 'ChatGPT 未接受此登录 Cookie，请更新 Cookie 或在窗口中重新登录' : '仅有 accessToken 无法恢复网页登录。请补充网页登录 Cookie，或在此独立窗口登录一次';
      } else {
        environment.state = 'unverified';
        environment.message = '尚未确认网页登录，请检查浏览器页面、代理连接或网页验证提示';
      }
    } catch { /* 调试连接的暂时错误不代表网页登录失效，也不能关闭浏览器。 */ }
    finally { environment.verifying = false; }
  }

  async stop(id) {
    const environment = this.environments.get(id);
    if (environment) {
      if (!environment.child) throw new Error('环境正在启动，请稍后关闭');
      environment.state = 'closing';
      environment.controller.abort();
      clearInterval(environment.verifyTimer);
      clearInterval(environment.pageTimer);
      try { await environment.cdp.send('Browser.close'); } catch { environment.child.kill(); }
    }
    return { state: 'closing', api_status: null, message: '正在关闭浏览器' };
  }

  close() {
    this.closed = true;
    const exits = [];
    for (const environment of this.environments.values()) {
      environment.controller.abort();
      clearInterval(environment.verifyTimer); clearInterval(environment.pageTimer);
      environment.assistant?.close(); environment.proxy?.close();
      const child = environment.child;
      if (!child || child.exitCode !== null || child.signalCode !== null) continue;
      exits.push(new Promise(resolve => {
        const finished = () => { clearTimeout(timer); child.off('exit', finished); child.off('error', finished); resolve(); };
        const timer = setTimeout(() => { child.kill('SIGKILL'); finished(); }, 5000);
        timer.unref();
        child.once('exit', finished); child.once('error', finished);
        child.kill();
      }));
    }
    return Promise.all(exits);
  }
}

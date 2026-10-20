import { request } from './api';
import { activityContext, localActivityControl, recordAdminActivity } from './adminActivity';
import { defaultLauncherPort, launcherAddress, validateLauncherPort } from '../../shared/local-launcher.mjs';

const portKey = 'aitok.launcher.port';
export function launcherPort() {
  try { const saved = localStorage.getItem(portKey); if (saved) return validateLauncherPort(saved); } catch { /* 存储不可用时使用站点默认值。 */ }
  return defaultLauncherPort(window.location.origin);
}
export function saveLauncherPort(value) {
  const port = validateLauncherPort(value);
  localStorage.setItem(portKey, String(port));
  window.dispatchEvent(new Event('aitok-launcher-change'));
  return port;
}
export const launcherCommand = () => `node scripts/session-browser.mjs --origin ${JSON.stringify(window.location.origin)} --port ${launcherPort()}`;
export const browserEnvironmentID = (userID, accountID) => `${window.location.origin}:user:${userID}:account:${accountID}`;

export async function launcherRequest(path, { method = 'GET', body, signal, port = launcherPort() } = {}) {
  const context = activityContext(), control = localActivityControl(path, method);
  // 只从本机浏览器的结构化环境标识解析账号 ID，不上传路径、邮箱或请求正文。
  const environment = path.startsWith('/browsers/') ? decodeURIComponent(path.split('/')[2]) : path === '/browsers' ? body?.environment_id : '';
  const accountID = Number(/:account:([0-9]+)$/.exec(environment || '')?.[1]);
  let result = 'failure';
  try {
    let response;
    try {
      response = await fetch(launcherAddress(port) + path, {
        method, signal: signal ? AbortSignal.any([signal, AbortSignal.timeout(45000)]) : AbortSignal.timeout(45000), credentials: 'omit', cache: 'no-store', redirect: 'error',
        headers: { 'X-AiTok-Client': 'browser', ...(body ? { 'Content-Type': 'application/json' } : {}) },
        body: body ? JSON.stringify(body) : undefined,
      });
    } catch (error) {
      if (error.name === 'AbortError') { result = 'cancelled'; throw error; }
      throw new Error(`无法连接本机助手（端口 ${port}），请启动桌面助手，核对站点与端口，并允许浏览器访问本地网络。`);
    }
    const data = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(response.status === 401 || response.status === 403 ? '请确认桌面助手中的站点地址与当前网页一致，并检查连接端口。' : data.error || '本机助手请求失败');
    const savedTest = control === 'proxy_test' ? data.proxies?.find(proxy => proxy.id === path.split('/')[2])?.last_test : null;
    result = data.result?.ok === false || savedTest?.ok === false ? 'failure' : 'success';
    return data;
  } finally {
    if (control) void recordAdminActivity(context, { kind: 'local_request', control, result, ...(Number.isSafeInteger(accountID) && accountID > 0 ? { account_id: accountID } : {}) });
  }
}

export async function openLocalAccount(account, userID, token, { loadAssistant = true, refreshSession = false } = {}) {
  const port = launcherPort();
  const health = await launcherRequest('/health', { port });
  if (health.version !== 2) throw new Error('请重启最新版本机启动器');
  if (refreshSession && !health.session_refresh) throw new Error('请更新并重启支持自动更新 Session 的桌面助手');
  if (!health.shared_proxies) throw new Error('请更新并重启支持共享代理的桌面助手');
  if (health.fingerprint !== 'native-noise-v1') throw new Error('请更新并重启支持独立指纹的本机助手');
  const id = browserEnvironmentID(userID, account.id);
  const status = await launcherRequest('/browsers/' + encodeURIComponent(id), { port });
  if (!['closed', 'unavailable'].includes(status.state)) {
    if (refreshSession) throw new Error('此账号浏览器已打开，请先关闭，再更新 Session');
    return status;
  }
  const credentials = await request(`/accounts/${account.id}/browser-session${refreshSession ? '?mode=refresh' : ''}`, token, { method: 'POST' });
  if (!Object.hasOwn(credentials, 'shared_proxy')) throw new Error('服务端尚未支持共享代理，请更新服务端后重试');
  if (port !== launcherPort()) throw new Error('本机连接端口已更改，请重新打开账号');
  return launcherRequest('/browsers', { port, method: 'POST', body: { environment_id: id, session: credentials.session, shared_proxy: credentials.shared_proxy, expected_email: account.email, ...(refreshSession ? { refresh_session: true } : {}), ...(loadAssistant || refreshSession ? { assistant_token: credentials.assistant_token } : {}) } });
}

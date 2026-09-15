import { request } from './api';
import { activityContext, localActivityControl, recordAdminActivity } from './adminActivity';

export const launcherCommand = () => `node scripts/session-browser.mjs --origin ${JSON.stringify(window.location.origin)}`;
export const browserEnvironmentID = (userID, accountID) => `${window.location.origin}:user:${userID}:account:${accountID}`;

export async function launcherRequest(path, { method = 'GET', body, signal } = {}) {
  const context = activityContext(), control = localActivityControl(path, method);
  let result = 'failure';
  try {
    let response;
    try {
      response = await fetch('http://127.0.0.1:15683' + path, {
        method, signal: signal || AbortSignal.timeout(45000), credentials: 'omit', cache: 'no-store', redirect: 'error',
        headers: { 'X-AiTok-Client': 'browser', ...(body ? { 'Content-Type': 'application/json' } : {}) },
        body: body ? JSON.stringify(body) : undefined,
      });
    } catch (error) {
      if (error.name === 'AbortError') { result = 'cancelled'; throw error; }
      throw new Error('无法连接本机启动器，请启动它，并允许浏览器访问本地网络。');
    }
    const data = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(response.status === 401 || response.status === 403 ? '请重启最新版启动器，并确认 --origin 与当前网站地址一致。' : data.error || '本机启动器请求失败');
    const savedTest = control === 'proxy_test' ? data.proxies?.find(proxy => proxy.id === path.split('/')[2])?.last_test : null;
    result = data.result?.ok === false || savedTest?.ok === false ? 'failure' : 'success';
    return data;
  } finally {
    if (control) void recordAdminActivity(context, { kind: 'local_request', control, result });
  }
}

export async function openLocalAccount(account, userID, token, totpCode) {
  const health = await launcherRequest('/health');
  if (health.version !== 2) throw new Error('请重启最新版本机启动器');
  const id = browserEnvironmentID(userID, account.id);
  const status = await launcherRequest('/browsers/' + encodeURIComponent(id));
  if (!['closed', 'unavailable'].includes(status.state)) return status;
  const credentials = await request(`/accounts/${account.id}/browser-session`, token, { method: 'POST', totpCode });
  return launcherRequest('/browsers', { method: 'POST', body: { environment_id: id, session: credentials.session, expected_email: account.email, assistant_token: credentials.assistant_token } });
}

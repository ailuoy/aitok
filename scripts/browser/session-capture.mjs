import { isLoginCookie, loginCookies } from './cookies.mjs';
import { isVerificationPage } from './page-verification.mjs';

// 仅在用户主动更新时读取真实会话；凭据只返回本机进程，不返回助手面板。
export async function captureBrowserSession(environment) {
  const { cdp } = environment;
  const { targetInfos } = await cdp.send('Target.getTargets');
  const target = targetInfos.find(target => target.type === 'page' && /^https:\/\/chatgpt\.com(?:\/|$)/.test(target.url));
  if (!target) throw new Error('请先在当前浏览器打开 ChatGPT 并登录，再更新 Session');
  const { sessionId } = await cdp.send('Target.attachToTarget', { targetId: target.targetId, flatten: true });
  try {
    const { frameTree } = await cdp.send('Page.getFrameTree', {}, sessionId);
    const { executionContextId } = await cdp.send('Page.createIsolatedWorld', { frameId: frameTree.frame.id, worldName: 'aitok-session-update' }, sessionId);
    const response = await cdp.send('Runtime.evaluate', {
      expression: `(${readSession.toString()})(${isVerificationPage.toString()})`,
      contextId: executionContextId, returnByValue: true, awaitPromise: true, timeout: 10000,
    }, sessionId);
    const value = response.result?.value;
    if (value?.error) throw new Error(value.error);
    const session = value?.session;
    if (!session || typeof session.accessToken !== 'string' || !session.accessToken || typeof session.user?.email !== 'string') throw new Error('未读取到有效 Session，请先在 ChatGPT 登录后重试');
    const expected = environment.expectedEmail || environment.session?.user?.email;
    if (!expected || session.user.email.trim().toLowerCase() !== expected.trim().toLowerCase()) throw new Error('当前登录邮箱与此账号不一致，未更新 Session');
    const { cookies } = await cdp.send('Network.getCookies', { urls: ['https://chatgpt.com/'] }, sessionId);
    session.cookies = loginCookies({ cookies: cookies.filter(cookie => isLoginCookie(cookie.name) && ['chatgpt.com', '.chatgpt.com'].includes(cookie.domain)) });
    if (Buffer.byteLength(JSON.stringify(session)) > 240000) throw new Error('Session 数据过大，无法更新');
    return session;
  } finally {
    await cdp.send('Target.detachFromTarget', { sessionId }).catch(() => {});
  }
}

async function readSession(isVerificationPage) {
  if (location.origin !== 'https://chatgpt.com') return { error: '请在 ChatGPT 页面更新 Session' };
  if (isVerificationPage()) return { error: '请先完成 ChatGPT 网页验证，再更新 Session' };
  try {
    const response = await fetch('/api/auth/session', { credentials: 'include', cache: 'no-store', redirect: 'error', signal: AbortSignal.timeout(8000) });
    if (response.headers.get('cf-mitigated') === 'challenge') return { error: '请先完成 ChatGPT 网页验证，再更新 Session' };
    if (!response.ok) return { error: '读取 Session 失败，请确认 ChatGPT 已登录后重试' };
    const raw = await response.text();
    if (raw.length > 240000) return { error: 'Session 数据过大，无法更新' };
    const body = JSON.parse(raw);
    return { session: Object.fromEntries(['accessToken', 'user', 'expires', 'account', 'authProvider'].filter(key => body[key] !== undefined).map(key => [key, body[key]])) };
  } catch { return { error: '读取 Session 失败，请检查网络及 ChatGPT 登录状态后重试' }; }
}

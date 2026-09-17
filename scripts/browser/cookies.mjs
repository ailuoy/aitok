export const isLoginCookie = name => typeof name === 'string' && /^__Secure-(?:next-auth|authjs)\.session-token(?:\.\d+)?$/.test(name);

// 与 NextAuth / Auth.js 一致，为 Cookie 名称和属性预留 160 字节。
const SESSION_CHUNK_SIZE = 4096 - 160;

export function loginCookies(session) {
  const source = session.cookies === undefined ? [] : session.cookies;
  if (!Array.isArray(source) || source.length > 100) throw new Error('cookies 必须是最多 100 项的数组');
  const cookies = [...source];
  if (session.sessionToken !== undefined) cookies.push({ name: '__Secure-next-auth.session-token', value: session.sessionToken });
  const result = new Map();
  for (const cookie of cookies) {
    if (!cookie || typeof cookie !== 'object') throw new Error('Cookie 格式无效');
    if (!isLoginCookie(cookie.name) || (cookie.domain && !['chatgpt.com', '.chatgpt.com'].includes(cookie.domain))) continue;
    if (typeof cookie.value !== 'string' || !cookie.value || cookie.value.length > 16000 || /[^\x21\x23-\x2b\x2d-\x3a\x3c-\x5b\x5d-\x7e]/.test(cookie.value)) throw new Error('网页登录 Cookie 格式无效');
    result.set(cookie.name, { name: cookie.name, value: cookie.value, domain: '.chatgpt.com', path: '/', secure: true, httpOnly: true, sameSite: 'Lax' });
  }
  for (const cookie of [...result.values()]) {
    if (/\.\d+$/.test(cookie.name) || cookie.value.length <= SESSION_CHUNK_SIZE) continue;
    // 完整 Token 重新分段时替换同名旧分段，避免拼接出混合凭据。
    for (const name of result.keys()) {
      if (name === cookie.name || name.startsWith(`${cookie.name}.`)) result.delete(name);
    }
    for (let offset = 0; offset < cookie.value.length; offset += SESSION_CHUNK_SIZE) {
      const name = `${cookie.name}.${offset / SESSION_CHUNK_SIZE}`;
      result.set(name, { ...cookie, name, value: cookie.value.slice(offset, offset + SESSION_CHUNK_SIZE) });
    }
  }
  for (const cookie of result.values()) {
    if (cookie.name.length + cookie.value.length > 4096) throw new Error('网页登录 Cookie 分段过长，请重新复制原始分段 Cookie');
  }
  return [...result.values()];
}

export async function restoreLoginCookies(cdp, session) {
  const cookies = loginCookies(session);
  if (!cookies.length) return false;
  const existing = await cdp.send('Storage.getCookies');
  for (const cookie of existing.cookies) {
    if (isLoginCookie(cookie.name) && ['chatgpt.com', '.chatgpt.com'].includes(cookie.domain)) {
      // Storage 支持浏览器级连接；用过期 Cookie 精确清理旧分段，保留其他站点数据。
      await cdp.send('Storage.setCookies', { cookies: [{ name: cookie.name, value: '', domain: cookie.domain, path: cookie.path, secure: cookie.secure, expires: 1 }] });
    }
  }
  const expires = Date.parse(session.expires);
  await cdp.send('Storage.setCookies', { cookies: cookies.map(cookie => ({ ...cookie, ...(Number.isFinite(expires) ? { expires: expires / 1000 } : {}) })) });
  return true;
}

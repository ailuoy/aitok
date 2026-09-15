import React, { useState } from 'react';
import JsonEditor from './JsonEditor';

export default function SessionFields({ updating = false }) {
  const [hint, setHint] = useState('');
  const [raw, setRaw] = useState('');
  function preview(raw) {
    if (!raw.trim()) { setHint(''); return; }
    try {
      const session = JSON.parse(raw);
      const credentials = session?.tokens || session?.credentials || session;
      if (!(session?.accessToken || session?.access_token || credentials?.accessToken || credentials?.access_token)) {
        setHint('缺少 accessToken，请复制完整会话 JSON。'); return;
      }
      setHint(typeof session.user?.email === 'string' ? `识别到账号：${session.user.email}` : '已识别访问凭据；若无法自动识别邮箱，请在下方补充。');
    } catch { setHint('等待完整的 JSON 对象…'); }
  }
  return <>
    <p className="muted">在已登录 ChatGPT 的浏览器中打开 <a href="https://chatgpt.com/api/auth/session" target="_blank" rel="noreferrer">获取 Session JSON</a>，复制完整内容后粘贴。凭据将加密保存。</p>
    <JsonEditor value={raw} onChange={value => { setRaw(value); preview(value); }} />
    {hint && <p className="muted" role="status">{hint}</p>}
    <label>网页登录 Cookie（可选）<input name="session_cookie" type="password" autoComplete="off" placeholder="__Secure-next-auth.session-token 的值" maxLength={16000} /></label>
    <p className="muted">普通 Session JSON 只有 accessToken，不能恢复网页登录。可补充登录 Cookie（开发者工具 → Application → Cookies → chatgpt.com），或在打开的独立窗口中登录一次。分段 Cookie 可放入 JSON 的 cookies 数组。</p>
    {!updating && <><label>账号名称（可选）<input name="label" maxLength={120} placeholder="默认使用会话中的名称或邮箱" /></label><label>登录邮箱（未识别时填写）<input name="email" type="email" placeholder="默认从 Session 自动识别" autoComplete="off" /></label></>}
  </>;
}

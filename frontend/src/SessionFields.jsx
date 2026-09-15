import React, { useState } from 'react';
import JsonEditor from './JsonEditor';

export default function SessionFields({ updating = false }) {
  const [hint, setHint] = useState('');
  const [raw, setRaw] = useState('');
  const [needsEmail, setNeedsEmail] = useState(false);
  function preview(raw) {
    setNeedsEmail(false);
    if (!raw.trim()) { setHint(''); return; }
    try {
      const session = JSON.parse(raw);
      const token = [session, session?.tokens, session?.credentials].flatMap(value => [value?.accessToken, value?.access_token]).find(value => typeof value === 'string' && value.trim())?.trim();
      if (!token) {
        setHint('缺少 accessToken，请复制完整会话 JSON。'); return;
      }
      let email = typeof session.user?.email === 'string' ? session.user.email.trim() : '';
      // 与后端一致，JWT 的邮箱仅用于预览，不能作为登录成功的证明。
      try {
        if (token.split('.').length === 3) {
          const payload = token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/');
          const claims = JSON.parse(new TextDecoder().decode(Uint8Array.from(atob(payload), c => c.charCodeAt(0))));
          const claimedEmail = claims['https://api.openai.com/profile']?.email;
          if (typeof claimedEmail === 'string' && claimedEmail.trim()) email = claimedEmail.trim();
        }
      } catch {}
      setNeedsEmail(!email);
      setHint(email ? `识别到账号：${email.toLowerCase()}` : '未识别到邮箱，请在下方补充。');
    } catch { setHint('等待完整的 JSON 对象…'); }
  }
  return <>
    <p className="muted">在已登录 ChatGPT 的浏览器中打开 <a href="https://chatgpt.com/api/auth/session" target="_blank" rel="noreferrer">获取 Session JSON</a>，复制完整内容后粘贴。凭据将加密保存。</p>
    <JsonEditor value={raw} onChange={value => { setRaw(value); preview(value); }} />
    {hint && <p className="muted" role="status">{hint}</p>}
    {!updating && needsEmail && <label>登录邮箱<input name="email" type="email" required placeholder="填写此账号的登录邮箱" autoComplete="off" /></label>}
  </>;
}

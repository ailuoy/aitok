export function sessionExpiry(expiresAt, hasSession, now = Date.now()) {
  if (!hasSession) return { text: '未保存 Session', status: 'unknown' };
  const expiry = expiresAt ? Date.parse(expiresAt) : NaN;
  if (!Number.isFinite(expiry)) return { text: '到期时间未知', status: 'unknown' };
  if (expiry <= now) return { text: '已过期', status: 'expired' };
  let seconds = Math.ceil((expiry - now) / 1000);
  const days = Math.floor(seconds / 86400); seconds %= 86400;
  const hours = Math.floor(seconds / 3600); seconds %= 3600;
  const minutes = Math.floor(seconds / 60); seconds %= 60;
  const text = `${days ? `${days}天` : ''}${hours ? `${hours}小时` : ''}${minutes ? `${minutes}分` : ''}${seconds}秒`;
  return { text: `剩余 ${text}`, status: expiry - now <= 86400000 ? 'soon' : 'valid' };
}

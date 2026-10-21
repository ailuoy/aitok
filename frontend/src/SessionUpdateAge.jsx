import React, { useEffect, useState } from 'react';
import { formatUTC8 } from './time';
import { sessionExpiry } from './sessionExpiry.mjs';

export default function SessionUpdateAge({ value, expiresAt, hasSession }) {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    setNow(Date.now());
    if (!hasSession || !expiresAt || !Number.isFinite(Date.parse(expiresAt)) || Date.parse(expiresAt) <= Date.now()) return;
    const timer = setInterval(() => {
      const current = Date.now();
      setNow(current);
      if (current >= Date.parse(expiresAt)) clearInterval(timer);
    }, 1000);
    return () => clearInterval(timer);
  }, [expiresAt, hasSession]);
  const expiry = sessionExpiry(expiresAt, hasSession, now);
  const at = value ? Date.parse(value) : NaN;
  const days = Math.max(0, Math.floor((now - at) / 86400000));
  return <div>
    <strong className="session-countdown" data-status={expiry.status} title={expiresAt && Number.isFinite(Date.parse(expiresAt)) ? `Session 到期：${formatUTC8(expiresAt)}（UTC+8）。此时间对应保存的 Session，本机 Cookie 仍可能有效。` : undefined}>{expiry.text}</strong>
    <small className="cell-secondary">{Number.isFinite(at) ? `距上次更新：${days === 0 ? '不足 1 天' : `${days} 天`}` : '更新时间未记录'}</small>
    {Number.isFinite(at) && <small className="cell-secondary">{formatUTC8(value)}</small>}
  </div>;
}

import React from 'react';
import { formatUTC8 } from './time';

export default function SessionUpdateAge({ value }) {
  const at = value ? Date.parse(value) : NaN;
  if (!Number.isFinite(at)) return <span className="muted">未记录</span>;
  const days = Math.max(0, Math.floor((Date.now() - at) / 86400000));
  return <div><strong>{days === 0 ? '不足 1 天' : `${days} 天`}</strong><small className="cell-secondary">{formatUTC8(value)}</small></div>;
}

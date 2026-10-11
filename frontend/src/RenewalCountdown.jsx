import React from 'react';
import { renewalDays } from './accountRenewalStatus.mjs';

export default function RenewalCountdown({ date, today }) {
  const days = renewalDays(date, today);
  if (!Number.isFinite(days)) return null;
  const status = days > 10 ? 'safe' : days > 0 ? 'soon' : days === 0 ? 'today' : 'overdue';
  return <small className="cell-secondary renewal-countdown" data-status={status}>{days < 0 ? `已逾期 ${-days} 天` : days === 0 ? '今天续费（0 天）' : `距离续费 ${days} 天`}</small>;
}

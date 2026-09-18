export function renewalDays(date, today) {
  return Math.round((Date.parse(date) - Date.parse(today)) / 86400000);
}

export function matchesRenewalStatus(date, status, today) {
  if (!status) return true;
  const days = renewalDays(date, today);
  if (!Number.isFinite(days)) return false;
  return status === 'safe' ? days > 10 : status === 'soon' ? days >= 0 && days <= 10 : status === 'overdue' ? days < 0 : false;
}

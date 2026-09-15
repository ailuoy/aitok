const utc8 = new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' });
export function formatUTC8(value) { return value && Number.isFinite(Date.parse(value)) ? utc8.format(new Date(value)) : '尚未登录'; }

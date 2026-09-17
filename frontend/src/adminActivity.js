import { API } from './apiConfig';

let active = null;
const pagePattern = /^\/admin\/(accounts|proxies|addresses|bank-cards|users|wallet|orders|packages|notices|audit|proxy-activity|payment-exceptions)(\/[0-9]+)?$/;
export const adminPage = () => pagePattern.test(location.pathname) ? location.pathname : '';
export const adminAuditHeaders = () => adminPage() ? { 'X-Aitok-Page': adminPage() } : {};
export const activityContext = () => active && adminPage() ? { ...active, page: adminPage() } : null;

// 只发送动作枚举，绝不采集输入框、选项值、按钮原文、查询参数或剪贴板。
export async function recordAdminActivity(context, event) {
  if (!context) return;
  const body = JSON.stringify({ request_key: crypto.randomUUID(), page: context.page, ...event });
  for (let attempt = 0; attempt < 2; attempt++) {
    try {
      const response = await fetch(API + '/admin-activity', {
        method: 'POST', keepalive: true, signal: AbortSignal.timeout(5000),
        headers: { Authorization: `Bearer ${context.token}`, 'Content-Type': 'application/json' }, body,
      });
      if (response.ok || response.status < 500) return;
    } catch {}
    if (!active || active.token !== context.token) return;
  }
  // 不缓存到本地存储，避免跨用户重放；失败也不影响原业务操作。
  console.warn('操作审计暂时无法保存');
}

function controlFor(element) {
  if (element.matches('.account-owner-bind')) return 'bind_user';
  if (element.matches('.account-renewal-switch')) return 'subscription';
  if (element.closest('.pagination-pages')) return 'paginate';
  if (element.getAttribute('role') === 'option' || element.getAttribute('role') === 'combobox') return 'select';
  if (element.matches('a')) return 'link';
  const text = (element.textContent || '').trim();
  const labels = [
    [/^(添加|新增|创建|新建)/, 'add'], [/^编辑/, 'edit'], [/^删除/, 'delete'], [/^确认/, 'confirm'],
    [/^(取消|返回修改|收起|关闭$)/, 'cancel'], [/^保存/, 'save'], [/^(搜索|查找)/, 'search'], [/^(刷新|重试|立即重试)/, 'refresh'],
    [/^导出/, 'export'], [/^(导入|批量导入|测试并导入)/, 'import'], [/^(打开账号|打开浏览器)/, 'open_browser'],
    [/^(关闭浏览器|关闭账号窗口)/, 'close_browser'], [/^(详情|完整资料|查看|使用记录|全部使用记录|余额 \/ 对账单|运营 \/ 核对)/, 'view'],
    [/^复制/, 'copy'], [/^(自动|白色|黑色)$/, 'theme'], [/^(上一页|下一页)$/, 'paginate'], [/^测试/, 'test'],
    [/^获取 IP/, 'get_ip'], [/^分配/, 'assign'], [/^核验/, 'verify'], [/^(退款|申请原路退款|批准原路退款|原申请重试)/, 'refund'],
    [/^记录存入/, 'deposit'], [/^(官网扣款|记录开通扣款|核对并记账)/, 'purchase'], [/^管理分组/, 'group'], [/^废弃/, 'discard'],
    [/^(开启|关闭)续费提醒/, 'subscription'],
  ];
  return labels.find(([pattern]) => pattern.test(text))?.[1] || 'button';
}

export function installAdminActivity(token, userID) {
  const session = { token, userID };
  active = session;
  const click = event => {
    const element = event.target instanceof Element ? event.target.closest('button,a,[role="option"],summary') : null;
    if (!element || element.matches(':disabled,[aria-disabled="true"]')) return;
    // 页面跳转由 page_view 记录一次；其他链接不上传目标 URL。
    if (element.matches('a') && pagePattern.test(element.getAttribute('href') || '')) return;
    void recordAdminActivity(activityContext(), { kind: 'click', control: controlFor(element), result: 'triggered' });
  };
  const submit = () => { void recordAdminActivity(activityContext(), { kind: 'submit', control: 'form', result: 'triggered' }); };
  const change = event => {
    if (event.target.matches('input[type="checkbox"],input[type="radio"],select')) {
      void recordAdminActivity(activityContext(), { kind: 'change', control: 'toggle', result: 'triggered' });
    }
  };
  document.addEventListener('click', click, true);
  document.addEventListener('submit', submit, true);
  document.addEventListener('change', change, true);
  return () => {
    document.removeEventListener('click', click, true);
    document.removeEventListener('submit', submit, true);
    document.removeEventListener('change', change, true);
    if (active === session) active = null;
  };
}

export function localActivityControl(path, method) {
  if (/^\/browsers\/[^/]+\/fingerprint$/.test(path) && method === 'POST') return 'browser_fingerprint';
  if (/^\/browsers\/[^/]+\/proxy$/.test(path) && method === 'PATCH') return 'proxy_bind';
  if (path === '/browsers' && method === 'POST') return 'open_browser';
  if (/^\/browsers\/[^/]+$/.test(path) && method === 'DELETE') return 'close_browser';
  if (path === '/proxies/parse' && method === 'POST') return 'proxy_import';
  if (/^\/proxies(?:\/[^/]+)?\/test$/.test(path) && method === 'POST') return 'proxy_test';
  if (path === '/proxies' && method === 'POST') return 'proxy_create';
  if (/^\/proxies\/[^/]+$/.test(path)) return { GET: 'proxy_read', PATCH: 'proxy_update', DELETE: 'proxy_delete' }[method];
  return null;
}

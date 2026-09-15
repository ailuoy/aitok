import { setTimeout as delay } from 'node:timers/promises';
import { normalizeIP, testProxy } from './proxy-test.mjs';

export const IPIFY_URL = 'https://api.ipify.org/';
export const BILLING_URL = 'https://chatgpt.com/#settings/Billing';
export const CLEANIP_URL = 'https://cleanip.io/';

// 读取账号浏览器实际显示的 IP，确保检查与后续标签使用同一浏览器及代理。
export async function checkBrowserIP(cdp, pageSession, proxyURL, { url = IPIFY_URL, timeout = 20000, signal } = {}) {
  const fetchIP = async () => {
    await cdp.send('Network.setCacheDisabled', { cacheDisabled: true }, pageSession);
    const navigation = await cdp.send('Page.navigate', { url }, pageSession);
    if (navigation.errorText) throw new Error('IP 页面无法打开');
    const deadline = Date.now() + timeout;
    while (Date.now() < deadline && !signal?.aborted) {
      try {
        const result = await cdp.send('Runtime.evaluate', {
          expression: `location.href === ${JSON.stringify(url)} && document.readyState === 'complete' ? document.body?.innerText.trim().slice(0, 128) : null`,
          returnByValue: true,
        }, pageSession);
        const text = result.result?.value;
        if (typeof text === 'string') return normalizeIP(text);
      } catch (error) {
        if (error.code === 'IPIFY_INVALID_IP') throw error;
        // 导航切换执行上下文时稍后重试；不会重新请求或切换到直连。
      }
      await delay(100, undefined, { signal });
    }
    throw new Error('获取浏览器出口 IP 超时');
  };
  if (proxyURL) return testProxy(proxyURL, { fetchIP });
  try { return { ok: true, exit_ip: await fetchIP(), proxy_ips: [], matches: null }; }
  catch { return { ok: false, error: '无法从浏览器获取有效出口 IP，请检查网络后关闭并重新打开账号' }; }
}

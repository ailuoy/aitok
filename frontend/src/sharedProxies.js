import { request } from './api';
import { launcherPort, launcherRequest } from './localBrowser';

export async function requireSharedProxyLauncher(port) {
  const health = await launcherRequest('/health', { port });
  if (!health.shared_proxies) throw new Error('请更新并重启支持共享代理的桌面助手');
  return health;
}

export async function testSharedProxy(proxy, port = launcherPort(), action = 'test') {
  await requireSharedProxyLauncher(port);
  return launcherRequest('/shared-proxies/test', { port, method: 'POST', body: { proxy, action } });
}

// 配置统一走服务端；仅连通性测试在当前电脑执行。
export async function sharedProxyRequest(path, token, options = {}) {
  if (path.endsWith('/test') && options.method === 'POST') {
    const id = path.slice('/proxies/'.length, -'/test'.length);
    const proxy = await request('/proxies/' + id, token);
    const { result } = await testSharedProxy(proxy, undefined, options.body?.action);
    return request('/proxies/' + id + '/test-result', token, { method: 'POST', body: { ...proxy, last_test: result } });
  }
  return request(path, token, options);
}

export async function readLocalProxyConfig() {
  const port = launcherPort();
  const config = await launcherRequest('/proxies', { port });
  const proxies = [];
  for (const proxy of config.proxies) proxies.push(await launcherRequest('/proxies/' + proxy.id, { port }));
  if (port !== launcherPort()) throw new Error('本机端口已更改，请重新读取旧配置');
  return { ...config, proxies };
}

export async function readLegacyProxies() {
  const config = await readLocalProxyConfig();
  if (!config.proxies.length) throw new Error('当前电脑没有可导入的旧代理配置');
  const bindings = {};
  for (const [environment, proxy] of Object.entries(config.bindings || {})) {
    if (!environment.startsWith(location.origin + ':user:')) continue;
    const account = /:user:[0-9]+:account:([0-9]+)$/.exec(environment)?.[1];
    if (!account) continue;
    if (bindings[account] && bindings[account] !== proxy) throw new Error('本机不同登录用户对同一账号的代理选择有冲突，请先统一选择再导入');
    bindings[account] = proxy;
  }
  return { proxies: config.proxies, bindings };
}

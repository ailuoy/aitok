import https from 'node:https';
import tls from 'node:tls';
import { isIP } from 'node:net';
import { lookup } from 'node:dns/promises';
import { connectSocks5, parseProxy, ProxyError } from './proxy.mjs';

export function normalizeIP(ip) {
  if (isIP(ip) === 4) return ip;
  if (isIP(ip) === 6) return new URL(`http://[${ip}]`).hostname.slice(1, -1);
  throw new ProxyError('IPIFY_INVALID_IP', 'http');
}

export function proxyFailure(error) {
  const stages = { tcp: '连接代理服务器', negotiation: '协商 SOCKS5 协议', authentication: 'SOCKS5 认证', target: '代理连接 api.ipify.org:443', tls: '与 api.ipify.org 建立 TLS 连接', http: '请求 ipify 出口 IP', dns: '解析代理域名' };
  const messages = {
    SOCKS_AUTH_REJECTED: 'SOCKS5 认证失败：代理拒绝了用户名或密码',
    SOCKS_METHOD: '代理不支持当前 SOCKS5 认证方式，请检查协议和认证设置',
    SOCKS_REFUSED: '代理服务器拒绝连接，请检查地址、端口及服务状态',
    SOCKS_DNS: '无法解析代理域名，请检查代理主机地址',
    SOCKS_PROTOCOL: '代理返回了无效 SOCKS5 响应，请检查代理协议',
    SOCKS_TARGET_1: '代理服务器无法连接 api.ipify.org:443（一般故障）',
    SOCKS_TARGET_2: '代理规则禁止访问 api.ipify.org:443',
    SOCKS_TARGET_3: '代理服务器无法访问目标网络',
    SOCKS_TARGET_4: '代理服务器无法访问 api.ipify.org（域名解析或目标主机不可达）',
    SOCKS_TARGET_5: '代理连接 api.ipify.org:443 被拒绝',
    TLS_TIMEOUT: 'SOCKS5 连接已建立，但 api.ipify.org 的 TLS 握手超时，请检查代理出口网络',
    TLS_CLOSED: 'SOCKS5 连接已建立，但 TLS 握手被中断，请检查代理出口网络',
    TLS_CERTIFICATE: 'ipify 的 TLS 证书验证失败，请检查代理线路',
    HTTP_TIMEOUT: '已建立 TLS 连接，但获取出口 IP 超时',
    HTTP_CLOSED: '已建立 TLS 连接，但 ipify 响应中断',
    HTTP_STATUS: 'ipify 返回异常 HTTP 状态，请稍后重试',
    IPIFY_INVALID_IP: 'ipify 未返回有效 IP 地址',
    PROXY_DNS_FAILED: '无法解析代理主机 IP，暂时不能比对出口 IP',
  };
  const stage = error instanceof ProxyError && stages[error.stage] ? error.stage : 'unknown';
  const code = error instanceof ProxyError ? error.code : 'UNKNOWN';
  let message = messages[code];
  if (code === 'SOCKS_TIMEOUT') message = stages[stage] + '超时，请检查代理服务及网络连接';
  if (code === 'SOCKS_CLOSED') message = stages[stage] + '时连接中断，请检查代理服务及网络连接';
  return { stage, error_code: message ? code : 'UNKNOWN', error: message || '代理测试失败，请检查地址、认证信息及网络连接' };
}

async function fetchExitIP(proxy) {
  const agent = new https.Agent({ keepAlive: false });
  const signal = AbortSignal.timeout(30000);
  let stage = 'tcp';
  agent.createConnection = (_options, callback) => {
    connectSocks5(proxy, 'api.ipify.org', 443).then(socket => {
      if (signal.aborted) { socket.destroy(); callback(new ProxyError('SOCKS_TIMEOUT', 'target')); return; }
      stage = 'tls';
      socket.setTimeout(0);
      const secure = tls.connect({ socket, servername: 'api.ipify.org' });
      secure.once('secureConnect', () => { stage = 'http'; });
      callback(null, secure);
    }, callback);
  };
  try {
    return await new Promise((resolve, reject) => {
      const request = https.get('https://api.ipify.org', { agent, signal }, response => {
        if (response.statusCode !== 200) { response.resume(); reject(new ProxyError('HTTP_STATUS', 'http')); return; }
        let body = '';
        response.setEncoding('utf8');
        response.on('data', chunk => { body += chunk; if (body.length > 128) request.destroy(new Error('出口响应过大')); });
        response.once('error', reject);
        response.once('end', () => { try { resolve(normalizeIP(body.trim())); } catch (error) { reject(error); } });
      });
      request.once('error', reject);
    });
  } catch (error) {
    if (error instanceof ProxyError) throw error;
    const timeout = signal.aborted || error.code === 'ETIMEDOUT';
    const certificate = /CERT|SELF_SIGNED|UNABLE_TO_VERIFY/.test(error.code || '');
    throw new ProxyError(certificate ? 'TLS_CERTIFICATE' : stage === 'tls' ? timeout ? 'TLS_TIMEOUT' : 'TLS_CLOSED' : timeout ? 'HTTP_TIMEOUT' : 'HTTP_CLOSED', stage);
  } finally { agent.destroy(); }
}

// 目标固定为 ipify；不接受网页传入的测试 URL，也不回退到直连。
export async function testProxy(raw, { fetchIP = fetchExitIP, resolve = lookup } = {}) {
  const proxy = parseProxy(raw), start = Date.now();
  try {
    const [exitIP, addresses] = await Promise.all([
      fetchIP(proxy), isIP(proxy.host) ? [{ address: proxy.host }] : resolve(proxy.host, { all: true }).catch(() => { throw new ProxyError('PROXY_DNS_FAILED', 'dns'); }),
    ]);
    const ips = [...new Set(addresses.map(({ address }) => normalizeIP(address)))];
    return { ok: true, exit_ip: normalizeIP(exitIP), proxy_ips: ips, matches: ips.includes(normalizeIP(exitIP)), latency_ms: Date.now() - start, tested_at: new Date().toISOString() };
  } catch (error) {
    return { ok: false, ...proxyFailure(error), latency_ms: Date.now() - start, tested_at: new Date().toISOString() };
  }
}

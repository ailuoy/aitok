// 网页导出与本机连接共用格式，正确编码认证信息和 IPv6 地址。
export function proxyURL(proxy) {
  const host = proxy.host.includes(':') ? `[${proxy.host}]` : proxy.host;
  const auth = proxy.username ? `${encodeURIComponent(proxy.username)}:${encodeURIComponent(proxy.password)}@` : '';
  return `socks5://${auth}${host}:${proxy.port}`;
}

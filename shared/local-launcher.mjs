// 网页、命令行和桌面助手共享连接规则，避免开发与线上互相占用端口。
export function normalizeOrigin(value) {
  let url;
  try { url = new URL(value); } catch { throw new Error('请输入完整站点地址，例如 https://toktopup.com'); }
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.pathname !== '/' || url.search || url.hash) {
    throw new Error('站点地址只包含协议、域名和端口，不包含路径、账号或查询参数');
  }
  return url.origin;
}

export function defaultLauncherPort(origin) {
  const host = new URL(normalizeOrigin(origin)).hostname;
  return ['localhost', '127.0.0.1', '[::1]'].includes(host) ? 15684 : 15683;
}

export function validateLauncherPort(value) {
  if (!/^\d{1,5}$/.test(String(value)) || Number(value) < 1024 || Number(value) > 65535) {
    throw new Error('本机连接端口必须是 1024 至 65535 的整数');
  }
  return Number(value);
}

export const launcherAddress = port => `http://127.0.0.1:${validateLauncherPort(port)}`;
